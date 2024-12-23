package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/joe-black-jb/compass-api/internal"
	"github.com/joe-black-jb/compass-api/internal/database"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var dynamoClient *dynamodb.Client
var s3Client *s3.Client
var latestFileKey = "latest/news.json"

func init() {
	env := os.Getenv("ENV")

	if env == "local" {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file err: ", err)
			return
		}
	}
	region := os.Getenv("REGION")
	cfg, cfgErr := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if cfgErr != nil {
		fmt.Println("Load default config error: %v", cfgErr)
		return
	}
	s3Client = s3.NewFromConfig(cfg)
	dynamoClient = dynamodb.NewFromConfig(cfg)
}

// TODO: バッチでDynamoDBの中身をS3に保存する
// TODO: Dynamo Stream で DBの更新をトリガーにデータをS3に流す機能
// TODO: GetCompanies を DB からではなく S3 から取るようにする
func GetCompanies(req events.APIGatewayProxyRequest, dynamoClient *dynamodb.Client) (events.APIGatewayProxyResponse, error) {
	limit := req.QueryStringParameters["limit"]

	companies, err := GetCompaniesProcessor(limit)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error",
		}, err
	}
	body, err := json.Marshal(companies)
	if err != nil {
		fmt.Println("failed to marshal companies to json: ", err)
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "json.Marshal Error",
		}, err
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       string(body),
		Headers: map[string]string{
			"Content-type": "application/json",
		},
	}, nil
}

func GetCompany(req events.APIGatewayProxyRequest, dynamoClient *dynamodb.Client) (events.APIGatewayProxyResponse, error) {
	fmt.Println("==== GetCompany ====")

	// API Gateway で /{companyId} を指定する
	companyId := req.PathParameters["companyId"]

	company, err := GetCompanyProcessor(companyId)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error",
		}, err
	}
	body, err := json.MarshalIndent(company, "", "  ")
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       string(body),
	}, nil
}

func SearchCompaniesByName(req events.APIGatewayProxyRequest, dynamoClient *dynamodb.Client) (events.APIGatewayProxyResponse, error) {
	companyName := req.QueryStringParameters["companyName"]

	companies, err := SearchCompaniesByNameProcessor(companyName)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error",
		}, err
	}
	body, err := json.MarshalIndent(companies, "", "  ")
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "json.MarshalIndent Error",
		}, err
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       string(body),
		Headers: map[string]string{
			"Content-type": "application/json",
		},
	}, nil
}

func GetTitles(c *gin.Context) {
	parentOnly := c.Query("parent_only")
	Titles := &[]internal.Title{}
	if err := database.Db.Find(Titles).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	// 親のいない勘定項目を抽出
	var parents []internal.Title

	if parentOnly == "true" {
		for _, title := range *Titles {
			if title.ParentTitleId == 0 {
				parents = append(parents, title)
			}
		}
		c.IndentedJSON(http.StatusOK, parents)
		return
	}
	fmt.Println("親の数: ", len(parents))

	c.IndentedJSON(http.StatusOK, Titles)
}

func UpdateCompanyTitles(c *gin.Context) {
	id := c.Param("id")
	titleId := c.Param("titleId")
	var reqParams internal.UpdateCompanyTitleParams
	// リクエストボディをバインドする
	if err := c.ShouldBindJSON(&reqParams); err != nil {
		c.JSON(http.StatusNotFound, err)
		return
	}
	companyTitle := &internal.CompanyTitle{}
	if err := database.Db.Preload("Company").Preload("Title").Where("company_id = ? AND title_id = ?", id, titleId).First(&companyTitle).Error; err != nil {
		c.JSON(http.StatusNotFound, err.Error())
		return
	}
	c.JSON(http.StatusOK, companyTitle)
}

