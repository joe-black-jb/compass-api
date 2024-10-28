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

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file err: ", err)
		return
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

// TODO: CF計算書の登録、取得処理

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
		// fmt.Println("ファンダメンタル ⭐️: ", fundamental)
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
	// 2022-01-01 ~ 2024-09-30 まで完了
	/*
	  【末日】
	    Jan: 31   Feb: 28   Mar: 31   Apr: 30   May: 31   Jun: 30
	    Jul: 31   Aug: 31   Sep: 30   Oct: 31   Nov: 30   Dec: 31
	*/
	// 集計開始日付
	date := time.Date(2024, time.February, 1, 1, 0, 0, 0, loc)
	// 集計終了日付
	endDate := time.Date(2024, time.February, 31, 1, 0, 0, 0, loc)
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

	//////////// CF 計算書 ////////////
	// 【連結キャッシュ・フロー計算書】
	consolidatedCFPattern := `(?s)<jpcrp_cor:ConsolidatedStatementOfCashFlowsTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:ConsolidatedStatementOfCashFlowsTextBlock>`
	consolidatedCFRe := regexp.MustCompile(consolidatedCFPattern)
	consolidatedCFMattches := consolidatedCFRe.FindString(string(body))
	// fmt.Println(fmt.Sprintf("「%s」の連結CF\n%s", companyName, consolidatedCFMattches))
	fmt.Println(fmt.Sprintf("「%s」の連結CFはありますか❓: %v", companyName, consolidatedCFMattches != ""))
	// 【連結キャッシュ・フロー計算書 (IFRS)】
	consolidatedCFIFRSPattern := `(?s)<jpigp_cor:ConsolidatedStatementOfCashFlowsIFRSTextBlock contextRef="CurrentYearDuration">(.*?)</jpigp_cor:ConsolidatedStatementOfCashFlowsIFRSTextBlock>`
	consolidatedCFIFRSRe := regexp.MustCompile(consolidatedCFIFRSPattern)
	consolidatedCFIFRSMattches := consolidatedCFIFRSRe.FindString(string(body))
	// fmt.Println(fmt.Sprintf("「%s」の連結CF (IFRS)\n%s", companyName, consolidatedCFIFRSMattches))
	fmt.Println(fmt.Sprintf("「%s」の連結CF (IFRS) はありますか❓: %v", companyName, consolidatedCFIFRSMattches != ""))

	// 【キャッシュ・フロー計算書】
	soloCFPattern := `(?s)<jpcrp_cor:StatementOfCashFlowsTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:StatementOfCashFlowsTextBlock>`
	soloCFRe := regexp.MustCompile(soloCFPattern)
	soloCFMattches := soloCFRe.FindString(string(body))
	fmt.Println(fmt.Sprintf("「%s」のCFはありますか❓: %v", companyName, soloCFMattches != ""))

	// 【キャッシュ・フロー計算書 (IFRS)】
	soloCFIFRSPattern := `(?s)<jpcrp_cor:StatementOfCashFlowsIFRSTextBlock contextRef="CurrentYearDuration">(.*?)</jpcrp_cor:StatementOfCashFlowsIFRSTextBlock>`
	soloCFIFRSRe := regexp.MustCompile(soloCFIFRSPattern)
	soloCFIFRSMattches := soloCFIFRSRe.FindString(string(body))
	fmt.Println(fmt.Sprintf("「%s」のCF (IFRS)はありますか❓: %v", companyName, soloCFIFRSMattches != ""))
	////////////////////////////////////

	// エスケープ文字をデコード
	// 貸借対照表データの整形
	var unescapedBS string
	if consolidatedBSMatches == "" && soloBSMatches == "" {
		return
	} else if consolidatedBSMatches != "" {
		unescapedBS = html.UnescapeString(consolidatedBSMatches)
	} else if soloBSMatches != "" {
		unescapedBS = html.UnescapeString(soloBSMatches)
	}

	// 損益計算書データの整形
	var unescapedPL string
	if consolidatedPLMatches == "" && soloPLMatches == "" {
		return
	} else if consolidatedPLMatches != "" {
		unescapedPL = html.UnescapeString(consolidatedPLMatches)
	} else if soloPLMatches != "" {
		unescapedPL = html.UnescapeString(soloPLMatches)
	}

	// デコードしきれていない文字は replace
	// 特定のエンティティをさらに手動でデコード
	unescapedBS = strings.ReplaceAll(unescapedBS, "&apos;", "'")
	unescapedPL = strings.ReplaceAll(unescapedPL, "&apos;", "'")

	// html ファイルとして書き出す
	HTMLDirName := "HTML"
	bsHTMLFileName := fmt.Sprintf("%s.html", BSFileNamePattern)
	bsHTMLFilePath := filepath.Join(HTMLDirName, bsHTMLFileName)

	plHTMLFileName := fmt.Sprintf("%s.html", PLFileNamePattern)
	plHTMLFilePath := filepath.Join(HTMLDirName, plHTMLFileName)

	// HTMLディレクトリが存在するか確認
	if _, err := os.Stat(HTMLDirName); os.IsNotExist(err) {
		// ディレクトリが存在しない場合は作成
		err := os.Mkdir(HTMLDirName, 0755) // 0755はディレクトリのパーミッション
		if err != nil {
			fmt.Println("Error creating directory:", err)
			return
		}
	}

	// 貸借対照表
	bsHTML, err := os.Create(bsHTMLFilePath)
	if err != nil {
		fmt.Println("BS HTML create err: ", err)
		return
	}
	defer bsHTML.Close()

	_, err = bsHTML.WriteString(unescapedBS)
	if err != nil {
		fmt.Println("BS HTML write err: ", err)
		return
	}

	bsHTMLFile, err := os.Open(bsHTMLFilePath)
	if err != nil {
		fmt.Println("BS HTML open error: ", err)
		return
	}
	defer bsHTMLFile.Close()

	// goqueryでHTMLをパース
	doc, err := goquery.NewDocumentFromReader(bsHTMLFile)
	if err != nil {
		log.Fatal(err)
	}

	// 損益計算書
	plHTML, err := os.Create(plHTMLFilePath)
	if err != nil {
		fmt.Println("PL HTML create err: ", err)
		return
	}
	defer plHTML.Close()

	_, err = plHTML.WriteString(unescapedPL)
	if err != nil {
		fmt.Println("PL HTML write err: ", err)
		return
	}

	plHTMLFile, err := os.Open(plHTMLFilePath)
	if err != nil {
		fmt.Println("PL HTML open error: ", err)
		return
	}
	defer plHTMLFile.Close()

	// goqueryでHTMLをパース
	plDoc, err := goquery.NewDocumentFromReader(plHTMLFile)
	if err != nil {
		log.Fatal(err)
	}

	// 貸借対照表データ
	var summary internal.Summary
	summary.CompanyName = companyName
	summary.PeriodStart = periodStart
	summary.PeriodEnd = periodEnd
	UpdateSummary(doc, &summary, fundamental)
	isSummaryValid := ValidateSummary(summary)

	// 損益計算書データ
	var plSummary internal.PLSummary
	plSummary.CompanyName = companyName
	plSummary.PeriodStart = periodStart
	plSummary.PeriodEnd = periodEnd
	UpdatePLSummary(plDoc, &plSummary, fundamental)
	isPLSummaryValid := ValidatePLSummary(plSummary)

	// CF計算書データ
	cfFileNamePattern := fmt.Sprintf("%s-%s-CF-from-%s-to-%s", EDINETCode, docID, periodStart, periodEnd)
	cfDoc, err := ParseCF(cfFileNamePattern, string(body), consolidatedCFMattches, consolidatedCFIFRSMattches, soloCFMattches, soloCFIFRSMattches)
	if err != nil {
		fmt.Println("ParseCF err: ", err)
		return
	}
	var cfSummary internal.CFSummary
	cfSummary.CompanyName = companyName
	cfSummary.PeriodStart = periodStart
	cfSummary.PeriodEnd = periodEnd
	UpdateCFSummary(cfDoc, &cfSummary)
	// TODO: CFバリデーション後処理
	// isCFSummaryValid := ValidateCFSummary(cfSummary)

	// fmt.Println("summary ⭐️: ", summary)

	// 貸借対照表バリデーション後
	bsJsonName := fmt.Sprintf("%s.json", BSFileNamePattern)
	bsJsonPath := fmt.Sprintf("json/%s", bsJsonName)
	if isSummaryValid {
		// RegisterCompany
		RegisterCompany(dynamoClient, EDINETCode, companyName, isSummaryValid, false)
		// fmt.Println("有効な BS です🎾")

		// ディレクトリが存在しない場合は作成
		err = os.MkdirAll("json", os.ModePerm)
		if err != nil {
			fmt.Println("Error creating directory:", err)
			return
		}

		jsonFile, err := os.Create(bsJsonPath)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer jsonFile.Close()

		jsonBody, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			fmt.Println(err)
			return
		}
		_, err = jsonFile.Write(jsonBody)
		if err != nil {
			fmt.Println(err)
			return
		}

		// S3 に json ファイルを送信
		// Key は aws configure で設定する
		region := os.Getenv("REGION")
		sdkConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
		if err != nil {
			fmt.Println(err)
			return
		}
		s3Client := s3.NewFromConfig(sdkConfig)
		bucketName := os.Getenv("BUCKET_NAME")
		jsonFileOpen, err := os.Open(bsJsonPath)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer jsonFileOpen.Close()

		splitJsonName := strings.Split(bsJsonName, "-")
		if len(splitJsonName) >= 3 {
			reportType := splitJsonName[2] // BS or PL
			key := fmt.Sprintf("%s/%s/%s", EDINETCode, reportType, bsJsonName)

			// ファイルの存在チェック
			existsFile, _ := s3Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})
			if existsFile == nil {
				_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
					Bucket:      aws.String(bucketName),
					Key:         aws.String(key),
					Body:        jsonFileOpen,
					ContentType: aws.String("application/json"),
				})
				if err != nil {
					fmt.Println(err)
					return
				}
				uploadDoneMsg := fmt.Sprintf("「%s」の貸借対照表JSONを登録しました ⭕️ (ファイル名: %s)", companyName, key)
				fmt.Println(uploadDoneMsg)
			}
		}

		// HTML 送信
		// 貸借対照表HTML
		bsHTMLFileOpen, err := os.Open(bsHTMLFilePath)
		if err != nil {
			fmt.Println("BS HTML create err: ", err)
			return
		}
		defer bsHTMLFileOpen.Close()

		splitBSHTMLName := strings.Split(bsHTMLFileName, "-")
		if len(splitBSHTMLName) >= 3 {
			reportType := splitBSHTMLName[2] // BS or PL
			bsHTMLKey := fmt.Sprintf("%s/%s/%s", EDINETCode, reportType, bsHTMLFileName)

			// ファイルの存在チェック
			existsBSHTMLFile, _ := s3Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(bsHTMLKey),
			})
			if existsBSHTMLFile == nil {
				_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
					Bucket:      aws.String(bucketName),
					Key:         aws.String(bsHTMLKey),
					Body:        bsHTMLFileOpen,
					ContentType: aws.String("text/html"),
				})
				if err != nil {
					fmt.Println(err)
					return
				}
				uploadDoneMsg := fmt.Sprintf("「%s」の貸借対照表HTMLを登録しました ⭕️ (ファイル名: %s)", companyName, bsHTMLKey)
				fmt.Println(uploadDoneMsg)
			}
		}
		// validSummaryMsg := fmt.Sprintf("有効な BS Summary (CompanyName: %s, EDINETCode: %s, docID: %s, summary: %v)", companyName, EDINETCode, docID, summary)
		// fmt.Println(validSummaryMsg)
	} else {
		// invalidSummaryMsg := fmt.Sprintf("Invalid BS Summary (CompanyName: %s, EDINETCode: %s, docID: %s, summary: %v)", companyName, EDINETCode, docID, summary)
		invalidSummaryJson, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			fmt.Println("BSデータの json.MarshalIndent エラー❗️: ", err)
		}
		invalidSummaryMsg := fmt.Sprintf("「%s」の BS データが不正です ❌ (EDINETCode: %s, docID: %s, summaryJSON:\n%v)", companyName, EDINETCode, docID, string(invalidSummaryJson))
		fmt.Println(invalidSummaryMsg)
	}

	// 損益計算書バリデーション後
	plJsonName := fmt.Sprintf("%s.json", PLFileNamePattern)
	plJsonPath := fmt.Sprintf("json/%s", plJsonName)
	if isPLSummaryValid {
		// RegisterCompany
		RegisterCompany(dynamoClient, EDINETCode, companyName, false, isPLSummaryValid)
		jsonFile, err := os.Create(plJsonPath)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer jsonFile.Close()

		jsonBody, err := json.MarshalIndent(plSummary, "", "  ")
		if err != nil {
			fmt.Println(err)
			return
		}
		_, err = jsonFile.Write(jsonBody)
		if err != nil {
			fmt.Println(err)
			return
		}

		// S3 に json ファイルを送信
		// Key は aws configure で設定する
		region := os.Getenv("REGION")
		sdkConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
		if err != nil {
			fmt.Println(err)
			return
		}
		s3Client := s3.NewFromConfig(sdkConfig)
		bucketName := os.Getenv("BUCKET_NAME")
		jsonFileOpen, err := os.Open(plJsonPath)
		if err != nil {
			fmt.Println(err)
			return
		}
		splitJsonName := strings.Split(plJsonName, "-")
		if len(splitJsonName) >= 3 {
			reportType := splitJsonName[2] // BS or PL
			key := fmt.Sprintf("%s/%s/%s", EDINETCode, reportType, plJsonName)

			// ファイルの存在チェック
			existsFile, _ := s3Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})
			if existsFile == nil {
				_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
					Bucket:      aws.String(bucketName),
					Key:         aws.String(key),
					Body:        jsonFileOpen,
					ContentType: aws.String("application/json"),
				})
				if err != nil {
					fmt.Println(err)
					return
				}
				uploadDoneMsg := fmt.Sprintf("「%s」の損益計算書JSONを登録しました ⭕️ (ファイル名: %s)", companyName, key)
				fmt.Println(uploadDoneMsg)
			}
		}

		// 損益計算書HTML
		plHTMLFileOpen, err := os.Open(plHTMLFilePath)
		if err != nil {
			fmt.Println("PL HTML create err: ", err)
			return
		}
		defer plHTMLFileOpen.Close()

		splitPLHTMLName := strings.Split(plHTMLFileName, "-")
		if len(splitPLHTMLName) >= 3 {
			reportType := splitPLHTMLName[2] // BS or PL
			plHTMLKey := fmt.Sprintf("%s/%s/%s", EDINETCode, reportType, plHTMLFileName)

			// ファイルの存在チェック
			existsPLHTMLFile, _ := s3Client.HeadObject(context.TODO(), &s3.HeadObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(plHTMLKey),
			})
			if existsPLHTMLFile == nil {
				_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
					Bucket:      aws.String(bucketName),
					Key:         aws.String(plHTMLKey),
					Body:        plHTMLFileOpen,
					ContentType: aws.String("text/html"),
				})
				if err != nil {
					fmt.Println(err)
					return
				}
				uploadDoneMsg := fmt.Sprintf("「%s」の損益計算書HTMLを登録しました ⭕️ (ファイル名: %s)", companyName, plHTMLKey)
				fmt.Println(uploadDoneMsg)
			}
		}
	} else {
		invalidPLSummaryJson, err := json.MarshalIndent(plSummary, "", "  ")
		if err != nil {
			fmt.Println("PLデータの json.MarshalIndent エラー❗️: ", err)
		}
		invalidSummaryMsg := fmt.Sprintf("「%s」の PL データが不正です ❌ (EDINETCode: %s, docID: %s, summaryJSON:\n%s)", companyName, EDINETCode, docID, string(invalidPLSummaryJson))
		fmt.Println(invalidSummaryMsg)
	}

	// TODO: CF計算書バリデーション後
	/*
	  ・CF HTML の送信
	  ・CF JSON の送信
	*/
	// cfJsonName := fmt.Sprintf("%s.json", cfFileNamePattern)

	// HTML ファイルの削除
	err = os.RemoveAll(bsHTMLFilePath)
	if err != nil {
		fmt.Println("BS HTML ファイル削除エラー: ", err)
	}
	err = os.RemoveAll(plHTMLFilePath)
	if err != nil {
		fmt.Println("PL HTML ファイル削除エラー: ", err)
	}
	// JSON ファイルの削除
	err = os.RemoveAll(bsJsonPath)
	if err != nil {
		fmt.Println("PL HTML ファイル削除エラー: ", err)
	}
	err = os.RemoveAll(plJsonPath)
	if err != nil {
		fmt.Println("PL HTML ファイル削除エラー: ", err)
	}
	// XBRL ファイルの削除 parentPath
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

			if strings.Contains(titleName, "流動資産合計") {
				summary.CurrentAssets.Previous = previousIntValue
				summary.CurrentAssets.Current = currentIntValue
			}
			if strings.Contains(titleName, "有形固定資産合計") {
				summary.TangibleAssets.Previous = previousIntValue
				summary.TangibleAssets.Current = currentIntValue
			}
			if strings.Contains(titleName, "無形固定資産合計") {
				summary.IntangibleAssets.Previous = previousIntValue
				summary.IntangibleAssets.Current = currentIntValue
			}
			if strings.Contains(titleName, "投資その他の資産合計") {
				summary.InvestmentsAndOtherAssets.Previous = previousIntValue
				summary.InvestmentsAndOtherAssets.Current = currentIntValue
			}
			if strings.Contains(titleName, "流動負債合計") {
				summary.CurrentLiabilities.Previous = previousIntValue
				summary.CurrentLiabilities.Current = currentIntValue
			}
			if strings.Contains(titleName, "固定負債合計") {
				summary.FixedLiabilities.Previous = previousIntValue
				summary.FixedLiabilities.Current = currentIntValue
			}
			if strings.Contains(titleName, "純資産合計") {
				summary.NetAssets.Previous = previousIntValue
				summary.NetAssets.Current = currentIntValue
				// fundamental
				fundamental.NetAssets = currentIntValue
			}
			if strings.Contains(titleName, "負債合計") {
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
		summary.CurrentAssets.Previous != 0 &&
		summary.CurrentAssets.Current != 0 &&
		summary.TangibleAssets.Previous != 0 &&
		summary.TangibleAssets.Current != 0 &&
		summary.IntangibleAssets.Previous != 0 &&
		summary.IntangibleAssets.Current != 0 &&
		summary.InvestmentsAndOtherAssets.Previous != 0 &&
		summary.InvestmentsAndOtherAssets.Current != 0 &&
		summary.CurrentLiabilities.Previous != 0 &&
		summary.CurrentLiabilities.Current != 0 &&
		summary.FixedLiabilities.Previous != 0 &&
		summary.FixedLiabilities.Current != 0 &&
		summary.NetAssets.Previous != 0 &&
		summary.NetAssets.Current != 0 {
		return true
	}
	return false
}

