package internal

import (
	"gorm.io/gorm"
)

type Company struct {
	gorm.Model
	Name  string
	Established string
	Capital string
	Titles []*Title `gorm:"many2many:company_titles;"`
}

type Title struct {
	gorm.Model
	Name  string
	Category string
	CompanyID int
	Depth int
	HasValue bool
	StatementType int
	Order int `json:"order" gorm:"default:null"`
	FiscalYear int
	Value string
	ParentTitleId int `json:"parent_title_id" gorm:"default:null"`
}

type CompanyTitle struct {
	gorm.Model
	CompanyID  int `gorm:"primaryKey"`
	TitleID int `gorm:"primaryKey"`
	Value string
}