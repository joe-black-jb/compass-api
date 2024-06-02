package api

import (
	"time"

	"github.com/conmass-api/internal/api/companies"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
) 

func Router() {
	router := gin.Default()
	// trustedProxies := []string {"http://localhost:3000"}
	// router.SetTrustedProxies(trustedProxies)
	router.Use(cors.New(cors.Config{
		AllowOrigins: []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return origin == "https://github.com"
		},
		MaxAge: 12 * time.Hour,
	}))
	// config := cors.DefaultConfig()
	// config.AllowOrigins = []string{"http://localhost:3000"}
	// router.Use(cors.New(config))

	router.GET("/companies", companies.GetCompanies)

	err := router.Run("localhost:8080"); 
	if err != nil {
		panic(err)
	}
}