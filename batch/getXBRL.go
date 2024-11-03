package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/joe-black-jb/compass-api/internal"
	"github.com/joe-black-jb/compass-api/internal/api"
	"github.com/joho/godotenv"
)

var EDINETAPIKey string

var dynamoClient *dynamodb.Client

var tableName string

/* NOTE
・連結キャッシュフロー計算書:  0105050

*/

// TODO: 売上高 ではなく 営業収益 で計上している企業の PL
//       売上高と営業収益がどちらか入っていればOK
// TODO: 営業利益 の部分に 営業損失 とだけ記載してある企業の PL
// TODO: HTML は Validate 結果が false でも送信する

/*
【営業収益、営業利益の場合】
=== 借方 ===

=== 貸方 ===

*/

func init() {
	env := os.Getenv("ENV")

	if env == "local" {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file err: ", err)
			return
		}
	}
	EDINETAPIKey = os.Getenv("EDINET_API_KEY")
	if EDINETAPIKey == "" {
		fmt.Println("API key not found")
		return
	}

	cfg, cfgErr := config.LoadDefaultConfig(context.TODO())
	if cfgErr != nil {
		fmt.Println("Load default config error: %v", cfgErr)
		return
	}
	dynamoClient = dynamodb.NewFromConfig(cfg)
	tableName = os.Getenv("DYNAMO_TABLE_NAME")
}

func main() {
	start := time.Now()

	if tableName == "" {
		log.Fatal("テーブル名が設定されていません")
	}
	reports, err := GetReports()
	fmt.Println("len(reports): ", len(reports))
	if err != nil {
		fmt.Println(err)
		return
	}

	var wg sync.WaitGroup
	for _, report := range reports {
		wg.Add(1)
		EDINETCode := report.EdinetCode
		companyName := report.FilerName
		docID := report.DocId
		var periodStart string
		var periodEnd string
		if report.PeriodStart == "" || report.PeriodEnd == "" {
			// 正規表現を用いて抽出
			periodPattern := `(\d{4}/\d{2}/\d{2})－(\d{4}/\d{2}/\d{2})`
			// 正規表現をコンパイル
			re := regexp.MustCompile(periodPattern)
			// 正規表現でマッチした部分を取得
			match := re.FindString(report.DocDescription)

			if match != "" {
				splitPeriod := strings.Split(match, "－")
				if len(splitPeriod) >= 2 {
					periodStart = strings.ReplaceAll(splitPeriod[0], "/", "-")
					periodEnd = strings.ReplaceAll(splitPeriod[1], "/", "-")
				}
			}
		}

		if report.PeriodStart != "" {
			periodStart = report.PeriodStart
		}

		if report.PeriodEnd != "" {
			periodEnd = report.PeriodEnd
		}

		// ファンダメンタルズ
		fundamental := internal.Fundamental{
			CompanyName:     companyName,
			PeriodStart:     periodStart,
			PeriodEnd:       periodEnd,
			Sales:           0,
			OperatingProfit: 0,
			Liabilities:     0,
			NetAssets:       0,
		}
		go RegisterReport(dynamoClient, EDINETCode, docID, companyName, periodStart, periodEnd, &fundamental, &wg)
	}
	wg.Wait()

	fmt.Println("All processes done ⭐️")
	fmt.Println("所要時間: ", time.Since(start))
}

func unzip(source, destination string) (string, error) {
	// ZIPファイルをオープン
	r, err := zip.OpenReader(source)
	if err != nil {
		return "", fmt.Errorf("failed to open zip file: %v", err)
	}
	defer r.Close()

	var XBRLFilepath string

	// ZIP内の各ファイルを処理
	for _, f := range r.File {
		extension := filepath.Ext(f.Name)
		underPublic := strings.Contains(f.Name, "PublicDoc")

		// ファイル名に EDINETコードが含まれる かつ 拡張子が .xbrl の場合のみ処理する
		if underPublic && extension == ".xbrl" {
			// ファイル名を作成
			fpath := filepath.Join(destination, f.Name)

			// ディレクトリの場合は作成
			if f.FileInfo().IsDir() {
				os.MkdirAll(fpath, os.ModePerm)
				continue
			}

			// ファイルの場合はファイルを作成し、内容をコピー
			if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
				return "", err
			}

			outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return "", err
			}

			rc, err := f.Open()
			if err != nil {
				return "", err
			}

			_, err = io.Copy(outFile, rc)

			// リソースを閉じる
			outFile.Close()
			rc.Close()

			if err != nil {
				return "", err
			}

			XBRLFilepath = f.Name
		}
	}
	// zipファイルを削除
	err = os.RemoveAll(source)
	if err != nil {
		fmt.Println("zip ファイル削除エラー: ", err)
	}
	return XBRLFilepath, nil
}

/*
EDINET 書類一覧取得 API を使用し有価証券報告書または訂正有価証券報告書のデータを取得する
*/
func GetReports() ([]internal.Result, error) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		fmt.Println("load location error")
		return nil, err
	}

	var results []internal.Result
	/*
	  【末日】
	    Jan: 31   Feb: 28   Mar: 31   Apr: 30   May: 31   Jun: 30
	    Jul: 31   Aug: 31   Sep: 30   Oct: 31   Nov: 30   Dec: 31
	*/
	// ソフトバンクグループ株式会社 2024/06/21 15:21

	// 集計開始日付
	date := time.Date(2024, time.June, 21, 1, 0, 0, 0, loc)
	// 集計終了日付
	endDate := time.Date(2024, time.June, 21, 1, 0, 0, 0, loc)
	// now := time.Now()
	for date.Before(endDate) || date.Equal(endDate) {
		fmt.Println(fmt.Sprintf("%s の処理を開始します⭐️", date.Format("2006-01-02")))

		var statement internal.Report

		dateStr := date.Format("2006-01-02")
		url := fmt.Sprintf("https://api.edinet-fsa.go.jp/api/v2/documents.json?date=%s&&Subscription-Key=%s&type=2", dateStr, EDINETAPIKey)
		resp, err := http.Get(url)
		if err != nil {
			fmt.Println("http get error : ", err)
			return nil, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(body, &statement)
		if err != nil {
			return nil, err
		}

		for _, s := range statement.Results {
			// 有価証券報告書 (Securities Report)
			isSecReport := s.FormCode == "030000" && s.DocTypeCode == "120"
			// 訂正有価証券報告書 (Amended Securities Report)
			isAmendReport := s.FormCode == "030001" && s.DocTypeCode == "130"

			if isSecReport || isAmendReport {
				results = append(results, s)
			}
		}
		date = date.AddDate(0, 0, 1)
	}
	return results, nil
}

