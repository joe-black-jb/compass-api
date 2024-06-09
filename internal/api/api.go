package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joe-black-jb/compass-api/internal"
	"github.com/joe-black-jb/compass-api/internal/database"
)

func GetCompanies (c *gin.Context) {
	Companies := &[]internal.Company{}
	if err := database.Db.Find(Companies).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	c.IndentedJSON(http.StatusOK, Companies)
}

func GetCompany (c *gin.Context) {
	Id := c.Param("id")
	Company := &internal.Company{}
	if err := database.Db.First(Company, Id).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	c.IndentedJSON(http.StatusOK, Company)
}

func GetTitles (c *gin.Context) {
	Titles := &[]internal.Title{}
	if err := database.Db.Find(Titles).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	c.IndentedJSON(http.StatusOK, Titles)
}

func GetCompanyTitles (c *gin.Context) {
	Id := c.Param("id")
	fmt.Println("パラメータのID: ", Id)
	Company := &internal.Company{}
	if err := database.Db.Preload("Titles").First(Company, Id); err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	fmt.Println("ID 1 の会社が持つ項目: ", Company)
	c.IndentedJSON(http.StatusOK, Company)
}