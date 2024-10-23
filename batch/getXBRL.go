package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"encoding/xml"
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
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/joe-black-jb/compass-api/internal"
	"github.com/joho/godotenv"
)

// <link:schemaRef> 要素
type SchemaRef struct {
	Href string `xml:"xlink:href,attr"`
	Type string `xml:"xlink:type,attr"`
}

// <xbrli:identifier> 要素
type Identifier struct {
	Scheme string `xml:"scheme,attr"`
	Value  string `xml:",chardata"`
}

// <xbrli:entity> 要素
type Entity struct {
	Identifier Identifier `xml:"identifier"`
}

// <xbrli:period> 要素
type Period struct {
	Instant string `xml:"instant"`
}

// <xbrli:context> 要素
type Context struct {
	ID     string `xml:"id,attr"`
	Entity Entity `xml:"entity"`
	Period Period `xml:"period"`
}

// <jppfs_cor:MoneyHeldInTrustCAFND> タグの構造体
type MoneyHeldInTrust struct {
	ContextRef string `xml:"contextRef,attr"`
	Decimals   string `xml:"decimals,attr"`
	UnitRef    string `xml:"unitRef,attr"`
	Value      string `xml:",chardata"`
}

// 貸借対照表の行（Assets or Liabilities）
type BalanceSheetItem struct {
	Description string `xml:"td>p>span"`              // 項目名
	AmountYear1 string `xml:"td:nth-child(2)>p>span"` // 特定28期の金額
	AmountYear2 string `xml:"td:nth-child(3)>p>span"` // 特定29期の金額
}

// 貸借対照表の構造体
type BalanceSheet struct {
	Title string             `xml:"p>span"`   // 貸借対照表のタイトル
	Unit  string             `xml:"caption"`  // 単位
	Items []BalanceSheetItem `xml:"tbody>tr"` // 資産、負債の各項目
}

// XML全体のルート構造体
type XBRL struct {
	XMLName                                                                 xml.Name         `xml:"xbrl"`
	SchemaRef                                                               SchemaRef        `xml:"schemaRef"`
	Contexts                                                                []Context        `xml:"context"`
	MoneyHeldInTrust                                                        MoneyHeldInTrust `xml:"MoneyHeldInTrustCAFND"`
	BalanceSheetTextBlock                                                   BalanceSheet     `xml:"BalanceSheetTextBlock"`
	NotesFinancialInformationOfInvestmentTrustManagementCompanyEtcTextBlock BalanceSheet     `xml:"NotesFinancialInformationOfInvestmentTrustManagementCompanyEtcTextBlock"`
}

// 項目ごとの値
type TitleValue struct {
	Previous int `json:"previous"`
	Current  int `json:"current"`
}

type MyError interface {
	Error() string
}

var EDINETAPIKey string

var dynamoClient *dynamodb.Client

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
}

func main() {
	reports, err := GetReports()
	fmt.Println("len(reports): ", len(reports))
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, report := range reports {
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
		// fmt.Println("資料名 ⭐️: ", report.DocDescription)
		// fmt.Println("コード: ", EDINETCode)
		// fmt.Println("企業名: ", companyName)
		// fmt.Println("docID: ", docID)
		// fmt.Println("periodStart: ", periodStart)
		// fmt.Println("periodEnd: ", periodEnd)
    RegisterCompany(dynamoClient, EDINETCode, companyName)
		RegisterReport(EDINETCode, docID, companyName, periodStart, periodEnd)
	}

	fmt.Println("All processes done ⭐️")
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

			// TODO: ディレクトリを削除する
		}
	}
	return XBRLFilepath, nil
}

type Param struct {
	Date string `json:"date"`
	Type string `json:"type"`
}

type ResultCount struct {
	Count int `json:"count"`
}

type Meta struct {
	Title           string      `json:"title"`
	Parameter       Param       `json:"parameter"`
	ResultSet       ResultCount `json:"resultset"`
	ProcessDateTime string      `json:"processDateTime"`
	Status          string      `json:"status"`
	Message         string      `json:"message"`
}

