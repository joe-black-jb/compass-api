package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joe-black-jb/compass-api/internal"
)

func GetCompaniesProcessor(limit string) ([]internal.Company, error) {
	fmt.Println("==== GetCompanies ====")
	var companies []internal.Company
	// pagination 用
	var lastEvaluatedKey map[string]types.AttributeValue

	if limit == "" {
		for {
			scanInput := &dynamodb.ScanInput{
				TableName: aws.String("compass_companies"),
				Limit:     aws.Int32(50),
			}
			if lastEvaluatedKey != nil {
				scanInput.ExclusiveStartKey = lastEvaluatedKey
			}
			result, err := dynamoClient.Scan(context.TODO(), scanInput)
			if err != nil {
				fmt.Println("scan err: ", err)
				return nil, err
			}

			var batch []internal.Company
			// 取得したアイテムを Company 構造体に変換
			err = attributevalue.UnmarshalListOfMaps(result.Items, &batch)
			if err != nil {
				fmt.Println("unMarshal err: ", err)
				return nil, err
			}

			companies = append(companies, batch...)

			if result.LastEvaluatedKey == nil {
				break
			}
			lastEvaluatedKey = result.LastEvaluatedKey
		}
	} else {
		limitInt, err := strconv.Atoi(limit)
		if err != nil {
			return nil, err
		}
		limitInt32 := int32(limitInt)
		scanInput := &dynamodb.ScanInput{
			TableName: aws.String("compass_companies"),
			Limit:     aws.Int32(limitInt32),
		}
		result, err := dynamoClient.Scan(context.TODO(), scanInput)
		if err != nil {
			fmt.Println("scan err: ", err)
			return nil, err
		}

		var batch []internal.Company
		// 取得したアイテムを Place 構造体に変換
		err = attributevalue.UnmarshalListOfMaps(result.Items, &batch)
		if err != nil {
			fmt.Println("unMarshal err: ", err)
			return nil, err
		}
		companies = append(companies, batch...)
	}

	return companies, nil
}

func GetCompanyProcessor(companyId string) (internal.Company, error) {
	var company internal.Company

	getItemInput := &dynamodb.GetItemInput{
		TableName: aws.String("compass_companies"),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: companyId}, // 取得したい id の値を指定
		},
	}
	getItemOutput, err := dynamoClient.GetItem(context.TODO(), getItemInput)
	if err != nil {
		getItemNgMsg := fmt.Sprintf("「%s」getItem error: %v", company.Name, err)
		fmt.Println(getItemNgMsg)
		return internal.Company{}, err
	}
	fmt.Println("getItemOutput ⭐️: ", getItemOutput)
	err = attributevalue.UnmarshalMap(getItemOutput.Item, &company)
	if err != nil {
		return internal.Company{}, err
	}
	return company, nil
}

func SearchCompaniesByNameProcessor(companyName string) ([]internal.Company, error) {
	if companyName == "" {
		return nil, errors.New("企業名を指定してください")
	}
	companies, err := ScanCompaniesByName(dynamoClient, "compass_companies", companyName)
	if err != nil {
		return nil, err
	}
	return companies, nil
}

func GetReportsProcessor(EDINETCode string, reportType string, extension string) ([]internal.ReportData, error) {
	// S3 から BS HTML 一覧を取得
	bucketName := os.Getenv("BUCKET_NAME")
	// プレフィックス (ディレクトリのようなもの)
	prefix := fmt.Sprintf("%s/", EDINETCode)

	// ListObjectsV2Inputを使って、特定のプレフィックスにマッチするオブジェクトをリストアップ
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	}

	// オブジェクト一覧を取得
	result, err := s3Client.ListObjectsV2(context.TODO(), input)
	if err != nil {
		log.Fatalf("failed to list objects, %v", err)
	}

	var keys []string
	// .htmlファイルだけをフィルタリング
	for _, item := range result.Contents {
		key := *item.Key
		if extension == "html" {
			if strings.HasSuffix(key, ".html") {
				splitFileName := strings.Split(key, "/")
				if len(splitFileName) >= 2 {
					fileType := splitFileName[1] // BS or PL
					if (reportType == "BS" && fileType == "BS") || (reportType == "PL" && fileType == "PL") || (reportType == "CF" && fileType == "CF") {
						keys = append(keys, key)
					}
				}
			}
		}
		if extension == "json" {
			if strings.HasSuffix(key, ".json") {
				splitFileName := strings.Split(key, "/")
				if len(splitFileName) >= 2 {
					fileType := splitFileName[1] // BS or PL
					if (reportType == "BS" && fileType == "BS") || (reportType == "PL" && fileType == "PL") || (reportType == "CF" && fileType == "CF") {
						keys = append(keys, key)
					}
				}
			}
		}
	}
	// fmt.Println("取得対象ファイル: ", keys)

	var reportData []internal.ReportData
	// レポートファイルの中身を取得
	for _, key := range keys {
		input := &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(key),
		}
		// オブジェクトを取得
		result, err := s3Client.GetObject(context.TODO(), input)
		if err != nil {
			log.Fatalf("failed to get object, %v", err)
		}
		// fmt.Println("取得した object: ", result)
		fmt.Println("取得した object の Body: ", result.Body)
		body, err := io.ReadAll(result.Body)
		if err != nil {
			fmt.Println("io.ReadAll err: ", err)
			return nil, err
		}
		var data internal.ReportData
		data.FileName = key
		data.Data = string(body)
		reportData = append(reportData, data)
	}
	return reportData, nil
}

func GetFundamentalsProcessor(EDINETCode string, bucketName string, prefix string) ([]internal.Fundamental, error) {
	// ListObjectsV2Inputを使って、特定のプレフィックスにマッチするオブジェクトをリストアップ
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	}

	// オブジェクト一覧を取得
	result, err := s3Client.ListObjectsV2(context.TODO(), input)
	if err != nil {
		// log.Fatalf("failed to list objects, %v", err)
		fmt.Println("failed to list fundamentals objects: ", err)
	}

	// var keys []string
	// .htmlファイルだけをフィルタリング
	var fundamentals []internal.Fundamental
	for _, item := range result.Contents {
		var fundamental internal.Fundamental
		key := *item.Key
		fmt.Println("getObject key: ", key)
		// key を指定し json ファイルを取得
		result, err := s3Client.GetObject(context.TODO(), &s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(key),
		})
		if err != nil {
			fmt.Println("s3 getObject err: ", err)
			return nil, err
		}
		body, err := io.ReadAll(result.Body)
		if err != nil {
			fmt.Println("getObject io.ReadAll err: ", err)
			return nil, err
		}
		fmt.Println("string(body): ", string(body))
		err = json.Unmarshal(body, &fundamental)
		if err != nil {
			fmt.Println("s3 getObject Unmarshal err: ", err)
			return nil, err
		}
		fundamentals = append(fundamentals, fundamental)
	}
	return fundamentals, nil
}

func GetLatestNewsProcessor(newsBucketName string, latestFileKey string) (string, error) {
	output, err := GetS3Object(s3Client, newsBucketName, latestFileKey)
	if err != nil {
		return "", err
	}
	if output == nil {
		return "", errors.New("No Data")
	}
	body, err := io.ReadAll(output.Body)
	if err != nil {
		return "", err
	}
	defer output.Body.Close()

	var newsData internal.NewsResult
	err = json.Unmarshal(body, &newsData)
	if err != nil {
		return "", err
	}

	jsonBody, err := json.MarshalIndent(newsData, "", "  ")
	if err != nil {
		return "", err
	}
	return string(jsonBody), nil
}
