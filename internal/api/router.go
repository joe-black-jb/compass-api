package api

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func Router() {
	router := gin.Default()
	// trustedProxies := []string {"http://localhost:3000"}
	// router.SetTrustedProxies(trustedProxies)
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		AllowOriginFunc: func(origin string) bool {
			return origin == "https://github.com"
		},
		MaxAge: 12 * time.Hour,
	}))

	// リクエスト内容をログ出力
	// router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {	
	// 	return fmt.Sprintf("%s - [%s] \"%s %s %s %d %s \"%s\" %s\"%s\"\n",
	// 		param.ClientIP,
	// 		param.TimeStamp.Format(time.RFC1123),
	// 		param.Method,
	// 		param.Path,
	// 		param.Request.Proto,
	// 		param.StatusCode,
	// 		param.Latency,
	// 		param.Request.UserAgent(),
	// 		param.ErrorMessage,
	// 		param.Request.Header.Get("x-api-key"),
	// 	)
	// }))

	// router.Use()

	// config := cors.DefaultConfig()
	// config.AllowOrigins = []string{"http://localhost:3000"}
	// router.Use(cors.New(config))

	router.GET("/companies", GetCompanies)
	router.GET("/titles", GetTitles)
	// router.GET("/company/:id/with-title", GetCompanyTitles)
	router.GET("/company/:id/titles", GetCompanyTitles)
	router.GET("/company/:id", GetCompany)
	router.PUT("/company/:id/title/:titleId", UpdateCompanyTitles)

	// localhost だと Docker コンテナを立ち上げ外部からリクエストを受けることができないため
	// 0.0.0.0 に変更
	// err := router.Run("localhost:8080");
	err := router.Run("0.0.0.0:8080")
	if err != nil {
		panic(err)
	}
}
