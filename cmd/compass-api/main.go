package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joe-black-jb/compass-api/internal/api"
	"github.com/joe-black-jb/compass-api/internal/database"
	"github.com/joho/godotenv"
)

var dynamoClient *dynamodb.Client
var s3Client *s3.Client

func init() {
	fmt.Println("init ⭐️")

	env := os.Getenv("ENV")

	if env == "local" {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file err: ", err)
			return
		}
	}
	region := os.Getenv("REGION")

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		fmt.Println("Load default config error: %v", err)
		return
	}
	dynamoClient = dynamodb.NewFromConfig(cfg)

	s3Client = s3.NewFromConfig(cfg)
}

func main() {
	fmt.Println("main ⭐️")
	// DB接続
	database.Connect()

	// // ルーター起動 (gin を使用する場合)
	// api.Router()

	// ハンドラー関数実行 (Lambda を使用する場合)
	lambda.Start(handler)
}

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	fmt.Printf("Received request: %v\n", req)

	path := req.PathParameters["path"]
  companyId := req.PathParameters["companyId"]

  if companyId != "" {
    fmt.Println("get company route")
		return api.GetCompany(req, dynamoClient)
  }

	// Routing
	switch path {
	case "companies":
		fmt.Println("companies route")
		return api.GetCompanies(req, dynamoClient)
	case "search":
		fmt.Println("search companies route")
		return api.SearchCompaniesByName(req, dynamoClient)
	// case "company":
	// 	fmt.Println("search company route")
	// 	return api.GetCompany(req, dynamoClient)
	case "reports":
		fmt.Println("search reports route")
		return api.GetReports(req, dynamoClient)
	case "fundamentals":
		fmt.Println("search fundamentals route")
		return api.GetFundamentals(req, dynamoClient)
	default:
		fmt.Println("default")
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       string("OK"),
	}, nil
}