func UpdateTitle(c *gin.Context) {
	id := c.Param("id")
	var reqBody internal.CreateTitleBody
	title := &internal.Title{}
	if err := database.Db.First(title, id).Error; err != nil {
		err := &internal.Error{}
		err.Status = http.StatusBadRequest
		err.Message = fmt.Sprintf("更新対象の項目が見つかりませんでした。項目ID: %v", id)
		c.JSON(http.StatusBadRequest, err)
		return
	}
	// リクエストボディをバインドする
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.JSON(http.StatusNotFound, err)
		return
	}
	// body 作成処理
	errors, updates := ConvertUpdateTitleBody(&reqBody)
	if len(errors) > 0 {
		err := &internal.Error{}
		err.Status = http.StatusBadRequest
		err.Message = "Bad Request"
		c.JSON(http.StatusBadRequest, err)
		return
	}

	// レコード更新処理
	if err := database.Db.First(title, id).Updates(updates).Error; err != nil {
		err := &internal.Error{}
		err.Status = http.StatusBadRequest
		err.Message = "項目更新処理に失敗しました"
		c.JSON(http.StatusInternalServerError, err)
		return
	}
	fmt.Println("Updated Title: ", title)
	c.JSON(http.StatusOK, title)
}

func GetCategories(c *gin.Context) {
	titles := &[]internal.Title{}
	if err := database.Db.Find(titles).Error; err != nil {
		c.JSON(http.StatusNotFound, err)
	}
	var categories []string
	for _, title := range *titles {
		if !slices.Contains(categories, title.Category) {
			categories = append(categories, title.Category)
		}
	}
	c.JSON(http.StatusOK, categories)
}

func CreateTitle(c *gin.Context) {
	var reqBody internal.CreateTitleBody
	var title = &internal.Title{}
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		fmt.Println("err: ", err)
		c.JSON(http.StatusNotFound, err)
		return
	}
	// すでに存在する項目の場合エラーを返す
	var duplicatedTitles = &[]internal.Title{}
	database.Db.Where("name = ? AND company_id = ?", reqBody.Name, reqBody.CompanyID).Find(duplicatedTitles)

	if len(*duplicatedTitles) > 0 {
		err := &internal.Error{}
		err.Status = http.StatusBadRequest
		err.Message = fmt.Sprintf("項目が既に登録されています。項目名: %v", *reqBody.Name)
		c.JSON(http.StatusBadRequest, err)
		return
	}

	// body 作成処理
	errors, ok := ConvertTitleBody(title, &reqBody)
	if len(errors) > 0 {
		err := &internal.Error{}
		err.Status = http.StatusBadRequest
		err.Message = fmt.Sprintf("項目が不足しています。不足している項目: %v", errors)
		c.JSON(http.StatusBadRequest, err)
		return
	}
	if !ok {
		err := &internal.Error{}
		err.Status = http.StatusBadRequest
		err.Message = "項目登録処理に失敗しました"
		c.JSON(http.StatusBadRequest, err)
		return
	}
	// トランザクション処理
	tx := database.Db.Begin()
	database.Db.Transaction(func(tx *gorm.DB) error {
		// 1. titles テーブルにレコード追加
		if err := tx.Create(&title).Error; err != nil {
			tx.Rollback()
			fmt.Println("エラー内容: ", err)
			errObj := &internal.Error{}
			errObj.Status = http.StatusInternalServerError
			errObj.Message = "勘定項目の作成に失敗しました"
			c.JSON(http.StatusInternalServerError, errObj)
			return nil
		}
		// 2. company_titles テーブルにレコード追加
		var companyTitle = &internal.CompanyTitle{}
		titleId := &title.ID
		companyTitle.CompanyID = *reqBody.CompanyID
		companyTitle.TitleID = int(*titleId)
		if err := tx.Create(&companyTitle).Error; err != nil {
			tx.Rollback()
			fmt.Println("エラー内容: ", err)
			errObj := &internal.Error{}
			errObj.Status = http.StatusInternalServerError
			errObj.Message = "中間テーブルへの登録に失敗しました"
			c.JSON(http.StatusInternalServerError, errObj)
			return nil
		}
		return nil
	})
	tx.Commit()
	c.JSON(http.StatusOK, title)
}