type Result struct {
	SeqNumber            int    `json:"seqNumber"`
	DocId                string `json:"docId"`
	EdinetCode           string `json:"edinetCode"`
	SecCode              string `json:"secCode"`
	JCN                  string `json:"JCN"`
	FilerName            string `json:"filerName"` // 企業名
	FundCode             string `json:"fundCode"`
	OrdinanceCode        string `json:"ordinanceCode"`
	FormCode             string `json:"formCode"`
	DocTypeCode          string `json:"docTypeCode"`
	PeriodStart          string `json:"periodStart"`
	PeriodEnd            string `json:"periodEnd"`
	SubmitDateTime       string `json:"submitDateTime"` // Date にしたほうがいいかも
	DocDescription       string `json:"docDescription"` // 資料名
	IssuerEdinetCode     string `json:"issuerEdinetCode"`
	SubjectEdinetCode    string `json:"subjectEdinetCode"`
	SubsidiaryEdinetCode string `json:"subsidiaryEdinetCode"`
	CurrentReportReason  string `json:"currentReportReason"`
	ParentDocID          string `json:"parentDocID"`
	OpeDateTime          string `json:"opeDateTime"` // Date かも
	WithdrawalStatus     string `json:"withdrawalStatus"`
	DocInfoEditStatus    string `json:"docInfoEditStatus"`
	DisclosureStatus     string `json:"disclosureStatus"`
	XbrlFlag             string `json:"xbrlFlag"`
	PdfFlag              string `json:"pdfFlag"`
	AttachDocFlag        string `json:"attachDocFlag"`
	CsvFlag              string `json:"csvFlag"`
	LegalStatus          string `json:"legalStatus"`
}

type Report struct {
	Metadata Meta     `json:"metadata"`
	Results  []Result `json:"results"`
}