func RegisterReport(dynamoClient *dynamodb.Client, EDINETCode string, docID string, companyName string, periodStart string, periodEnd string, fundamental *internal.Fundamental, wg *sync.WaitGroup) {
	defer wg.Done()
	BSFileNamePattern := fmt.Sprintf("%s-%s-BS-from-%s-to-%s", EDINETCode, docID, periodStart, periodEnd)
	PLFileNamePattern := fmt.Sprintf("%s-%s-PL-from-%s-to-%s", EDINETCode, docID, periodStart, periodEnd)

	client := &http.Client{
		Timeout: 300 * time.Second,
	}
	url := fmt.Sprintf("https://api.edinet-fsa.go.jp/api/v2/documents/%s?type=1&Subscription-Key=%s", docID, EDINETAPIKey)
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("http get error : ", err)
		return
	}
	defer resp.Body.Close()

	dirPath := "XBRL"
	zipFileName := fmt.Sprintf("%s.zip", docID)
	path := filepath.Join(dirPath, zipFileName)

	// ディレクトリが存在しない場合は作成
	err = os.MkdirAll(dirPath, os.ModePerm)
	if err != nil {
		fmt.Println("Error creating directory:", err)
		return
	}
	file, err := os.Create(path)
	if err != nil {
		fmt.Println("Error while creating the file:", err)
		return
	}
	defer file.Close()

	// レスポンスのBody（ZIPファイルの内容）をファイルに書き込む
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		fmt.Println("Error while saving the file:", err)
		return
	}

	// ZIPファイルを解凍
	unzipDst := filepath.Join(dirPath, docID)
	XBRLFilepath, err := unzip(path, unzipDst)
	if err != nil {
		fmt.Println("Error unzipping file:", err)
		return
	}

	// XBRLファイルの取得
	parentPath := filepath.Join("XBRL", docID, XBRLFilepath)
	XBRLFile, err := os.Open(parentPath)
	if err != nil {
		fmt.Println("XBRL open err: ", err)
		return
	}
	body, err := io.ReadAll(XBRLFile)
	if err != nil {
		fmt.Println("XBRL read err: ", err)
		return
	}

	var xbrl internal.XBRL
	err = xml.Unmarshal(body, &xbrl)
	if err != nil {
		fmt.Println("XBRL Unmarshal err: ", err)
		return
	}

	// 【連結貸借対照表】
	consolidatedBSPattern := `(?s)<jpcrp_cor:ConsolidatedBalanceSheetTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:ConsolidatedBalanceSheetTextBlock>`
	consolidatedBSRe := regexp.MustCompile(consolidatedBSPattern)
	consolidatedBSMatches := consolidatedBSRe.FindString(string(body))

	// 【貸借対照表】
	soloBSPattern := `(?s)<jpcrp_cor:BalanceSheetTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:BalanceSheetTextBlock>`
	soloBSRe := regexp.MustCompile(soloBSPattern)
	soloBSMatches := soloBSRe.FindString(string(body))

	// 【連結損益計算書】
	consolidatedPLPattern := `(?s)<jpcrp_cor:ConsolidatedStatementOfIncomeTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:ConsolidatedStatementOfIncomeTextBlock>`
	consolidatedPLRe := regexp.MustCompile(consolidatedPLPattern)
	consolidatedPLMatches := consolidatedPLRe.FindString(string(body))

	// 【損益計算書】
	soloPLPattern := `(?s)<jpcrp_cor:StatementOfIncomeTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:StatementOfIncomeTextBlock>`
	soloPLRe := regexp.MustCompile(soloPLPattern)
	soloPLMatches := soloPLRe.FindString(string(body))

	// 【連結キャッシュ・フロー計算書】
	consolidatedCFPattern := `(?s)<jpcrp_cor:ConsolidatedStatementOfCashFlowsTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:ConsolidatedStatementOfCashFlowsTextBlock>`
	consolidatedCFRe := regexp.MustCompile(consolidatedCFPattern)
	consolidatedCFMattches := consolidatedCFRe.FindString(string(body))
	// 【連結キャッシュ・フロー計算書 (IFRS)】
	consolidatedCFIFRSPattern := `(?s)<jpigp_cor:ConsolidatedStatementOfCashFlowsIFRSTextBlock contextRef="CurrentYearDuration">(.*?)</jpigp_cor:ConsolidatedStatementOfCashFlowsIFRSTextBlock>`
	consolidatedCFIFRSRe := regexp.MustCompile(consolidatedCFIFRSPattern)
	consolidatedCFIFRSMattches := consolidatedCFIFRSRe.FindString(string(body))

	// 【キャッシュ・フロー計算書】
	soloCFPattern := `(?s)<jpcrp_cor:StatementOfCashFlowsTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:StatementOfCashFlowsTextBlock>`
	soloCFRe := regexp.MustCompile(soloCFPattern)
	soloCFMattches := soloCFRe.FindString(string(body))

	// 【キャッシュ・フロー計算書 (IFRS)】
	soloCFIFRSPattern := `(?s)<jpcrp_cor:StatementOfCashFlowsIFRSTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:StatementOfCashFlowsIFRSTextBlock>`
	soloCFIFRSRe := regexp.MustCompile(soloCFIFRSPattern)
	soloCFIFRSMattches := soloCFIFRSRe.FindString(string(body))

	// 貸借対照表HTMLをローカルに作成
	doc, err := CreateHTML("BS", consolidatedBSMatches, soloBSMatches, consolidatedPLMatches, soloPLMatches, BSFileNamePattern, PLFileNamePattern)
	if err != nil {
		fmt.Println("PL CreateHTML エラー: ", err)
		return
	}
	// 損益計算書HTMLをローカルに作成
	plDoc, err := CreateHTML("PL", consolidatedBSMatches, soloBSMatches, consolidatedPLMatches, soloPLMatches, BSFileNamePattern, PLFileNamePattern)
	if err != nil {
		fmt.Println("PL CreateHTML エラー: ", err)
		return
	}

	// 貸借対照表データ
	var summary internal.Summary
	summary.CompanyName = companyName
	summary.PeriodStart = periodStart
	summary.PeriodEnd = periodEnd
	UpdateSummary(doc, &summary, fundamental)
	// BS バリデーション用
	// isSummaryValid := ValidateSummary(summary)

	// 損益計算書データ
	var plSummary internal.PLSummary
	plSummary.CompanyName = companyName
	plSummary.PeriodStart = periodStart
	plSummary.PeriodEnd = periodEnd
	UpdatePLSummary(plDoc, &plSummary, fundamental)
	isPLSummaryValid := ValidatePLSummary(plSummary)

	// CF計算書データ
	cfFileNamePattern := fmt.Sprintf("%s-%s-CF-from-%s-to-%s", EDINETCode, docID, periodStart, periodEnd)
	cfHTML, err := CreateCFHTML(cfFileNamePattern, string(body), consolidatedCFMattches, consolidatedCFIFRSMattches, soloCFMattches, soloCFIFRSMattches)
	if err != nil {
		fmt.Println("CreateCFHTML err: ", err)
		return
	}
	var cfSummary internal.CFSummary
	cfSummary.CompanyName = companyName
	cfSummary.PeriodStart = periodStart
	cfSummary.PeriodEnd = periodEnd
	UpdateCFSummary(cfHTML, &cfSummary)
	isCFSummaryValid := ValidateCFSummary(cfSummary)

	// CF計算書バリデーション後
	var putFileWg sync.WaitGroup
	putFileWg.Add(2)
	// CF HTML は バリデーションの結果に関わらず送信
	// S3 に CF HTML 送信 (HTML はスクレイピング処理があるので S3 への送信処理を個別で実行)
	go PutFileToS3(EDINETCode, companyName, cfFileNamePattern, "html", &putFileWg)
	if isCFSummaryValid {
		// S3 に JSON 送信
		go HandleRegisterJSON(EDINETCode, companyName, cfFileNamePattern, cfSummary, &putFileWg)
	} else {
		putFileWg.Done()
		///// ログを出さない場合はコメントアウト /////
		PrintValidatedSummaryMsg(companyName, cfFileNamePattern, cfSummary, isCFSummaryValid)
		////////////////////////////////////////
	}
	putFileWg.Wait()

	// 貸借対照表バリデーションなしバージョン
	_, err = CreateJSON(BSFileNamePattern, summary)
	if err != nil {
		fmt.Println("BS JSON ファイル作成エラー: ", err)
		return
	}
	var putBsWg sync.WaitGroup
	putBsWg.Add(2)
	// BS JSON 送信
	go PutFileToS3(EDINETCode, companyName, BSFileNamePattern, "json", &putBsWg)
	// BS HTML 送信
	go PutFileToS3(EDINETCode, companyName, BSFileNamePattern, "html", &putBsWg)
	putBsWg.Wait()

	// 損益計算書バリデーション後
	var putPlWg sync.WaitGroup
	putPlWg.Add(2)
	// PL HTML 送信 (バリデーション結果に関わらず)
	go PutFileToS3(EDINETCode, companyName, PLFileNamePattern, "html", &putPlWg)
	if isPLSummaryValid {
		_, err = CreateJSON(PLFileNamePattern, plSummary)
		if err != nil {
			fmt.Println("PL JSON ファイル作成エラー: ", err)
			return
		}
		// PL JSON 送信
		go PutFileToS3(EDINETCode, companyName, PLFileNamePattern, "json", &putPlWg)
	} else {
		wg.Done()
	}
	putPlWg.Wait()

	// XBRL ファイルの削除
	xbrlDir := filepath.Join("XBRL", docID)
	err = os.RemoveAll(xbrlDir)
	if err != nil {
		fmt.Println("XBRL ディレクトリ削除エラー: ", err)
	}

	// ファンダメンタル用jsonの送信
	if ValidateFundamentals(*fundamental) {
		RegisterFundamental(dynamoClient, *fundamental, EDINETCode)
	}
}

