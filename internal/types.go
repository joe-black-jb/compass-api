package internal

import (
	"gorm.io/gorm"
)

type Company struct {
	gorm.Model
	Name          string
	Established   string
	Capital       string
	Titles        []*Title        `gorm:"many2many:company_titles;"`
	CompanyTitles []*CompanyTitle `gorm:"foreignKey:CompanyID"`
}

type Title struct {
	gorm.Model
	Name          string
	Category      string
	CompanyID     int
	Depth         int
	HasValue      bool
	StatementType int
	Order         int `json:"order" gorm:"default:null"`
	FiscalYear    int
	Value         string
	ParentTitleId int             `json:"parent_title_id" gorm:"default:null"`
	Companies     []*Company      `gorm:"many2many:company_titles;"`
	CompanyTitles []*CompanyTitle `gorm:"foreignKey:TitleID"`
}

type CompanyTitle struct {
	gorm.Model
	CompanyID int `gorm:"primaryKey"`
	TitleID   int `gorm:"primaryKey"`
	Value     string
	Company   Company `gorm:"foreignKey:CompanyID"`
	Title     Title   `gorm:"foreignKey:TitleID"`
}

type UpdateCompanyTitleParams struct {
	Name  string
	Value string
}