/*
EDINET 書類一覧取得 API を使用し有価証券報告書または訂正有価証券報告書のデータを取得する
*/
func GetReports() ([]Result, error) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		fmt.Println("load location error")
		return nil, err
	}

	var results []Result
	date := time.Date(2023, time.January, 1, 1, 0, 0, 0, loc)
	for i := 0; i < 20; i++ {
		var statement Report

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

/*
B/S の値のうち比例縮尺図に使うもの
*/
type Summary struct {
	CompanyName               string     `json:"company_name"`
	PeriodStart               string     `json:"period_start"`
	PeriodEnd                 string     `json:"period_end"`
	UnitString                string     `json:"unit_string"`                  // 単位
	TangibleAssets            TitleValue `json:"tangible_assets"`              // 有形固定資産
	IntangibleAssets          TitleValue `json:"intangible_assets"`            // 無形固定資産
	InvestmentsAndOtherAssets TitleValue `json:"investments_and_other_assets"` // 投資その他の資産
	CurrentLiabilities        TitleValue `json:"current_liabilities"`          // 流動負債
	FixedLiabilities          TitleValue `json:"fixed_liabilities"`            // 固定負債
	NetAssets                 TitleValue `json:"net_assets"`                   // 純資産
}

type PLSummary struct {
	CompanyName     string     `json:"company_name"`
	PeriodStart     string     `json:"period_start"`
	PeriodEnd       string     `json:"period_end"`
	UnitString      string     `json:"unit_string"`
	CostOfGoodsSold TitleValue `json:"cost_of_goods_sold"` // 売上原価
	SGAndA          TitleValue `json:"sg_and_a"`           // 販売費及び一般管理費
	Sales           TitleValue `json:"sales"`              // 売上高
	OperatingProfit TitleValue `json:"operating_profit"`   // 営業利益
	// OperatingLoss             TitleValue `json:"operating_loss"` // 営業損失
}

func RegisterReport(EDINETCode string, docID string, companyName string, periodStart string, periodEnd string) {
	BSFileNamePattern := fmt.Sprintf("%s-%s-BS-from-%s-to-%s", EDINETCode, docID, periodStart, periodEnd)
	PLFileNamePattern := fmt.Sprintf("%s-%s-PL-from-%s-to-%s", EDINETCode, docID, periodStart, periodEnd)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	url := fmt.Sprintf("https://api.edinet-fsa.go.jp/api/v2/documents/%s?type=1&Subscription-Key=%s", docID, EDINETAPIKey)
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("http get error : ", err)
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

	var xbrl XBRL
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
	var summary Summary
	summary.CompanyName = companyName
	summary.PeriodStart = periodStart
	summary.PeriodEnd = periodEnd
	UpdateSummary(doc, &summary)

	// 損益計算書データ
	var plSummary PLSummary
	plSummary.CompanyName = companyName
	plSummary.PeriodStart = periodStart
	plSummary.PeriodEnd = periodEnd
	UpdatePLSummary(plDoc, &plSummary)

	isSummaryValid := ValidateSummary(summary)

	// 貸借対照表バリデーション後
	if isSummaryValid {
		jsonName := fmt.Sprintf("%s.json", BSFileNamePattern)
		jsonPath := fmt.Sprintf("json/%s", jsonName)
		jsonFile, err := os.Create(jsonPath)
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
		jsonFileOpen, err := os.Open(jsonPath)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer jsonFileOpen.Close()

		splitJsonName := strings.Split(jsonName, "-")
		if len(splitJsonName) >= 3 {
			reportType := splitJsonName[2] // BS or PL
			key := fmt.Sprintf("%s/%s/%s", EDINETCode, reportType, jsonName)

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
				uploadDoneMsg := fmt.Sprintf("Successfully uploaded (CompanyName: %s, FileName: %s)", companyName, key)
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
				uploadDoneMsg := fmt.Sprintf("Successfully uploaded (CompanyName: %s, FileName: %s)", companyName, bsHTMLKey)
				fmt.Println(uploadDoneMsg)
			}
		}
	} else {
		invalidSummaryMsg := fmt.Sprintf("Invalid Summary (CompanyName: %s, docID: %s)", companyName, docID)
		fmt.Println(invalidSummaryMsg)
	}

	// 損益計算書バリデーション後
	isPLSummaryValid := ValidatePLSummary(plSummary)

	if isPLSummaryValid {
		jsonName := fmt.Sprintf("%s.json", PLFileNamePattern)
		jsonPath := fmt.Sprintf("json/%s", jsonName)
		jsonFile, err := os.Create(jsonPath)
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
		jsonFileOpen, err := os.Open(jsonPath)
		if err != nil {
			fmt.Println(err)
			return
		}
		splitJsonName := strings.Split(jsonName, "-")
		if len(splitJsonName) >= 3 {
			reportType := splitJsonName[2] // BS or PL
			key := fmt.Sprintf("%s/%s/%s", EDINETCode, reportType, jsonName)

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
				uploadDoneMsg := fmt.Sprintf("Successfully uploaded (CompanyName: %s, FileName: %s)", companyName, key)
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
				uploadDoneMsg := fmt.Sprintf("Successfully uploaded (CompanyName: %s, FileName: %s)", companyName, plHTMLKey)
				fmt.Println(uploadDoneMsg)
			}
		}

	} else {
		invalidSummaryMsg := fmt.Sprintf("Invalid Summary (CompanyName: %s, docID: %s)", companyName, docID)
		fmt.Println(invalidSummaryMsg)
	}
}

func UpdateSummary(doc *goquery.Document, summary *Summary) {
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
			previous := titleTexts[1]
			previousStr := strings.ReplaceAll(previous, ",", "")
			previousInt, prevErr := strconv.Atoi(previousStr)
			current := titleTexts[2]
			currentStr := strings.ReplaceAll(current, ",", "")
			currentInt, currErr := strconv.Atoi(currentStr)

			if prevErr == nil && currErr == nil {
				switch titleName {
				case "有形固定資産合計":
					summary.TangibleAssets.Previous = previousInt
					summary.TangibleAssets.Current = currentInt
				case "無形固定資産合計":
					summary.IntangibleAssets.Previous = previousInt
					summary.IntangibleAssets.Current = currentInt
				case "投資その他の資産合計":
					summary.InvestmentsAndOtherAssets.Previous = previousInt
					summary.InvestmentsAndOtherAssets.Current = currentInt
				case "流動負債合計":
					summary.CurrentLiabilities.Previous = previousInt
					summary.CurrentLiabilities.Current = currentInt
				case "固定負債合計":
					summary.FixedLiabilities.Previous = previousInt
					summary.FixedLiabilities.Current = currentInt
				case "純資産合計":
					summary.NetAssets.Previous = previousInt
					summary.NetAssets.Current = currentInt
				}
			}
		}
		// fmt.Println("titleTexts ❗️: ", titleTexts)
		// fmt.Println("len(splitTdTexts) ❗️: ", len(splitTdTexts))
		// fmt.Println(`strings.Contains(titleTexts[0], "単位：") ❗️: `, strings.Contains(titleTexts[0], "単位："))
		if len(splitTdTexts) == 1 && titleTexts != nil && strings.Contains(titleTexts[0], "単位：") {
			baseStr := splitTdTexts[0]
			baseStr = strings.ReplaceAll(baseStr, "(", "")
			baseStr = strings.ReplaceAll(baseStr, ")", "")
			splitUnitStrs := strings.Split(baseStr, "：")
			if len(splitUnitStrs) >= 2 {
				summary.UnitString = splitUnitStrs[1]
			}
		}
	})
}