func UpdateSummary(doc *goquery.Document, summary *internal.Summary, fundamental *internal.Fundamental) {
	doc.Find("tr").Each(func(i int, s *goquery.Selection) {
		tdText := s.Find("td").Text()
		tdText = strings.TrimSpace(tdText)
		splitTdTexts := strings.Split(tdText, "\n")
		var titleTexts []string
		for _, t := range splitTdTexts {
			if t != "" {
				titleTexts = append(titleTexts, t)
			}
		}
		if len(titleTexts) >= 3 {
			titleName := titleTexts[0]

			// 前期
			previousText := titleTexts[1]
			previousIntValue, err := api.ConvertTextValue2IntValue(previousText)
			if err != nil {
				return
			}

			// 当期
			currentText := titleTexts[2]
			currentIntValue, err := api.ConvertTextValue2IntValue(currentText)
			if err != nil {
				return
			}

			if titleName == "流動資産合計" {
				summary.CurrentAssets.Previous = previousIntValue
				summary.CurrentAssets.Current = currentIntValue
			}
			if titleName == "有形固定資産合計" {
				summary.TangibleAssets.Previous = previousIntValue
				summary.TangibleAssets.Current = currentIntValue
			}
			if titleName == "無形固定資産合計" {
				summary.IntangibleAssets.Previous = previousIntValue
				summary.IntangibleAssets.Current = currentIntValue
			}
			if titleName == "投資その他の資産合計" {
				summary.InvestmentsAndOtherAssets.Previous = previousIntValue
				summary.InvestmentsAndOtherAssets.Current = currentIntValue
			}
			if titleName == "流動負債合計" {
				summary.CurrentLiabilities.Previous = previousIntValue
				summary.CurrentLiabilities.Current = currentIntValue
			}
			if titleName == "固定負債合計" {
				summary.FixedLiabilities.Previous = previousIntValue
				summary.FixedLiabilities.Current = currentIntValue
			}
			if titleName == "純資産合計" {
				summary.NetAssets.Previous = previousIntValue
				summary.NetAssets.Current = currentIntValue
				// fundamental
				fundamental.NetAssets = currentIntValue
			}
			if titleName == "負債合計" {
				// fundamental
				fundamental.Liabilities = currentIntValue
			}
		}

		if len(splitTdTexts) == 1 && titleTexts != nil && strings.Contains(titleTexts[0], "単位：") {
			baseStr := splitTdTexts[0]
			baseStr = strings.ReplaceAll(baseStr, "(", "")
			baseStr = strings.ReplaceAll(baseStr, "（", "")
			baseStr = strings.ReplaceAll(baseStr, ")", "")
			baseStr = strings.ReplaceAll(baseStr, "）", "")
			splitUnitStrs := strings.Split(baseStr, "：")
			if len(splitUnitStrs) >= 2 {
				summary.UnitString = splitUnitStrs[1]
			}
		}
	})
}

