package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joe-black-jb/compass-api/internal"
	"github.com/joho/godotenv"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var Users = []internal.User{
	// Password: pass のハッシュ値
	{
		Name:     "サンプルユーザ",
		Password: []byte("$2a$10$lbnP92Wdad2olUA18I1Xbe21Zuma6eoriPCmohCxAku8Bdzo3.SL2"),
		Email:    "sample@sample.com",
		Admin:    false,
	},
}

var Companies = []internal.Company{
	{Name: "ヨネックス株式会社", Established: "1958/06", Capital: "4766"},
	{Name: "美津濃株式会社", Established: "1906/04", Capital: "26137"},
	{Name: "株式会社ユニクロ / UNIQLO CO., LTD.", Established: "1974/09/02"},
}

var Titles = []internal.Title{
	{
		Name:          "流動資産",
		Category:      "資産",
		CompanyID:     1,
		Depth:         1,
		HasValue:      false,
		StatementType: 1,
		FiscalYear:    2023,
	},
	{
		Name:          "固定資産",
		Category:      "資産",
		CompanyID:     1,
		Depth:         1,
		HasValue:      false,
		StatementType: 1,
		FiscalYear:    2023,
		Order:         2,
	},
	{
		Name:          "流動負債",
		Category:      "負債",
		CompanyID:     1,
		Depth:         1,
		HasValue:      false,
		StatementType: 1,
		FiscalYear:    2023,
		Order:         1,
	},
	{
		Name:          "固定負債",
		Category:      "負債",
		CompanyID:     1,
		Depth:         1,
		HasValue:      false,
		StatementType: 1,
		FiscalYear:    2023,
		Order:         2,
	},
	{
		Name:          "株主資本",
		Category:      "純資産",
		CompanyID:     1,
		Depth:         1,
		HasValue:      false,
		StatementType: 1,
		FiscalYear:    2023,
		Order:         1,
	},
	{
		Name:          "有形固定資産",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 2,
		FiscalYear:    2023,
		Order:         1,
		Value:         "21014",
	},
	{
		Name:          "無形固定資産",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 2,
		FiscalYear:    2023,
		Order:         2,
		Value:         "1994",
	},
	{
		Name:          "投資その他の資産",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 2,
		FiscalYear:    2023,
		Order:         3,
		Value:         "2946",
	},
	// 流動資産 配下 (parent_title_id = 1)
	{
		Name:          "現金及び預金",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 1,
		FiscalYear:    2023,
		Order:         1,
		Value:         "16912",
	},
	{
		Name:          "受取手形",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 1,
		FiscalYear:    2023,
		Order:         2,
		Value:         "4410",
	},
	{
		Name:          "売掛金",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 1,
		FiscalYear:    2023,
		Order:         3,
		Value:         "10619",
	},
	{
		Name:          "商品及び製品",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 1,
		FiscalYear:    2023,
		Order:         4,
		Value:         "14871",
	},
	{
		Name:          "仕掛品",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 1,
		FiscalYear:    2023,
		Order:         5,
		Value:         "1941",
	},
	{
		Name:          "原材料及び貯蔵品",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 1,
		FiscalYear:    2023,
		Order:         6,
		Value:         "2019",
	},
	{
		Name:          "その他",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 1,
		FiscalYear:    2023,
		Order:         7,
		Value:         "2757",
	},
	{
		Name:          "貸倒引当金",
		Category:      "資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 1,
		FiscalYear:    2023,
		Order:         8,
		Value:         "-66",
	},
	// 流動負債 配下 (parent_title_id = 3)
	{
		Name:          "支払手形及び買掛金",
		Category:      "負債",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 3,
		FiscalYear:    2023,
		Order:         1,
		Value:         "7128",
	},
	// 固定負債 配下 (parent_title_id = 4)
	{
		Name:          "長期借入金",
		Category:      "負債",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 4,
		FiscalYear:    2023,
		Order:         1,
		Value:         "2373",
	},
	// 株主資本 配下 (parent_title_id = 5)
	{
		Name:          "資本金",
		Category:      "純資産",
		CompanyID:     1,
		Depth:         2,
		HasValue:      true,
		StatementType: 1,
		ParentTitleId: 5,
		FiscalYear:    2023,
		Order:         1,
		Value:         "4706",
	},
}