func UpdatePLSummary(doc *goquery.Document, plSummary *PLSummary) {
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
			isPreviousMinus := false
			isCurrentMinus := false
			titleName := titleTexts[0]

			// 数字のみのパターン
			re := regexp.MustCompile(`\d+`)

			// 前期
			previous := titleTexts[1]
			previousStr := strings.ReplaceAll(previous, ",", "")
			if strings.Contains(previousStr, "△") {
				isPreviousMinus = true
			}
			previousMatch := re.FindString(previousStr)
			// △ で始まっていたらマイナスにする
			if isPreviousMinus {
				previousMatch = "-" + previousMatch
			}
			previousInt, prevErr := strconv.Atoi(previousMatch)

			// 当期
			current := titleTexts[2]
			currentStr := strings.ReplaceAll(current, ",", "")
			if strings.Contains(currentStr, "△") {
				isCurrentMinus = true
			}
			currentMatch := re.FindString(currentStr)
			// △ で始まっていたらマイナスにする
			if isCurrentMinus {
				currentMatch = "-" + currentMatch
			}
			currentInt, currErr := strconv.Atoi(currentMatch)

			// fmt.Println(fmt.Sprintf("===== %s =====", titleName))
			// fmt.Println("前期: ", previousInt)
			// fmt.Println("後期: ", currentInt)

			if prevErr == nil && currErr == nil {
				switch titleName {
				case "売上原価":
					plSummary.CostOfGoodsSold.Previous = previousInt
					plSummary.CostOfGoodsSold.Current = currentInt
				case "販売費及び一般管理費":
					plSummary.SGAndA.Previous = previousInt
					plSummary.SGAndA.Current = currentInt
				case "売上高":
					plSummary.Sales.Previous = previousInt
					plSummary.Sales.Current = currentInt
					// case "営業利益":
					// 	plSummary.OperatingProfit.Previous = previousInt
					// 	plSummary.OperatingProfit.Current = currentInt
					// case "営業損失":
					// 	plSummary.OperatingLoss.Previous = previousInt
					// 	plSummary.OperatingLoss.Current = currentInt
				}
			}
			if strings.Contains(titleName, "営業利益") {
				plSummary.OperatingProfit.Previous = previousInt
				plSummary.OperatingProfit.Current = currentInt
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

func ValidateSummary(summary Summary) bool {
	if summary.CompanyName != "" &&
		summary.PeriodStart != "" &&
		summary.PeriodEnd != "" &&
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

func ValidatePLSummary(plSummary PLSummary) bool {
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

func RegisterCompany(dynamoClient *dynamodb.Client, EDINETCode string, companyName string) {
  var company internal.Company
  id, uuidErr := uuid.NewUUID()
	if uuidErr != nil {
		fmt.Println("uuid create error")
	}
	company.ID = id.String()
  company.EDINETCode = EDINETCode
  company.Name = companyName

	item, err := attributevalue.MarshalMap(company)
	if err != nil {
		fmt.Println("MarshalMap err: ", err)
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String("compass_companies"),
		Item:      item,
	}
	// TODO: 同名の企業がある場合はDBに追加しない
	result, err := dynamoClient.PutItem(context.TODO(), input)
	if err != nil {
		fmt.Println("dynamoClient.PutItem err: ", err)
	}
	doneMsg := fmt.Sprintf("「%s」result %v ⭐️", companyName, result)
	fmt.Println(doneMsg)
}
