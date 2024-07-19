package api

import (
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/joe-black-jb/compass-api/internal"
	"github.com/joe-black-jb/compass-api/internal/database"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func GetCompanies(c *gin.Context) {
	Companies := &[]internal.Company{}
	if err := database.Db.Find(Companies).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	c.IndentedJSON(http.StatusOK, Companies)
}

func GetCompany(c *gin.Context) {
	Id := c.Param("id")
	Company := &internal.Company{}
	if err := database.Db.First(Company, Id).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	c.IndentedJSON(http.StatusOK, Company)
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

func GetCompanyTitles(c *gin.Context) {
	Id := c.Param("id")
	Company := &internal.Company{}
	if err := database.Db.Preload("Titles").First(Company, Id).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	// クエリー付きの場合
	titleId := c.Query("title_id")
	if titleId != "" {
		queryTitleIdInt, err := strconv.Atoi(titleId)
		if err != nil {
			err := &internal.Error{}
			err.Status = http.StatusBadRequest
			err.Message = fmt.Sprintf("不正なIDです。ID: %v", titleId)
			c.JSON(http.StatusBadRequest, err)
			return
		}
		var queryTitleUint uint = uint(queryTitleIdInt)

		var targetTitle *internal.Title
		for _, title := range Company.Titles {
			if title.ID == queryTitleUint {
				targetTitle = title
				break
			}
		}
		if targetTitle != nil {
			c.JSON(http.StatusOK, targetTitle)
		} else {
			err := &internal.Error{}
			err.Status = http.StatusBadRequest
			err.Message = fmt.Sprintf("指定したIDの項目が見つかりませんでした。ID: %v", titleId)
			c.JSON(http.StatusBadRequest, err)
			return
		}
		return
	}
	fmt.Println("ID 1 の会社が持つ項目: ", Company)
	c.IndentedJSON(http.StatusOK, Company)
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
	if reqBody.Name == nil{
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
		"exp": time.Now().Add(time.Hour * 24).Unix(),
		"admin": user.Admin,
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