func UpdatePLSummary(doc *goquery.Document, plSummary *internal.PLSummary, fundamental *internal.Fundamental) {
	// 営業収益合計の設定が終わったかどうか管理するフラグ
	isOperatingRevenueDone := false
	// 営業費用合計の設定が終わったかどうか管理するフラグ
	isOperatingCostDone := false

	doc.Find("tr").Each(func(i int, s *goquery.Selection) {
		tdText := s.Find("td").Text()
		tdText = strings.TrimSpace(tdText)
		splitTdTexts := strings.Split(tdText, "\n")
		var titleTexts []string
		for _, t := range splitTdTexts {
			if t != "" {
				titleTexts = append(titleTexts, t)
			}
		}
		if len(titleTexts) >= 3 {
			titleName := titleTexts[0]

			// 前期
			previousText := titleTexts[1]
			previousIntValue, err := api.ConvertTextValue2IntValue(previousText)
			if err != nil {
				// fmt.Println("previous value convert error: ", err)
				return
			}

			// 当期
			currentText := titleTexts[2]
			currentIntValue, err := api.ConvertTextValue2IntValue(currentText)
			if err != nil {
				// fmt.Println("current value convert error: ", err)
				return
			}

			// switch titleName {
			// case "売上原価":
			//   plSummary.CostOfGoodsSold.Previous = previousIntValue
			//   plSummary.CostOfGoodsSold.Current = currentIntValue
			// case "販売費及び一般管理費":
			//   plSummary.SGAndA.Previous = previousIntValue
			//   plSummary.SGAndA.Current = currentIntValue
			// case "売上高":
			//   plSummary.Sales.Previous = previousIntValue
			//   plSummary.Sales.Current = currentIntValue
			//   // fundamental
			//   fundamental.Sales = currentIntValue
			// }
			if strings.Contains(titleName, "売上原価") {
				plSummary.CostOfGoodsSold.Previous = previousIntValue
				plSummary.CostOfGoodsSold.Current = currentIntValue
			}
			if strings.Contains(titleName, "販売費及び一般管理費") {
				plSummary.SGAndA.Previous = previousIntValue
				plSummary.SGAndA.Current = currentIntValue
			}
			if strings.Contains(titleName, "売上高") {
				plSummary.Sales.Previous = previousIntValue
				plSummary.Sales.Current = currentIntValue
				// fundamental
				fundamental.Sales = currentIntValue
			}
			if strings.Contains(titleName, "営業利益") {
				plSummary.OperatingProfit.Previous = previousIntValue
				plSummary.OperatingProfit.Current = currentIntValue
				// fundamental
				fundamental.OperatingProfit = currentIntValue
			}
			if titleName == "営業損失（△）" {
				// fmt.Println("営業損失とだけ書いてあります")
				if plSummary.OperatingProfit.Previous == 0 {
					plSummary.OperatingProfit.Previous = previousIntValue
				}
				if plSummary.OperatingProfit.Current == 0 {
					plSummary.OperatingProfit.Current = currentIntValue
				}
				// fundamental
				if fundamental.OperatingProfit == 0 {
					fundamental.OperatingProfit = currentIntValue
				}
			}
			// 営業収益の後に営業収益合計がある場合は上書き
			if strings.Contains(titleName, "営業収益") && !isOperatingRevenueDone {
				plSummary.HasOperatingRevenue = true
				plSummary.OperatingRevenue.Previous = previousIntValue
				plSummary.OperatingRevenue.Current = currentIntValue
				// fundamental
				fundamental.HasOperatingRevenue = true
				fundamental.OperatingRevenue = currentIntValue
				if titleName == "営業収益合計" {
					isOperatingRevenueDone = true
				}
			}
			// 営業費用の後に営業費用合計がある場合は上書き
			if strings.Contains(titleName, "営業費用") && !isOperatingCostDone {
				plSummary.HasOperatingCost = true
				plSummary.OperatingCost.Previous = previousIntValue
				plSummary.OperatingCost.Current = currentIntValue
				// fundamental
				fundamental.HasOperatingCost = true
				fundamental.OperatingCost = currentIntValue
				if titleName == "営業費用合計" {
					isOperatingCostDone = true
				}
			}
		}
		if len(splitTdTexts) == 1 && titleTexts != nil && strings.Contains(titleTexts[0], "単位：") {
			baseStr := splitTdTexts[0]
			baseStr = strings.ReplaceAll(baseStr, "(", "")
			baseStr = strings.ReplaceAll(baseStr, ")", "")
			splitUnitStrs := strings.Split(baseStr, "：")
			if len(splitUnitStrs) >= 2 {
				plSummary.UnitString = splitUnitStrs[1]
			}
		}
	})
}

