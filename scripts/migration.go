package main

import (
	"fmt"
	"log"
	"os"

	"github.com/compass-api/internal"
	"github.com/joho/godotenv"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var Ptiltes = []internal.Title{
	{
		Name: "流動資産", 
		Category: "資産", 
		CompanyID: 1, 
		Depth: 1, 
		HasValue: false,
		StatementType: 1,
		FiscalYear: 2023,
	},
	{
		Name: "固定資産", 
		Category: "資産", 
		CompanyID: 1, 
		Depth: 1, 
		HasValue: false,
		StatementType: 1,
		FiscalYear: 2023,
		Order: 2,
	},
	{
		Name: "流動負債", 
		Category: "負債", 
		CompanyID: 1, 
		Depth: 1, 
		HasValue: false,
		StatementType: 1,
		FiscalYear: 2023,
		Order: 1,
	},
	{
		Name: "固定負債", 
		Category: "負債", 
		CompanyID: 1, 
		Depth: 1, 
		HasValue: false,
		StatementType: 1,
		FiscalYear: 2023,
		Order: 2,
	},
	{
		Name: "株主資本", 
		Category: "純資産", 
		CompanyID: 1, 
		Depth: 1, 
		HasValue: false,
		StatementType: 1,
		FiscalYear: 2023,
		Order: 1,
	},
}

var Ctitles = []internal.Title{
	{
		Name: "有形固定資産",
		Category: "資産",
		CompanyID: 1,
		Depth: 2,
		HasValue: false,
		StatementType: 1,
		ParentTitleId: 2,
		FiscalYear: 2023,
		Order: 1,
	},
	{
		Name: "無形固定資産",
		Category: "資産",
		CompanyID: 1,
		Depth: 2,
		HasValue: false,
		StatementType: 1,
		ParentTitleId: 2,
		FiscalYear: 2023,
		Order: 2,
	},
	{
		Name: "投資その他の資産",
		Category: "資産",
		CompanyID: 1,
		Depth: 2,
		HasValue: false,
		StatementType: 1,
		ParentTitleId: 2,
		FiscalYear: 2023,
		Order: 3,
	},
}

var Gchildtitles = []internal.Title{
	{
		Name: "投資有価証券",
		Category: "資産",
		CompanyID: 1,
		Depth: 3,
		HasValue: false,
		StatementType: 1,
		ParentTitleId: 8,
		FiscalYear: 2023,
		Order: 1,
	},
}

var Companytitles = []internal.CompanyTitle{
	{
		CompanyID: 1,
		TitleID: 1,
		Value: 1000,
	},
	{
		CompanyID: 1,
		TitleID: 2,
		Value: 2000,
	},
	{
		CompanyID: 1,
		TitleID: 3,
		Value: 500,
	},
	{
		CompanyID: 1,
		TitleID: 4,
		Value: 600,
	},
	{
		CompanyID: 1,
		TitleID: 5,
		Value: 1900,
	},
}

// TODO migration.go をスクリプトで実行できるようにする
func main() {
	enverr := godotenv.Load()
  if enverr != nil {
    log.Fatal("Error loading .env file")
  }

	dbuser := os.Getenv("MYSQL_USER")
	dbpass := os.Getenv("MYSQL_ROOT_PASSWORD")
	dbname := os.Getenv("MYSQL_DATABASE")
	// docker コンテナを立ち上げている場合、ホスト名は 127.0.0.1 ではなくサービス名（db）
	// コンテナ外でスクリプト実行想定のため、ホスト名は 127.0.0.1 にする
	dsn := fmt.Sprintf("%s:%s@tcp(127.0.0.1:3306)/%s?charset=utf8mb4&parseTime=True&loc=Local", dbuser, dbpass, dbname)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
    log.Fatal("failed to connect database")
  }
	
	// Drop Table
	db.Migrator().DropTable(&internal.Company{})
	db.Migrator().DropTable(&internal.Title{})
	db.Migrator().DropTable(&internal.CompanyTitle{})
	
	// Migrate the schema
  db.AutoMigrate(&internal.Company{}, &internal.Title{}, &internal.CompanyTitle{})

  // Create
  db.Create(&internal.Company{Name: "ヨネックス"})
  db.Create(&internal.Company{Name: "ミズノ"})

	// Batch Create
  db.Create(&Ptiltes)
  db.Create(&Ctitles)
  db.Create(&Gchildtitles)
  db.Create(&Companytitles)

	mysql, _ := db.DB()
	mysql.Close()
	fmt.Println("Done!! ⭐️")
}