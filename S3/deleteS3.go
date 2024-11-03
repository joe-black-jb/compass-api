package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joho/godotenv"
)

func main() {
	start := time.Now()

	env := os.Getenv("ENV")

	if env == "local" {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file err: ", err)
			return
		}
	}
	region := os.Getenv("REGION")
	sdkConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		fmt.Println(err)
		return
	}
	s3Client := s3.NewFromConfig(sdkConfig)
	bucketName := os.Getenv("BUCKET_NAME")

	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(""),
	})

	var wg sync.WaitGroup

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.TODO())
		if err != nil {
			fmt.Println("paginator.NextPage error: ", err)
			return
		}

		for _, object := range output.Contents {
			wg.Add(1)
			key := object.Key
			go DeleteS3Object(s3Client, bucketName, *key, &wg)
		}
	}
	wg.Wait()
	fmt.Printf("処理完了 (所要時間: %v) ⭐️", time.Since(start))
}

func DeleteS3Object(s3Client *s3.Client, bucketName string, key string, wg *sync.WaitGroup) {
	defer wg.Done()

	deleteInput := &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	}
	_, err := s3Client.DeleteObject(context.TODO(), deleteInput)
	if err != nil {
		fmt.Println("S3 deleteObject error: ", err)
	}
	fmt.Printf("%s を削除しました\n", key)
}