func ValidateSummary(summary internal.Summary) bool {
	if summary.CompanyName != "" &&
		summary.PeriodStart != "" &&
		summary.PeriodEnd != "" &&
		(summary.CurrentAssets.Previous != 0 || summary.CurrentAssets.Current != 0) &&
		(summary.TangibleAssets.Previous != 0 || summary.TangibleAssets.Current != 0) &&
		(summary.IntangibleAssets.Previous != 0 || summary.IntangibleAssets.Current != 0) &&
		(summary.InvestmentsAndOtherAssets.Previous != 0 || summary.InvestmentsAndOtherAssets.Current != 0) &&
		(summary.CurrentLiabilities.Previous != 0 || summary.CurrentLiabilities.Current != 0) &&
		(summary.FixedLiabilities.Previous != 0 || summary.FixedLiabilities.Current != 0) &&
		(summary.NetAssets.Previous != 0 || summary.NetAssets.Current != 0) {
		return true
	}
	return false
}

func ValidatePLSummary(plSummary internal.PLSummary) bool {
	if plSummary.HasOperatingRevenue && plSummary.HasOperatingCost {
		// 営業費用がある場合、営業費用と営業利益があればいい
		if (plSummary.OperatingRevenue.Previous != 0 || plSummary.OperatingRevenue.Current != 0) &&
			(plSummary.OperatingCost.Previous != 0 || plSummary.OperatingCost.Current != 0) {
			return true
		}
	} else if plSummary.CompanyName != "" &&
		plSummary.PeriodStart != "" &&
		plSummary.PeriodEnd != "" &&
		(plSummary.CostOfGoodsSold.Previous != 0 || plSummary.CostOfGoodsSold.Current != 0) &&
		(plSummary.SGAndA.Previous != 0 || plSummary.SGAndA.Current != 0) &&
		(plSummary.Sales.Previous != 0 || plSummary.Sales.Current != 0) &&
		(plSummary.OperatingProfit.Previous != 0 || plSummary.OperatingProfit.Current != 0) {
		return true
	}
	return false
}

func RegisterCompany(dynamoClient *dynamodb.Client, EDINETCode string, companyName string, isSummaryValid bool, isPLSummaryValid bool) {
	foundItems, err := api.QueryByName(dynamoClient, tableName, companyName, EDINETCode)
	if err != nil {
		fmt.Println(err)
		return
	}

	if len(foundItems) == 0 {
		var company internal.Company
		id, uuidErr := uuid.NewUUID()
		if uuidErr != nil {
			fmt.Println("uuid create error")
			return
		}
		company.ID = id.String()
		company.EDINETCode = EDINETCode
		company.Name = companyName
		if isSummaryValid {
			company.BS = 1
		}
		if isPLSummaryValid {
			company.PL = 1
		}

		item, err := attributevalue.MarshalMap(company)
		if err != nil {
			fmt.Println("MarshalMap err: ", err)
			return
		}

		input := &dynamodb.PutItemInput{
			TableName: aws.String(tableName),
			Item:      item,
		}
		_, err = dynamoClient.PutItem(context.TODO(), input)
		if err != nil {
			fmt.Println("dynamoClient.PutItem err: ", err)
			return
		}
		doneMsg := fmt.Sprintf("「%s」をDBに新規登録しました ⭕️", companyName)
		fmt.Println(doneMsg)
	} else {
		foundItem := foundItems[0]
		if foundItem != nil {
			var company internal.Company
			// BS, PL フラグの設定
			// fmt.Println("すでに登録された company: ", foundItem)
			// company型に UnmarshalMap
			err = attributevalue.UnmarshalMap(foundItem, &company)
			if err != nil {
				fmt.Println("attributevalue.UnmarshalMap err: ", err)
				return
			}

			if company.BS == 0 && isSummaryValid {
				// company.BS を 1 に更新
				UpdateBS(dynamoClient, company.ID, 1)
			}

			if company.PL == 0 && isPLSummaryValid {
				// company.PL を 1 に更新
				UpdatePL(dynamoClient, company.ID, 1)
			}
		}
	}
}

func UpdateBS(dynamoClient *dynamodb.Client, id string, bs int) {
	// 更新するカラムとその値の指定
	updateInput := &dynamodb.UpdateItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
		UpdateExpression: aws.String("SET #bs = :newBS"),
		ExpressionAttributeNames: map[string]string{
			"#bs": "bs", // "bs" カラムを指定
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":newBS": &types.AttributeValueMemberN{Value: strconv.Itoa(bs)},
		},
		ReturnValues: types.ReturnValueUpdatedNew, // 更新後の新しい値を返す
	}

	// 更新の実行
	_, err := dynamoClient.UpdateItem(context.TODO(), updateInput)
	if err != nil {
		log.Fatalf("failed to update item, %v", err)
	}
}

func UpdatePL(dynamoClient *dynamodb.Client, id string, pl int) {
	// 更新するカラムとその値の指定
	updateInput := &dynamodb.UpdateItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
		UpdateExpression: aws.String("SET #pl = :newPL"),
		ExpressionAttributeNames: map[string]string{
			"#pl": "pl", // "pl" カラムを指定
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":newPL": &types.AttributeValueMemberN{Value: strconv.Itoa(pl)},
		},
		ReturnValues: types.ReturnValueUpdatedNew, // 更新後の新しい値を返す
	}

	// 更新の実行
	_, err := dynamoClient.UpdateItem(context.TODO(), updateInput)
	if err != nil {
		log.Fatalf("failed to update item, %v", err)
	}
}