var CompanyTitles = []internal.CompanyTitle{
	{
		CompanyID: 1,
		TitleID:   1,
	},
	{
		CompanyID: 1,
		TitleID:   2,
	},
	{
		CompanyID: 1,
		TitleID:   3,
	},
	{
		CompanyID: 1,
		TitleID:   4,
	},
	{
		CompanyID: 1,
		TitleID:   5,
	},
	{
		CompanyID: 1,
		TitleID:   6,
	},
	{
		CompanyID: 1,
		TitleID:   7,
	},
	{
		CompanyID: 1,
		TitleID:   8,
	},
	{
		CompanyID: 1,
		TitleID:   9,
	},
	{
		CompanyID: 1,
		TitleID:   10,
	},
	{
		CompanyID: 1,
		TitleID:   11,
	},
	{
		CompanyID: 1,
		TitleID:   12,
	},
	{
		CompanyID: 1,
		TitleID:   13,
	},
	{
		CompanyID: 1,
		TitleID:   14,
	},
	{
		CompanyID: 1,
		TitleID:   15,
	},
	{
		CompanyID: 1,
		TitleID:   16,
	},
	{
		CompanyID: 1,
		TitleID:   17,
	},
	{
		CompanyID: 1,
		TitleID:   18,
	},
	{
		CompanyID: 1,
		TitleID:   19,
	},
	{
		CompanyID: 1,
		TitleID:   20,
	},
	{
		CompanyID: 1,
		TitleID:   21,
	},
	{
		CompanyID: 1,
		TitleID:   22,
	},
	{
		CompanyID: 1,
		TitleID:   23,
	},
	{
		CompanyID: 1,
		TitleID:   24,
	},
	{
		CompanyID: 1,
		TitleID:   25,
	},
	{
		CompanyID: 1,
		TitleID:   26,
	},
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	dbUser := os.Getenv("MYSQL_USER")
	dbPass := os.Getenv("MYSQL_ROOT_PASSWORD")
	dbName := os.Getenv("MYSQL_DATABASE")

	// create database
	initialDsn := fmt.Sprintf("%s:%s@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=True&loc=Local", dbUser, dbPass)
	initialDb, err := gorm.Open(mysql.Open(initialDsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database")
	}
	createDBSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s;", dbName)
	if err := initialDb.Exec(createDBSQL).Error; err != nil {
		log.Fatal("Failed to create database: ", err)
	}
	// docker コンテナを立ち上げている場合、ホスト名は 127.0.0.1 ではなくサービス名（db）
	// コンテナ外でスクリプト実行想定のため、ホスト名は 127.0.0.1 にする
	dsn := fmt.Sprintf("%s:%s@tcp(127.0.0.1:3306)/%s?charset=utf8mb4&parseTime=True&loc=Local", dbUser, dbPass, dbName)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database")
	}

	// Drop Table
	db.Migrator().DropTable(&internal.Company{})
	db.Migrator().DropTable(&internal.Title{})
	db.Migrator().DropTable(&internal.CompanyTitle{})
	db.Migrator().DropTable(&internal.User{})

	// Migrate the schema
	db.AutoMigrate(&internal.Company{}, &internal.Title{}, &internal.CompanyTitle{}, &internal.User{})

	// Create
	// db.Create(&internal.Company{Name: "ヨネックス株式会社", Established: "1958/06", Capital: "4766"})
	// db.Create(&internal.Company{Name: "美津濃株式会社", Established: "1906/04", Capital: "26137"})
	// db.Create(&internal.Company{Name: "株式会社ユニクロ / UNIQLO CO., LTD.", Established: "1974/09/02"})

	// Batch Create
	db.Create(&Companies)
	db.Create(&Titles)
	db.Create(&Users)

	// db.Create(&CompanyTitles)

	for i := range Titles {
		idAddress := &i
		id := *idAddress
		id++
		var CompanyTitle = internal.CompanyTitle{
			CompanyID: 1,
			TitleID:   id,
		}
		db.Create(&CompanyTitle)
	}

	mysql, _ := db.DB()
	mysql.Close()
	fmt.Println("Done!! ⭐️")
}
