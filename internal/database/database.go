package database

import (
	"gorm.io/gorm"
)

var Db *gorm.DB

// MySQL を使用しないためコメントアウト
// func Connect() {
// 	fmt.Println("start connection to DB")

// 	env := os.Getenv("ENV")
// 	fmt.Println("環境: ", env)

// 	if env == "production" {
// 		envErr := godotenv.Load()
// 		if envErr != nil {
// 			log.Fatal("Error loading .env file")
// 		}

// 		DbUser := os.Getenv("MYSQL_USER")
// 		DbPass := os.Getenv("MYSQL_ROOT_PASSWORD")
// 		DbName := os.Getenv("MYSQL_DATABASE")
// 		DbHost := os.Getenv("MYSQL_HOST")

// 		type Product struct {
// 			gorm.Model
// 			Code  string
// 			Price uint
// 		}

// 		// docker コンテナを立ち上げている場合、ホスト名は 127.0.0.1 ではなくサービス名（db）
// 		dsn := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?charset=utf8mb4&parseTime=True&loc=Local", DbUser, DbPass, DbHost, DbName)
// 		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
// 		if err != nil {
// 			log.Fatal("failed to connect database")
// 		}

// 		fmt.Println("Connected⭐️")
// 		Db = db
// 	}

// }