func RegisterFundamental(dynamoClient *dynamodb.Client, fundamental internal.Fundamental, EDINETCode string) {
	region := os.Getenv("REGION")
	sdkConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		fmt.Println(err)
		return
	}
	s3Client := s3.NewFromConfig(sdkConfig)
	bucketName := os.Getenv("BUCKET_NAME")

	fundamentalBody, err := json.Marshal(fundamental)
	if err != nil {
		fmt.Println("fundamental json.Marshal err: ", err)
		return
	}
	// ファイル名
	fundamentalsFileName := fmt.Sprintf("%s-fundamentals-from-%s-to-%s.json", EDINETCode, fundamental.PeriodStart, fundamental.PeriodEnd)
	key := fmt.Sprintf("%s/Fundamentals/%s", EDINETCode, fundamentalsFileName)
	// ファイルの存在チェック
	existsFile, _ := s3Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if existsFile == nil {
		_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
			Bucket:      aws.String(bucketName),
			Key:         aws.String(key),
			Body:        strings.NewReader(string(fundamentalBody)),
			ContentType: aws.String("application/json"),
		})
		if err != nil {
			fmt.Println(err)
			return
		}
		///// ログを出さない場合はコメントアウト /////
		uploadDoneMsg := fmt.Sprintf("「%s」のファンダメンタルズJSONを登録しました ⭕️ (ファイル名: %s)", fundamental.CompanyName, key)
		fmt.Println(uploadDoneMsg)
		////////////////////////////////////////
	}
}

func ValidateFundamentals(fundamental internal.Fundamental) bool {
	if fundamental.HasOperatingRevenue && fundamental.HasOperatingCost {
		if fundamental.CompanyName != "" &&
			fundamental.PeriodStart != "" &&
			fundamental.PeriodEnd != "" &&
			fundamental.OperatingProfit != 0 &&
			fundamental.Liabilities != 0 &&
			fundamental.NetAssets != 0 &&
			fundamental.OperatingRevenue != 0 &&
			fundamental.OperatingCost != 0 {
			return true
		}
	} else if fundamental.CompanyName != "" &&
		fundamental.PeriodStart != "" &&
		fundamental.PeriodEnd != "" &&
		fundamental.Sales != 0 &&
		fundamental.OperatingProfit != 0 &&
		fundamental.Liabilities != 0 &&
		fundamental.NetAssets != 0 {
		return true
	}
	return false
}

/*
HTMLをパースしローカルに保存する
@params

		fileType:                BS もしくは PL
		body:                    ファイルの中身
		consolidatedBSMatches:   連結貸借対照表データが入っているタグの中身
		soloBSMatches:           貸借対照表データが入っているタグの中身
		consolidatedPLMatches:   連結損益計算書データが入っているタグの中身
		soloPLMatches:           損益計算書データが入っているタグの中身
	  BSFileNamePattern:       BSファイル名パターン
		PLFileNamePattern:       PLファイル名パターン
*/
func CreateHTML(fileType, consolidatedBSMatches, soloBSMatches, consolidatedPLMatches, soloPLMatches, BSFileNamePattern, PLFileNamePattern string) (*goquery.Document, error) {
	// エスケープ文字をデコード
	var unescapedStr string

	// BS の場合
	if fileType == "BS" {
		if consolidatedBSMatches == "" && soloBSMatches == "" {
			return nil, errors.New("parse 対象の貸借対照表データがありません")
		} else if consolidatedBSMatches != "" {
			unescapedStr = html.UnescapeString(consolidatedBSMatches)
		} else if soloBSMatches != "" {
			unescapedStr = html.UnescapeString(soloBSMatches)
		}
	}

	// PL の場合
	if fileType == "PL" {
		if consolidatedPLMatches == "" && soloPLMatches == "" {
			return nil, errors.New("parse 対象の損益計算書データがありません")
		} else if consolidatedPLMatches != "" {
			unescapedStr = html.UnescapeString(consolidatedPLMatches)
		} else if soloPLMatches != "" {
			unescapedStr = html.UnescapeString(soloPLMatches)
		}
	}

	// デコードしきれていない文字は replace
	// 特定のエンティティをさらに手動でデコード
	unescapedStr = strings.ReplaceAll(unescapedStr, "&apos;", "'")

	// HTMLデータを加工
	unescapedStr = FormatHtmlTable(unescapedStr)

	// html ファイルとして書き出す
	HTMLDirName := "HTML"
	var fileName string
	var filePath string

	if fileType == "BS" {
		fileName = fmt.Sprintf("%s.html", BSFileNamePattern)
		filePath = filepath.Join(HTMLDirName, fileName)
	}

	if fileType == "PL" {
		fileName = fmt.Sprintf("%s.html", PLFileNamePattern)
		filePath = filepath.Join(HTMLDirName, fileName)
	}

	// HTMLディレクトリが存在するか確認
	if _, err := os.Stat(HTMLDirName); os.IsNotExist(err) {
		// ディレクトリが存在しない場合は作成
		err := os.Mkdir(HTMLDirName, 0755) // 0755はディレクトリのパーミッション
		if err != nil {
			fmt.Println("Error creating directory:", err)
			return nil, err
		}
	}

	createFile, err := os.Create(filePath)
	if err != nil {
		fmt.Println("HTML create err: ", err)
		return nil, err
	}
	defer createFile.Close()

	_, err = createFile.WriteString(unescapedStr)
	if err != nil {
		fmt.Println("HTML write err: ", err)
		return nil, err
	}

	openFile, err := os.Open(filePath)
	if err != nil {
		fmt.Println("HTML open error: ", err)
		return nil, err
	}
	defer openFile.Close()

	// goqueryでHTMLをパース
	doc, err := goquery.NewDocumentFromReader(openFile)
	if err != nil {
		fmt.Println("HTML goquery.NewDocumentFromReader error: ", err)
		return nil, err
	}
	// return した doc は updateSummary に渡す
	return doc, nil
}