func ValidatePLSummary(plSummary internal.PLSummary) bool {
	if plSummary.CompanyName != "" &&
		plSummary.PeriodStart != "" &&
		plSummary.PeriodEnd != "" &&
		plSummary.CostOfGoodsSold.Previous != 0 &&
		plSummary.CostOfGoodsSold.Current != 0 &&
		plSummary.SGAndA.Previous != 0 &&
		plSummary.SGAndA.Current != 0 &&
		plSummary.Sales.Previous != 0 &&
		plSummary.Sales.Current != 0 &&
		plSummary.OperatingProfit.Previous != 0 &&
		plSummary.OperatingProfit.Current != 0 {
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

	// 結果の表示
	// fmt.Printf("UpdateBS result: %+v\n", result)
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

	// 結果の表示
	// fmt.Printf("UpdatePL result: %+v\n", result)
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
	// E00748-S100PZ48-BS-from-2020-11-01-to-2021-10-31.html
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
		uploadDoneMsg := fmt.Sprintf("「%s」のファンダメンタルズJSONを登録しました ⭕️ (ファイル名: %s)", fundamental.CompanyName, key)

		fmt.Println(uploadDoneMsg)
	}
}

func ValidateFundamentals(fundamental internal.Fundamental) bool {
	if fundamental.CompanyName != "" &&
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

// CF計算書登録処理
/*
cfFileNamePattern:          ファイル名のパターン
body:                       文字列に変換したXBRLファイル
consolidatedCFMattches:     連結キャッシュ・フロー計算書
consolidatedCFIFRSMattches: 連結キャッシュ・フロー計算書 (IFRS)
soloCFMattches:             キャッシュ・フロー計算書
soloCFIFRSPattern:          キャッシュ・フロー計算書 (IFRS)
*/
func ParseCF(cfFileNamePattern, body string, consolidatedCFMattches string, consolidatedCFIFRSMattches string, soloCFMattches string, soloCFIFRSMattches string) (*goquery.Document, error) {

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
			baseStr := splitTdTexts[0]
			baseStr = strings.ReplaceAll(baseStr, "(", "")
			baseStr = strings.ReplaceAll(baseStr, ")", "")
			splitUnitStrs := strings.Split(baseStr, "：")
			if len(splitUnitStrs) >= 2 {
				cfSummary.UnitString = splitUnitStrs[1]
			}
		}
	})
}

func ValidateCFSummary(cfSummary internal.CFSummary) bool {
	if cfSummary.CompanyName != "" &&
		cfSummary.PeriodStart != "" &&
		cfSummary.PeriodEnd != "" &&
		cfSummary.OperatingCF.Previous != 0 &&
		cfSummary.OperatingCF.Current != 0 &&
		cfSummary.InvestingCF.Previous != 0 &&
		cfSummary.InvestingCF.Current != 0 &&
		cfSummary.FinancingCF.Previous != 0 &&
		cfSummary.FinancingCF.Current != 0 &&
		cfSummary.StartCash.Previous != 0 &&
		cfSummary.StartCash.Current != 0 &&
		cfSummary.EndCash.Previous != 0 &&
		cfSummary.EndCash.Current != 0 {
		return true
	}
	return false
}

// TODO: CF HTML 登録処理
func RegisterCFHTML() {}

// TODO: CF JSON 登録処理
func RegisterCFJSON() {}
