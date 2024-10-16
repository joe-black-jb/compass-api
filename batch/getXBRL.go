package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

func main() {
	fmt.Println("Hello XBRL")

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file err: ", err)
	}
	EDINETAPIKey := os.Getenv("EDINET_API_KEY")
	fmt.Println("key : ", EDINETAPIKey)

	// 三井住友DSアセットマネジメント株式会社 のデータを取得
	// まずは書類番号 (/documents/書類番号) を指定
	// S100SNA8
	docID := "S100SNA8"
	// EDINETコード
	EDINETCode := "E08957"
	url := fmt.Sprintf("https://api.edinet-fsa.go.jp/api/v2/documents/%s?type=1&Subscription-Key=%s", docID, EDINETAPIKey)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("http get error : ", err)
	}
	fmt.Println("content-type : ", resp.Header.Get("Content-Type"))
	defer resp.Body.Close()

	// body, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	fmt.Println("io.ReadAll error : ", err)
	// }
	// fmt.Println("body : ", body)
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

	fmt.Println("File downloaded and saved to", path)

	// ZIPファイルを解凍
	unzipDst := filepath.Join(dirPath, docID)
	XBRLFilepath, err := unzip(EDINETCode, path, unzipDst)
	if err != nil {
		fmt.Println("Error unzipping file:", err)
		return
	}
	fmt.Println(XBRLFilepath)

	// ZIPファイルを削除
	// os.Remove(path)

	// XBRLファイルの取得
	// parentPath := filepath.Join("XBRL", docID, XBRLFilepath)
	parentPath := filepath.Join("XBRL", docID, "XBRL", "PublicDoc", "jpsps070000-asr-001_G07493-000_2023-11-06_01_2024-02-01.xml")
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
	// // Chat GPT (ver.1) /////////
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

	// XML全体のルート構造体
	type XBRL struct {
		XMLName          xml.Name         `xml:"xbrl"`
		SchemaRef        SchemaRef        `xml:"schemaRef"`
		Contexts         []Context        `xml:"context"`
		MoneyHeldInTrust MoneyHeldInTrust `xml:"MoneyHeldInTrustCAFND"`
	}
	/////////////////////////////////

	// fmt.Println("string(body): ", string(body))
	var xbrl XBRL
	err = xml.Unmarshal(body, &xbrl)
	if err != nil {
		fmt.Println("XBRL Unmarshal err: ", err)
		return
	}
	fmt.Println("Unmarshal 後: ", xbrl)

	// パース結果の表示
	fmt.Printf("SchemaRef Href: %s\n", xbrl.SchemaRef.Href)
	for _, context := range xbrl.Contexts {
		fmt.Printf("Context ID: %s\n", context.ID)
		fmt.Printf("Entity Identifier Scheme: %s\n", context.Entity.Identifier.Scheme)
		fmt.Printf("Entity Identifier Value: %s\n", context.Entity.Identifier.Value)
		fmt.Printf("Period Instant: %s\n", context.Period.Instant)
	}
	// 結果の表示
	fmt.Printf("Money Held in Trust: %v\n", xbrl.MoneyHeldInTrust)

	// 手動で作成
	text := `
  <?xml version="1.0" encoding="UTF-8"?>
  <xbrli:xbrl xmlns:iso4217="http://www.xbrl.org/2003/iso4217" xmlns:jpdei_cor="http://disclosure.edinet-fsa.go.jp/taxonomy/jpdei/2013-08-31/jpdei_cor" xmlns:jppfs_cor="http://disclosure.edinet-fsa.go.jp/taxonomy/jppfs/2022-11-01/jppfs_cor" xmlns:jpsps_cor="http://disclosure.edinet-fsa.go.jp/taxonomy/jpsps/2022-11-01/jpsps_cor" xmlns:link="http://www.xbrl.org/2003/linkbase" xmlns:xbrldi="http://xbrl.org/2006/xbrldi" xmlns:xbrli="http://www.xbrl.org/2003/instance" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <link:schemaRef xlink:href="jpsps070000-asr-001_G07493-000_2023-11-06_01_2024-02-01.xsd" xlink:type="simple"/>
    <xbrli:context id="FilingDateInstant">
              <xbrli:entity>
                <xbrli:identifier scheme="http://disclosure.edinet-fsa.go.jp">G07493-000</xbrli:identifier>
              </xbrli:entity>
              <xbrli:period>
                <xbrli:instant>2024-02-01</xbrli:instant>
              </xbrli:period>
            </xbrli:context>
    </xbrli:xbrl>
		<jppfs_cor:MoneyHeldInTrustCAFND contextRef="Prior1YearInstant_NonConsolidatedMember" decimals="0" unitRef="JPY">8468659</jppfs_cor:MoneyHeldInTrustCAFND>
  `
	type Sample struct {
		Xml string `xml:"xbrli:xbrl"`
	}
	var sample Sample
	if err := xml.Unmarshal([]byte(text), &sample); err != nil {
		log.Fatal(err)
	}
	fmt.Println("sample: ", sample)

	

	// xmlData := `
	// <?xml version="1.0" encoding="UTF-8"?>
	// <xbrli:xbrl xmlns:iso4217="http://www.xbrl.org/2003/iso4217" xmlns:jpdei_cor="http://disclosure.edinet-fsa.go.jp/taxonomy/jpdei/2013-08-31/jpdei_cor" xmlns:jppfs_cor="http://disclosure.edinet-fsa.go.jp/taxonomy/jppfs/2022-11-01/jppfs_cor" xmlns:jpsps_cor="http://disclosure.edinet-fsa.go.jp/taxonomy/jpsps/2022-11-01/jpsps_cor" xmlns:link="http://www.xbrl.org/2003/linkbase" xmlns:xbrldi="http://xbrl.org/2006/xbrldi" xmlns:xbrli="http://www.xbrl.org/2003/instance" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
	//   <link:schemaRef xlink:href="jpsps070000-asr-001_G07493-000_2023-11-06_01_2024-02-01.xsd" xlink:type="simple"/>
	//   <xbrli:context id="FilingDateInstant">
	// 	<xbrli:entity>
	// 	  <xbrli:identifier scheme="http://disclosure.edinet-fsa.go.jp">G07493-000</xbrli:identifier>
	// 	</xbrli:entity>
	// 	<xbrli:period>
	// 	  <xbrli:instant>2024-02-01</xbrli:instant>
	// 	</xbrli:period>
	//   </xbrli:context>
	// </xbrli:xbrl>
	// `

	// Chat GPT (ver.2) /////////////
	//   // XMLの名前空間とタグに対応する構造体を定義
	// // <jppfs_cor:MoneyHeldInTrustCAFND> タグの構造体
	// type MoneyHeldInTrust struct {
	// 	ContextRef string `xml:"contextRef,attr"`
	// 	Decimals   string `xml:"decimals,attr"`
	// 	UnitRef    string `xml:"unitRef,attr"`
	// 	Value      string `xml:",chardata"`
	// }

	// // <xbrli:unit> タグをパースする構造体
	// type Unit struct {
	// 	ID      string   `xml:"id,attr"`
	// 	Measure string   `xml:"xbrli:measure"`
	// }

	// // <xbrldi:explicitMember> タグの構造体
	// type ExplicitMember struct {
	// 	Dimension string `xml:"dimension,attr"`
	// 	Value     string `xml:",chardata"`
	// }

	// // <xbrli:scenario> タグの構造体
	// type Scenario struct {
	// 	ExplicitMember ExplicitMember `xml:"xbrldi:explicitMember"`
	// }

	//   // <xbrli:period> タグの構造体
	// type Period struct {
	// 	StartDate string `xml:"xbrli:startDate,omitempty"`
	// 	EndDate   string `xml:"xbrli:endDate,omitempty"`
	// 	Instant   string `xml:"xbrli:instant,omitempty"`
	// }

	//   // <xbrli:identifier> タグの構造体
	// type Identifier struct {
	// 	Scheme string `xml:"scheme,attr"`
	// 	Value  string `xml:",chardata"`
	// }
	//   // <xbrli:entity> タグの構造体
	// type Entity struct {
	// 	Identifier Identifier `xml:"xbrli:identifier"`
	// }
	//   // <xbrli:context> タグをパースする構造体
	// type Context struct {
	// 	ID     string   `xml:"id,attr"`
	// 	Entity Entity   `xml:"xbrli:entity"`
	// 	Period Period   `xml:"xbrli:period"`
	// 	Scenario Scenario `xml:"xbrli:scenario"`
	// }
	// type XBRL2 struct {
	// 	// XMLName xml.Name  `xml:"xbrli:xbrl"`
	// 	// XMLName xml.Name  `xml:"xbrli"`
	// 	XMLName xml.Name  `xml:"xbrl"`
	// 	Contexts []Context `xml:"xbrli:context"`
	// 	Units    []Unit    `xml:"xbrli:unit"`
	// 	MoneyHeldInTrust MoneyHeldInTrust `xml:"jppfs_cor:MoneyHeldInTrustCAFND"`
	// }

	// XBRL構造体にXMLをパース
	// var xbrl2 XBRL2
	// 元: []byte(xmlData), 後: []byte(body)
	// if err := xml.Unmarshal([]byte(body), &xbrl2); err != nil {
	// 	log.Fatal(err)
	// }

	// // 実際のXBRLからとる
	// type RealXBRL struct {
	// 	XMLName   xml.Name  `xml:"xbrl"`
	// 	SchemaRef SchemaRef `xml:"schemaRef"`
	// 	Contexts  []Context `xml:"context"`
	// }

	// sample
	type Animal string
	type AnimalsType struct {
		Animal string `xml:"animal"`
	}
	blob := `
	<animals>
		<animal>gopher</animal>
		<animal>armadillo</animal>
		<animal>zebra</animal>
		<animal>unknown</animal>
		<animal>gopher</animal>
		<animal>bee</animal>
		<animal>gopher</animal>
		<animal>zebra</animal>
	</animals>`
	var zoo struct {
		Animals []Animal `xml:"animal"`
	}
	if err := xml.Unmarshal([]byte(blob), &zoo); err != nil {
		log.Fatal(err)
	}
	// fmt.Println("sample Unmarshal 後: ", zoo.Animals)
	// census := make(map[Animal]int)
	// for _, animal := range zoo.Animals {
	// 	census[animal] += 1
	// }

	// fmt.Printf("Zoo Census:\n* Gophers: %d\n* Zebras:  %d\n* Unknown: %d\n",
	// 	census[Gopher], census[Zebra], census[Unknown])

}

