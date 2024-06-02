package api

import (
	"github.com/conmass-api/internal/api/companies"
	"github.com/gin-gonic/gin"
) 

func Router() {
	router := gin.Default()
	router.GET("/companies", companies.GetCompanies)

	router.Run("localhost:8080")
}