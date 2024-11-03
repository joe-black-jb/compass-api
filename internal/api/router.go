package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// JWT認証ミドルウェア
func AuthMiddleware() gin.HandlerFunc {
	var jwtSecret = os.Getenv("SECRET_KEY")
	// var jwtSecret = []byte(os.Getenv("SECRET_KEY"))
	return func(c *gin.Context) {
		// Authorizationヘッダーからトークンを取得
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		// Bearer 部分を除去しトークンを取得
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token format"})
			c.Abort()
			return
		}

		// トークンの検証
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// 署名方法の確認
			if token.Method.Alg() != "HS256" {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})
		if err != nil {
			fmt.Println("err ❗️ : ", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			// c.JSON(http.StatusUnauthorized, err.Error())
			c.JSON(http.StatusUnauthorized, token)
			c.Abort()
			return
		}

		// トークンが有効か確認
		claims, ok := token.Claims.(jwt.MapClaims)
		fmt.Println("claims ❗️: ", claims)
		fmt.Println("ok ❗️: ", ok)
		if ok && token.Valid {
			// ユーザー名をコンテキストに設定
			c.Set("username", claims["username"])
			c.Set("isAdmin", claims["admin"])
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// 次のハンドラーを実行
		c.Next()

	}
}

func Router() {
	router := gin.Default()
	// trustedProxies := []string {"http://localhost:3000"}
	// router.SetTrustedProxies(trustedProxies)
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "Access-Control-Allow-Origin"},
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

	// // gin を使用しない場合はコメントアウト
	// router.GET("/companies", GetCompanies)
	// router.GET("/titles", GetTitles)
	// router.GET("/company/:id/with-title", GetCompanyTitles)
	// 一時的にコメントアウト↓
	// router.GET("/company/:id", GetCompany)
	// router.PUT("/company/:id/title/:titleId", UpdateCompanyTitles)
	// router.PUT("/title/:id", UpdateTitle)
	// router.GET("/categories", GetCategories)
	// router.POST("/title", CreateTitle)
	// router.DELETE("/title/:id", DeleteTitle)
	// router.POST("/register", RegisterUser)
	// router.POST("/login", Login)
	// router.GET("/reports", GetReports)
	// router.GET("/fundamentals", GetFundamentals)
	// router.GET("/search/companies", SearchCompaniesByName)

	// 認証が必要なエンドポイント
	auth := router.Group("/")
	auth.Use(AuthMiddleware())
	{
		// auth.GET("/company/:id/titles", GetCompanyTitles)
		auth.GET("/user/auth", AuthUser)
	}

	// localhost だと Docker コンテナを立ち上げ外部からリクエストを受けることができないため
	// 0.0.0.0 に変更
	// err := router.Run("localhost:8080");
	err := router.Run("0.0.0.0:8080")
	if err != nil {
		panic(err)
	}
}