// CF計算書登録処理
/*
cfFileNamePattern:          ファイル名のパターン
body:                       文字列に変換したXBRLファイル
consolidatedCFMattches:     連結キャッシュ・フロー計算書
consolidatedCFIFRSMattches: 連結キャッシュ・フロー計算書 (IFRS)
soloCFMattches:             キャッシュ・フロー計算書
soloCFIFRSPattern:          キャッシュ・フロー計算書 (IFRS)
*/
func CreateCFHTML(cfFileNamePattern, body string, consolidatedCFMattches string, consolidatedCFIFRSMattches string, soloCFMattches string, soloCFIFRSMattches string) (*goquery.Document, error) {

	if consolidatedCFMattches == "" && consolidatedCFIFRSMattches == "" && soloCFMattches == "" && soloCFIFRSMattches == "" {
		return nil, errors.New("パースする対象がありません")
	}

	var match string

	if consolidatedCFMattches != "" {
		match = consolidatedCFMattches
	} else if consolidatedCFIFRSMattches != "" {
		match = consolidatedCFIFRSMattches
	} else if soloCFMattches != "" {
		match = soloCFMattches
	} else if soloCFIFRSMattches != "" {
		match = soloCFIFRSMattches
	}

	unescapedMatch := html.UnescapeString(match)
	// デコードしきれていない文字は replace
	// 特定のエンティティをさらに手動でデコード
	unescapedMatch = strings.ReplaceAll(unescapedMatch, "&apos;", "'")
	unescapedMatch = FormatHtmlTable(unescapedMatch)

	HTMLDirName := "HTML"
	cfHTMLFileName := fmt.Sprintf("%s.html", cfFileNamePattern)
	cfHTMLFilePath := filepath.Join(HTMLDirName, cfHTMLFileName)

	// HTML ファイルの作成
	cfHTML, err := os.Create(cfHTMLFilePath)
	if err != nil {
		fmt.Println("CF HTML create err: ", err)
		return nil, err
	}
	defer cfHTML.Close()

	// HTML ファイルに書き込み
	_, err = cfHTML.WriteString(unescapedMatch)
	if err != nil {
		fmt.Println("CF HTML write err: ", err)
		return nil, err
	}

	// HTML ファイルの読み込み
	cfHTMLFile, err := os.Open(cfHTMLFilePath)
	if err != nil {
		fmt.Println("CF HTML open error: ", err)
		return nil, err
	}
	defer cfHTMLFile.Close()

	// goqueryでHTMLをパース
	cfDoc, err := goquery.NewDocumentFromReader(cfHTMLFile)
	if err != nil {
		fmt.Println("CF goquery.NewDocumentFromReader err: ", err)
		return nil, err
	}
	return cfDoc, nil
}

func CreateJSON(fileNamePattern string, summary interface{}) (string, error) {
	fileName := fmt.Sprintf("%s.json", fileNamePattern)
	filePath := fmt.Sprintf("json/%s", fileName)

	// ディレクトリが存在しない場合は作成
	err := os.MkdirAll("json", os.ModePerm)
	if err != nil {
		fmt.Println("Error creating directory:", err)
		return "", err
	}

	jsonFile, err := os.Create(filePath)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	defer jsonFile.Close()

	jsonBody, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	_, err = jsonFile.Write(jsonBody)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	return filePath, nil
}

func UpdateCFSummary(cfDoc *goquery.Document, cfSummary *internal.CFSummary) {
	cfDoc.Find("tr").Each(func(i int, s *goquery.Selection) {
		tdText := s.Find("td").Text()
		tdText = strings.TrimSpace(tdText)
		splitTdTexts := strings.Split(tdText, "\n")
		var titleTexts []string
		for _, t := range splitTdTexts {
			if t != "" {
				titleTexts = append(titleTexts, t)
			}
		}
		if len(titleTexts) >= 3 {
			titleName := titleTexts[0]

			// 前期
			previousText := titleTexts[1]
			previousIntValue, err := api.ConvertTextValue2IntValue(previousText)
			if err != nil {
				// fmt.Println("previous value convert error: ", err)
				return
			}

			// 当期
			currentText := titleTexts[2]
			currentIntValue, err := api.ConvertTextValue2IntValue(currentText)
			if err != nil {
				// fmt.Println("current value convert error: ", err)
				return
			}

			if strings.Contains(titleName, "営業活動による") {
				cfSummary.OperatingCF.Previous = previousIntValue
				cfSummary.OperatingCF.Current = currentIntValue
			}
			if strings.Contains(titleName, "投資活動による") {
				cfSummary.InvestingCF.Previous = previousIntValue
				cfSummary.InvestingCF.Current = currentIntValue
			}
			if strings.Contains(titleName, "財務活動による") {
				cfSummary.FinancingCF.Previous = previousIntValue
				cfSummary.FinancingCF.Current = currentIntValue
			}
			if strings.Contains(titleName, "期首残高") {
				cfSummary.StartCash.Previous = previousIntValue
				cfSummary.StartCash.Current = currentIntValue
			}
			if strings.Contains(titleName, "期末残高") {
				cfSummary.EndCash.Previous = previousIntValue
				cfSummary.EndCash.Current = currentIntValue
			}
		}
		if len(splitTdTexts) == 1 && titleTexts != nil && strings.Contains(titleTexts[0], "単位：") {
			formatUnitStr := FormatUnitStr(splitTdTexts[0])
			if formatUnitStr != "" {
				cfSummary.UnitString = formatUnitStr
			}
		}
	})
}

func ValidateCFSummary(cfSummary internal.CFSummary) bool {
	if cfSummary.CompanyName != "" &&
		cfSummary.PeriodStart != "" &&
		cfSummary.PeriodEnd != "" &&
		(cfSummary.OperatingCF.Previous != 0 || cfSummary.OperatingCF.Current != 0) &&
		(cfSummary.InvestingCF.Previous != 0 || cfSummary.InvestingCF.Current != 0) &&
		(cfSummary.FinancingCF.Previous != 0 || cfSummary.FinancingCF.Current != 0) &&
		(cfSummary.StartCash.Previous != 0 || cfSummary.StartCash.Current != 0) &&
		(cfSummary.EndCash.Previous != 0 || cfSummary.EndCash.Current != 0) {
		return true
	}
	return false
}

func PrintValidatedSummaryMsg(companyName string, fileName string, summary interface{}, isValid bool) {
	var summaryType string

	switch summary.(type) {
	case internal.Summary:
		summaryType = "貸借対照表"
	case internal.PLSummary:
		summaryType = "損益計算書"
	case internal.CFSummary:
		summaryType = "CF計算書"
	}

	jsonBody, _ := json.MarshalIndent(summary, "", "  ")

	var detailStr string
	var validStr string
	switch isValid {
	case true:
		validStr = "有効です⭕️"
	case false:
		validStr = "無効です❌"
		detailStr = fmt.Sprintf("詳細:\n%v\n", string(jsonBody))
	}

	msg := fmt.Sprintf("「%s」の%sサマリーJSON (%s) は%s %s", companyName, summaryType, fileName, validStr, detailStr)
	println(msg)
}

