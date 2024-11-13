package api

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func GetCompaniesGin(c *gin.Context) {
	limit := c.Query("limit")
	companies, err := GetCompaniesProcessor(limit)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, err.Error())
	}
	c.IndentedJSON(http.StatusOK, companies)
}

func GetCompanyGin(c *gin.Context) {
	companyId := c.Query("companyId")
	company, err := GetCompanyProcessor(companyId)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, err.Error())
	}
	c.IndentedJSON(http.StatusOK, company)
}

func SearchCompaniesByNameGin(c *gin.Context) {
	companyName := c.Query("companyName")
	companies, err := SearchCompaniesByNameProcessor(companyName)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, err.Error())
	}
	c.IndentedJSON(http.StatusOK, companies)
}

func GetReportsGin(c *gin.Context) {
	EDINETCode := c.Query("EDINETCode")
	reportType := c.Query("reportType")
	extension := c.Query("extension")

	reportData, err := GetReportsProcessor(EDINETCode, reportType, extension)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, err.Error())
	}
	c.IndentedJSON(http.StatusOK, reportData)
}

func GetFundamentalsGin(c *gin.Context) {
	EDINETCode := c.Query("EDINETCode")
	bucketName := os.Getenv("BUCKET_NAME")
	// プレフィックス (ディレクトリのようなもの)
	prefix := fmt.Sprintf("%s/Fundamentals", EDINETCode)
	fundamentals, err := GetFundamentalsProcessor(EDINETCode, bucketName, prefix)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, err.Error())
	}
	c.IndentedJSON(http.StatusOK, fundamentals)
}

func GetLatestNewsGin(c *gin.Context) {
	newsBucketName := os.Getenv("NEWS_BUCKET_NAME")

	result, err := GetLatestNewsProcessor(newsBucketName, latestFileKey)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, err.Error())
	}
	c.IndentedJSON(http.StatusOK, result)
}
