package api

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joe-black-jb/compass-api/internal"
	"golang.org/x/text/width"
)

func ConvertTitleBody(title *internal.Title, reqBody *internal.CreateTitleBody) (errors []string, ok bool) {
	// 必須項目: 区分、会社ID、項目名、親項目ID
	if reqBody.Category == nil {
		errors = append(errors, "区分")
	}
	if reqBody.CompanyID == nil {
		errors = append(errors, "会社ID")
	}
	if reqBody.Name == nil {
		errors = append(errors, "項目名")
	}
	if reqBody.ParentTitleId == nil {
		errors = append(errors, "親項目ID")
	}
	if len(errors) > 0 {
		return errors, false
	}
	if reqBody.Depth == nil {
		defaultDepth := 1
		reqBody.Depth = &defaultDepth
	}
	if reqBody.HasValue == nil {
		defaultHasValue := true
		reqBody.HasValue = &defaultHasValue
	}
	if reqBody.StatementType == nil {
		defaultStatementType := 1
		reqBody.StatementType = &defaultStatementType
	}
	if reqBody.FiscalYear == nil {
		defaultFiscalYear := 2023
		reqBody.FiscalYear = &defaultFiscalYear
	}
	if reqBody.Order == nil {
		defaultOrder := 99
		reqBody.Order = &defaultOrder
	}
	title.Category = *reqBody.Category
	title.CompanyID = *reqBody.CompanyID
	title.Name = *reqBody.Name
	title.ParentTitleId = *reqBody.ParentTitleId
	title.Depth = *reqBody.Depth
	title.HasValue = *reqBody.HasValue
	title.StatementType = *reqBody.StatementType
	title.FiscalYear = *reqBody.FiscalYear
	title.Order = *reqBody.Order
	if reqBody.Value != nil {
		title.Value = *reqBody.Value
	}
	return nil, true
}

func ConvertUpdateTitleBody(reqBody *internal.CreateTitleBody) (errors []string, result map[string]interface{}) {
	updates := make(map[string]interface{})
	if reqBody.Name != nil {
		updates["Name"] = *reqBody.Name
	}
	if reqBody.Category != nil {
		updates["Category"] = *reqBody.Category
	}
	if reqBody.ParentTitleId != nil {
		updates["ParentTitleId"] = *reqBody.ParentTitleId
	}
	if reqBody.CompanyID != nil {
		updates["CompanyID"] = *reqBody.CompanyID
	}
	if reqBody.Value != nil {
		updates["Value"] = *reqBody.Value
	}
	return nil, updates
}

/*
正常系

	10,897,603

異常系

	※1 10,897,603
	※1,※2 10,897,603
*/
func ConvertTextValue2IntValue(text string) (int, error) {
	isMinus := false
	// fmt.Println("==========================")
	// fmt.Println("元text: ", text)

	// , を削除
	text = strings.ReplaceAll(text, ",", "")
	// ※1 などを削除
	asteriskAndHalfWidthNums := AsteriskAndHalfWidthNumRe.FindAllString(text, -1)
	for _, asteriskAndHalfWidthNum := range asteriskAndHalfWidthNums {
		text = strings.ReplaceAll(text, asteriskAndHalfWidthNum, "")
	}
	// マイナスチェック
	if strings.Contains(text, "△") {
		isMinus = true
	}
	// 数字部分のみ抜き出す
	text = OnlyNumRe.FindString(text)
	// スペースを削除
	text = strings.TrimSpace(text)
	// マイナスの場合、 - を先頭に追加する
	if isMinus {
		// previousMatch = "-" + previousMatch
		text = "-" + text
	}
	intValue, err := strconv.Atoi(text)
	if err != nil {
		// fmt.Println("strconv.Atoi error text: ", text)
		return 0, err
	}
	return intValue, nil
}

func QueryByName(svc *dynamodb.Client, tableName string, companyName string, edinetCode string) ([]map[string]types.AttributeValue, error) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		IndexName:              aws.String("CompanyNameIndex"), // GSIを指定
		KeyConditionExpression: aws.String("#n = :name AND #e = :edinetCode"),
		ExpressionAttributeNames: map[string]string{
			"#n": "name",       // `name`をエイリアス
			"#e": "edinetCode", // `edinetCode`をエイリアス
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":name":       &types.AttributeValueMemberS{Value: companyName},
			":edinetCode": &types.AttributeValueMemberS{Value: edinetCode},
		},
	}

	// クエリの実行
	result, err := svc.Query(context.TODO(), input)
	if err != nil {
		return nil, err
	}

	return result.Items, nil
}

func ScanCompaniesByName(svc *dynamodb.Client, tableName string, companyName string) ([]internal.Company, error) {
	targetNames := []string{companyName}
	// 半角文字の存在チェック
	halfWidthPattern := `[a-zA-Z0-9]`
	re := regexp.MustCompile(halfWidthPattern)
	if re.MatchString(companyName) {
		// 半角英数字を全角英数字に変換
		fullWidthCompanyName := width.Widen.String(companyName)
		targetNames = append(targetNames, fullWidthCompanyName)
	}

	var resultItems []map[string]types.AttributeValue
	for _, name := range targetNames {
		// フィルタ式とプレースホルダの設定
		filterExpression := "contains(#name, :companyName)"
		expressionAttributeNames := map[string]string{
			"#name": "name",
		}
		expressionAttributeValues := map[string]types.AttributeValue{
			":companyName": &types.AttributeValueMemberS{Value: name},
		}

		// Scan入力パラメータの設定
		input := &dynamodb.ScanInput{
			TableName:                 aws.String(tableName), // テーブル名を設定
			FilterExpression:          aws.String(filterExpression),
			ExpressionAttributeNames:  expressionAttributeNames,
			ExpressionAttributeValues: expressionAttributeValues,
		}

		// Scanの実行
		result, err := svc.Scan(context.TODO(), input)
		if err != nil {
			return nil, err
		}
		resultItems = append(resultItems, result.Items...)
	}

	var companies []internal.Company
	err := attributevalue.UnmarshalListOfMaps(resultItems, &companies)
	if err != nil {
		fmt.Println("unMarshal err: ", err)
		return nil, err
	}
	return companies, nil
}

/*
	S3 に指定したキーが存在するかチェックする

return: 存在すれば true, 存在しなければ false
*/
func CheckExistsS3Key(s3Client *s3.Client, bucketName string, key string) bool {
	// ファイルの存在チェック
	output, _ := s3Client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucketName),
		Prefix:  aws.String(key),
		MaxKeys: aws.Int32(1),
	})
	// fmt.Printf("%s/%s の存在チェック結果: %v\n", bucketName, key, existsFile)

	return len(output.Contents) > 0
}

func GetS3Object(s3Client *s3.Client, bucketName string, key string) (*s3.GetObjectOutput, error) {
	output, err := s3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	return output, nil
}

func ListS3Objects(s3Client *s3.Client, bucketName string, key string) *s3.ListObjectsV2Output {
	output, _ := s3Client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(key),
	})
	return output
}