// TODO: 汎用ファイル送信処理
func PutFileToS3(EDINETCode string, companyName string, fileNamePattern string, extension string, wg *sync.WaitGroup) {
	defer wg.Done()

	var fileName string
	var filePath string

	switch extension {
	case "json":
		fileName = fmt.Sprintf("%s.json", fileNamePattern)
		filePath = fmt.Sprintf("json/%s", fileName)
	case "html":
		fileName = fmt.Sprintf("%s.html", fileNamePattern)
		filePath = fmt.Sprintf("HTML/%s", fileName)
	}

	// 処理後、ローカルファイルを削除
	defer func() {
		err := os.RemoveAll(filePath)
		if err != nil {
			fmt.Printf("ローカルファイル削除エラー: %v\n", err)
			return
		}
		// fmt.Printf("%s を削除しました\n", filePath)
	}()

	// S3 に ファイルを送信 (Key は aws configure で設定しておく)
	region := os.Getenv("REGION")
	sdkConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		fmt.Println(err)
		return
	}
	s3Client := s3.NewFromConfig(sdkConfig)
	bucketName := os.Getenv("BUCKET_NAME")

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("open file error: ", err)
		return
	}
	defer func() {
		err := file.Close()
		if err != nil {
			fmt.Println("ローカルファイル close エラー: ", err)
			return
		}
	}()

	splitByHyphen := strings.Split(fileName, "-")
	if len(splitByHyphen) >= 3 {
		reportType := splitByHyphen[2] // BS or PL or CF
		key := fmt.Sprintf("%s/%s/%s", EDINETCode, reportType, fileName)

		contentType, err := GetContentType(extension)
		if err != nil {
			fmt.Println("ContentType 取得エラー: ", err)
			return
		}

		// ファイルの存在チェック
		existsFile, _ := s3Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(key),
		})
		if existsFile == nil {
			_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
				Bucket:      aws.String(bucketName),
				Key:         aws.String(key),
				Body:        file,
				ContentType: aws.String(contentType),
			})
			if err != nil {
				fmt.Println("S3 PutObject error: ", err)
				return
			}

			///// ログを出さない場合はコメントアウト /////
			var reportTypeStr string
			switch reportType {
			case "BS":
				reportTypeStr = "貸借対照表"
			case "PL":
				reportTypeStr = "損益計算書"
			case "CF":
				reportTypeStr = "CF計算書"
			}
			uploadDoneMsg := fmt.Sprintf("「%s」の%s%sを登録しました ⭕️ (ファイル名: %s)", companyName, reportTypeStr, extension, key)
			fmt.Println(uploadDoneMsg)
			////////////////////////////////////////
		}
	}
}

func GetContentType(extension string) (string, error) {
	switch extension {
	case "json":
		return "application/json", nil
	case "html":
		return "text/html", nil
	}
	return "", errors.New("無効なファイル形式です")
}

func HandleRegisterJSON(EDINETCode string, companyName string, fileNamePattern string, summary interface{}, wg *sync.WaitGroup) {
	_, err := CreateJSON(fileNamePattern, summary)
	if err != nil {
		fmt.Println("CF JSON ファイル作成エラー: ", err)
		return
	}
	PutFileToS3(EDINETCode, companyName, fileNamePattern, "json", wg)
}

func FormatUnitStr(baseStr string) string {
	baseStr = strings.ReplaceAll(baseStr, "(", "")
	baseStr = strings.ReplaceAll(baseStr, "（", "")
	baseStr = strings.ReplaceAll(baseStr, ")", "")
	baseStr = strings.ReplaceAll(baseStr, "）", "")
	splitUnitStrs := strings.Split(baseStr, "：")
	if len(splitUnitStrs) >= 2 {
		return splitUnitStrs[1]
	}
	return ""
}

func FormatHtmlTable(htmlStr string) string {
	tbPattern := `(?s)<table(.*?)>`
	tbRe := regexp.MustCompile(tbPattern)
	tbMatch := tbRe.FindString(htmlStr)

	tbWidthPatternSemicolon := `width(.*?);`
	tbWidthPatternPt := `width(.*?)pt`
	tbWidthPatternPx := `width(.*?)px`

	tbWidthReSemicolon := regexp.MustCompile(tbWidthPatternSemicolon)
	tbWidthRePt := regexp.MustCompile(tbWidthPatternPt)
	tbWidthRePx := regexp.MustCompile(tbWidthPatternPx)

	tbWidthMatchSemicolon := tbWidthReSemicolon.FindString(tbMatch)
	tbWidthMatchPt := tbWidthRePt.FindString(tbMatch)
	tbWidthMatchPx := tbWidthRePx.FindString(tbMatch)

	var newTbStr string
	if tbWidthMatchSemicolon != "" {
		newTbStr = strings.ReplaceAll(tbMatch, tbWidthMatchSemicolon, "")
	} else if tbWidthMatchPt != "" {
		newTbStr = strings.ReplaceAll(tbMatch, tbWidthMatchPt, "")
	} else if tbWidthMatchPx != "" {
		newTbStr = strings.ReplaceAll(tbMatch, tbWidthMatchPx, "")
	}

	if newTbStr != "" {
		// <table> タグを入れ替える
		htmlStr = strings.ReplaceAll(htmlStr, tbMatch, newTbStr)
	}

	// colgroup の削除
	colGroupPattern := `(?s)<colgroup(.*?)</colgroup>`
	colGroupRe := regexp.MustCompile(colGroupPattern)
	colGroupMatch := colGroupRe.FindString(htmlStr)
	if colGroupMatch != "" {
		htmlStr = strings.ReplaceAll(htmlStr, colGroupMatch, "")
	}
	return htmlStr
}