func DeleteTitle(c *gin.Context) {
	titleId := c.Param("id")
	title := &internal.Title{}

	if err := database.Db.First(title, titleId).Error; err != nil {
		errObj := &internal.Error{}
		errObj.Status = http.StatusBadRequest
		errObj.Message = fmt.Sprintf("削除対象項目が見つかりませんでした。項目ID: %v", titleId)
		c.JSON(http.StatusBadRequest, errObj)
		return
	}

	// 紐づく子項目がある場合は削除しない
	// => 削除しようとしているタイトルが parent_title_id に入っている title がある場合
	titles := &[]internal.Title{}
	if err := database.Db.Where("parent_title_id = ?", titleId).Find(titles).Error; err != nil {
		errObj := &internal.Error{}
		errObj.Status = http.StatusInternalServerError
		errObj.Message = fmt.Sprintf("指定した項目と紐づく項目取得処理でエラーが発生しました。項目ID: %v", titleId)
		c.JSON(http.StatusInternalServerError, errObj)
		return
	}

	if len(*titles) > 0 {
		errObj := &internal.Error{}
		errObj.Status = http.StatusBadRequest
		errObj.Message = "指定した項目に紐づく項目が存在するため削除できません"
		c.JSON(http.StatusBadRequest, errObj)
		return
	}

	// トランザクション処理
	tx := database.Db.Begin()
	database.Db.Transaction(func(tx *gorm.DB) error {
		// 1. company_titles からレコードを削除
		companyTitle := &internal.CompanyTitle{}
		if err := tx.Where("title_id = ? AND company_id = ?", titleId, title.CompanyID).Unscoped().Delete(&companyTitle).Error; err != nil {
			tx.Rollback()
			errObj := &internal.Error{}
			errObj.Status = http.StatusInternalServerError
			errObj.Message = fmt.Sprintf("中間テーブルのレコード削除に失敗しました。項目ID: %v, 会社ID: %v", titleId, title.CompanyID)
			c.JSON(http.StatusInternalServerError, errObj)
			return nil
		}
		// 2. titles からレコードを削除
		if err := tx.Unscoped().Delete(title, titleId).Error; err != nil {
			tx.Rollback()
			errObj := &internal.Error{}
			errObj.Status = http.StatusInternalServerError
			errObj.Message = fmt.Sprintf("項目の削除に失敗しました。項目ID: %v", titleId)
			c.JSON(http.StatusInternalServerError, errObj)
			return nil
		}
		return nil
	})
	tx.Commit()
	deletedMsg := fmt.Sprintf("項目を削除しました。項目名: %v", title.Name)
	c.JSON(http.StatusOK, deletedMsg)
}

func RegisterUser(c *gin.Context) {
	var reqBody internal.RegisterUserBody
	fmt.Println("ハッシュ化開始❗️")
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		fmt.Println("err: ", err)
		c.JSON(http.StatusNotFound, err)
	}
	var errors []string
	if reqBody.Name == nil {
		errors = append(errors, "名前")
	}
	if reqBody.Password == nil {
		errors = append(errors, "パスワード")
	}
	if reqBody.Email == nil {
		errors = append(errors, "メールアドレス")
	}
	if len(errors) > 0 {
		errObj := &internal.Error{}
		errObj.Status = http.StatusBadRequest
		errObj.Message = fmt.Sprintf("未入力の項目があります。項目: %v", errors)
		c.JSON(http.StatusBadRequest, errObj)
		return
	}
	// メールアドレス重複チェック
	users := &[]internal.User{}
	database.Db.Where("email = ?", reqBody.Email).First(&users)
	if len(*users) > 0 {
		errObj := &internal.Error{}
		errObj.Status = http.StatusBadRequest
		errObj.Message = ("入力されたメールアドレスは既に登録されています")
		c.JSON(http.StatusBadRequest, errObj)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*reqBody.Password), bcrypt.DefaultCost)
	if err != nil {
		errObj := &internal.Error{}
		errObj.Status = http.StatusInternalServerError
		errObj.Message = "パスワードの暗号化処理に失敗しました"
		c.JSON(http.StatusInternalServerError, errObj)
		return
	}
	user := &internal.User{}
	user.Name = *reqBody.Name
	user.Email = *reqBody.Email
	user.Password = hash
	user.Admin = false

	// DB登録
	if err := database.Db.Create(&user).Error; err != nil {
		errObj := &internal.Error{}
		errObj.Status = http.StatusInternalServerError
		errObj.Message = "ユーザ登録処理に失敗しました"
		c.JSON(http.StatusInternalServerError, errObj)
		return
	}

	c.JSON(http.StatusOK, "ユーザ登録に成功しました")
}

