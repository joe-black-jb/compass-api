package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var db *sql.DB

func Connect() {
	enverr := godotenv.Load()
  if enverr != nil {
    log.Fatal("Error loading .env file")
  }

	type Product struct {
		gorm.Model
		Code  string
		Price uint
	}

	dbuser := os.Getenv("MYSQL_USER")
	dbpass := os.Getenv("MYSQL_ROOT_PASSWORD")
	dbname := os.Getenv("MYSQL_DATABASE")
	dbhost := os.Getenv("MYSQL_HOST")
	// docker コンテナを立ち上げている場合、ホスト名は 127.0.0.1 ではなくサービス名（db）
	dsn := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?charset=utf8mb4&parseTime=True&loc=Local", dbuser, dbpass, dbhost, dbname)
	_, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
    log.Fatal("failed to connect database")
  }

	fmt.Println("Connected⭐️")
}