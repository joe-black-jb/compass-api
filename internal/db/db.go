package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

var db *sql.DB

func Connect() {
	enverr := godotenv.Load()
  if enverr != nil {
    log.Fatal("Error loading .env file")
  }

	cfg := mysql.Config{
		User: os.Getenv("DBUSER"),
		Passwd: os.Getenv("DBPASS"),
		Net:    "tcp",
    Addr:   "127.0.0.1:3306",
    DBName: "compass",
		AllowNativePasswords: true,
	}

	var err error
	db, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatal(err)
	}

	// TODO ping を通す (通らなくても DB と接続できることは確認済み)
	// pingErr := db.Ping()
	// if pingErr != nil {
	// 	log.Fatal(pingErr)
	// }
	fmt.Println("Connected⭐️")

}