func Login(c *gin.Context) {
	fmt.Println("ログイン処理開始")
	var reqBody internal.Credentials
	user := &internal.User{}
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		fmt.Println("err: ", err)
		c.JSON(http.StatusNotFound, err)
		return
	}
	fmt.Println("user: ", user)
	if err := database.Db.Where("email = ?", reqBody.Email).First(user).Error; err != nil {
		errObj := &internal.Error{}
		errObj.Status = http.StatusBadRequest
		errObj.Message = "入力されたメールアドレスは登録されていません"
		c.JSON(http.StatusBadRequest, errObj)
		return
	}
	if err := bcrypt.CompareHashAndPassword(user.Password, []byte(reqBody.Password)); err != nil {
		fmt.Println("エラー発生: ", err)
		errObj := &internal.Error{}
		errObj.Status = http.StatusBadRequest
		errObj.Message = "入力されたパスワードに誤りがあります"
		c.JSON(http.StatusInternalServerError, errObj)
		return
	}
	// jwt トークンの生成
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": user.Name,
		"exp":      time.Now().Add(time.Hour * 24).Unix(),
		"admin":    user.Admin,
	})

	// 秘密鍵の確認
	jwtSecret := os.Getenv("SECRET_KEY")
	if jwtSecret == "" {
		c.JSON(http.StatusInternalServerError, "err")
		return
	}

	// トークンの署名
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error while generating token"})
		return
	}
	fmt.Println("tokenString: ", tokenString)
	// token.Raw = tokenString
	// signature := strings.Split(tokenString, ".")[2]
	// fmt.Println("signature: ", signature)
	// token.Signature = signature

	okObj := &internal.Ok{}
	okObj.Status = http.StatusOK
	okObj.Message = "認証に成功しました"

	loginResult := internal.Login{}
	loginResult.Username = user.Name
	loginResult.Token = tokenString
	c.JSON(http.StatusOK, loginResult)
}

func AuthUser(c *gin.Context) {
	isAdmin, _ := c.Get("isAdmin")
	if isAdmin == true {
		c.JSON(http.StatusOK, true)
	} else {
		c.JSON(http.StatusOK, false)
	}
}

/*
- S3 から EDINETコード 配下にある BS データが記載された HTML 一覧を取得する
- HTML の中身を string で返してフロントで parse する
*/
func GetReports(req events.APIGatewayProxyRequest, client *dynamodb.Client) (events.APIGatewayProxyResponse, error) {
	EDINETCode := req.QueryStringParameters["EDINETCode"]
	reportType := req.QueryStringParameters["reportType"]
	extension := req.QueryStringParameters["extension"]

	fmt.Println("EDINETCode ⭐️: ", EDINETCode)

	reportData, err := GetReportsProcessor(EDINETCode, reportType, extension)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error",
		}, err
	}
	body, err := json.MarshalIndent(reportData, "", "  ")
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "json.MarshalIndent Error",
		}, err
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       string(body),
		Headers: map[string]string{
			"Content-type": "application/json",
		},
	}, nil
}

func GetFundamentals(req events.APIGatewayProxyRequest, client *dynamodb.Client) (events.APIGatewayProxyResponse, error) {
	EDINETCode := req.QueryStringParameters["EDINETCode"]

	// periodStart := c.Query("periodStart")

	// S3 から BS HTML 一覧を取得
	bucketName := os.Getenv("BUCKET_NAME")
	// プレフィックス (ディレクトリのようなもの)
	prefix := fmt.Sprintf("%s/Fundamentals", EDINETCode)

	fundamentals, err := GetFundamentalsProcessor(EDINETCode, bucketName, prefix)
	if err != nil {
		fmt.Println("Get Fundamentals error: ", err)
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error",
		}, err
	}
	body, err := json.MarshalIndent(fundamentals, "", "  ")
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "json.MarshalIndent Error",
		}, err
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       string(body),
		Headers: map[string]string{
			"Content-type": "application/json",
		},
	}, nil
}

func GetLatestNews(req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	newsBucketName := os.Getenv("NEWS_BUCKET_NAME")

	result, err := GetLatestNewsProcessor(newsBucketName, latestFileKey)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error",
		}, err
	}
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       result,
		Headers: map[string]string{
			"Content-type": "application/json",
		},
	}, nil
}
