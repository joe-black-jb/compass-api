package internal

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Name     string
	Email    string `gorm:"unique"`
	Password []byte
	Admin    bool
}

type Company struct {
	ID            string    `gorm:"primaryKey" json:"id" dynamodbav:"id"`
	CreatedAt     time.Time `json:"createdAt" dynamodbav:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt" dynamodbav:"updatedAt"`
	Name          string `gorm:"unique"  dynamodbav:"name"`
	Established   string
	Capital       string
	Titles        []*Title        `gorm:"many2many:company_titles;"`
	CompanyTitles []*CompanyTitle `gorm:"foreignKey:CompanyID"`
	EDINETCode    string `gorm:"edinet_code"`
}

type Title struct {
	gorm.Model
	Name          string `gorm:"type:varchar(255);uniqueIndex:name_company_unique"`
	Category      string
	CompanyID     int `gorm:"type:varchar(255);uniqueIndex:name_company_unique"`
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

type CreateTitleBody struct {
	Name          *string
	Category      *string
	CompanyID     *int
	Depth         *int
	HasValue      *bool
	StatementType *int
	Order         *int
	FiscalYear    *int
	Value         *string
	ParentTitleId *int
}

type Error struct {
	Status  int
	Message string
}

type Ok struct {
	Status  int
	Message string
}

type Credentials struct {
	Email    string
	Password string
}

type RegisterUserBody struct {
	Name     *string
	Email    *string
	Password *string
}

type Login struct {
	Username string
	Token    string
}

type HTMLData struct {
	FileName string `json:"file_name"`
	Data string `json:"data"`
}