func unzip(EDINETCode, source, destination string) (string, error) {
	// ZIPファイルをオープン
	r, err := zip.OpenReader(source)
	if err != nil {
		return "", fmt.Errorf("failed to open zip file: %v", err)
	}
	defer r.Close()

	var XBRLFilepath string

	// ZIP内の各ファイルを処理
	for _, f := range r.File {
		// fmt.Println("f.Name: ", f.Name)
		// ファイル名に EDINETコードが含まれる かつ 拡張子が .xbrl の場合のみ処理する
		extension := filepath.Ext(f.Name)
		// fmt.Println("拡張子 : ", extension)
		underPublic := strings.Contains(f.Name, "PublicDoc")
		// isFiscalStatements := strings.Contains(f.Name, "0105020")
		// hasEDINETCode := strings.Contains(f.Name, EDINETCode)
		// fmt.Println("ファイル名に EDINETCode が含まれていますか❓ : ", hasEDINETCode)

		if underPublic && extension == ".xbrl" {
			fmt.Println("ファイル名 : ", f.Name)

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

			// fmt.Println("fpath : ", fpath)
			outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return "", err
			}

			rc, err := f.Open()
			if err != nil {
				return "", err
			}

			// type XBRL struct {
			//   // BalanceSheetTextBlock string `xml:"jpsps_cor:BalanceSheetTextBlock"`
			//   BalanceSheetTextBlock string `xml:"xbrli:xbrl"`
			// }
			// // outFile: XBRLファイル
			// // TODO: XBRL から jpsps_cor:BalanceSheetTextBlock タグを取得する
			// body, err := io.ReadAll(rc)
			// if err != nil {
			//   fmt.Println("io.ReadAll err: ", err)
			// }
			// // fmt.Println("string(body): ", string(body))
			// var xbrl XBRL
			// err = xml.Unmarshal(body, &xbrl)
			// if err != nil {
			//   fmt.Println("Unmarshal err: ", err)
			// }
			// fmt.Println("Unmarshal後: ", xbrl)

			// var xbrl XBRL
			// decoder := xml.NewDecoder(rc)
			// err = decoder.Decode(&xbrl)
			// if err != nil {
			//   fmt.Printf("XMLのデコード中にエラーが発生しました: %v\n", err)
			// }
			// // <jpsps_cor:BalanceSheetTextBlock>タグの内容を表示
			// fmt.Println("デコード後: ", xbrl)
			// fmt.Println("BalanceSheetTextBlockの内容:")
			// fmt.Println(xbrl.BalanceSheetTextBlock)

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
	return XBRLFilepath, nil
}
