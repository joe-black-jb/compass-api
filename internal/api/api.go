package api

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/joe-black-jb/compass-api/internal"
	"github.com/joe-black-jb/compass-api/internal/database"
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
	// query := c.Param("")
	fmt.Println("parent_only クエリー: ", parentOnly)
	// fmt.Println(fmt.Sprintf("クエリーの型: %T", parentOnly))

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
	fmt.Println("パラメータのID: ", Id)
	fmt.Println("id の型: ", fmt.Sprintf("%T", Id))
	Company := &internal.Company{}
	if err := database.Db.Preload("Titles").First(Company, Id).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
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
	fmt.Println("リクエストBody: ", reqParams)
	companyTitle := &internal.CompanyTitle{}
	if err := database.Db.Preload("Company").Preload("Title").Where("company_id = ? AND title_id = ?", id, titleId).First(&companyTitle).Error; err != nil {
		fmt.Println("DB検索エラー❗️: ", err)
		fmt.Println(fmt.Sprintf("エラーの型: %T", err))
		// fmt.Println(fmt.Sprintf("エラーの型: %T", err.Error()))
		// fmt.Println(fmt.Sprintf("エラーの型: %T", string(err.Error())))
		c.JSON(http.StatusNotFound, err.Error())
		return
	}
	fmt.Println("companyTitle: ", companyTitle)
	c.JSON(http.StatusOK, companyTitle)
}

func UpdateTitle(c *gin.Context) {
	id := c.Param("id")
	title := &internal.Title{}
	database.Db.First(title, id)
	// fmt.Println("title: ", title)
	if !title.HasValue {
		c.JSON(http.StatusBadRequest, fmt.Sprintf("%s has no value", title.Name))
	}
	var reqParams internal.UpdateCompanyTitleParams
	// リクエストボディをバインドする
	if err := c.ShouldBindJSON(&reqParams); err != nil {
		c.JSON(http.StatusNotFound, err)
		return
	}
	// titleId: 6 有形固定資産 value: 21014
	if title.Value == reqParams.Value {
		msg := fmt.Sprintf("%s の value (%s) は変更されていないため更新しません", title.Name, title.Value)
		// fmt.Println(msg)
		c.JSON(http.StatusOK, msg)
		return
	}

	fmt.Println("リクエストBody: ", reqParams)
	if err := database.Db.First(title, id).Update("Value", reqParams.Value).Error; err != nil {
		c.JSON(http.StatusBadRequest, "Title Update Error")
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
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		fmt.Println("err: ", err)
		c.JSON(http.StatusNotFound, err)
		return
	}
	var errors []string
	if reqBody.Category == nil{
		errors = append(errors, "区分")
	}
	if reqBody.CompanyID  == nil{
		errors = append(errors, "会社ID")
	}
	if reqBody.Name == nil {
		errors = append(errors, "項目名")
	}
	if reqBody.ParentTitleId == nil {
		errors = append(errors, "親項目ID")
	}
	if len(errors) > 0 {
		err := &internal.Error{}
		err.Status = http.StatusBadRequest
		err.Message = fmt.Sprintf("項目が不足しています。不足している項目: %v", errors)
		c.JSON(http.StatusBadRequest, err)
		return
	}
	if reqBody.Depth == nil {
		defaultDepth := 1
		reqBody.Depth = &defaultDepth
	}
	if reqBody.HasValue == nil {
		defaultHasValue := true
		reqBody.HasValue = &defaultHasValue
	}
	if reqBody.StatementType == nil {
		defaultStatementType := 1
		reqBody.StatementType = &defaultStatementType
	}
	if reqBody.FiscalYear == nil {
		defaultFiscalYear := 2023
		reqBody.FiscalYear = &defaultFiscalYear
	}
	if reqBody.Order == nil {
		defaultOrder := 99
		reqBody.Order = &defaultOrder
	}
	fmt.Println("reqBody: ", reqBody);
	c.JSON(http.StatusOK, reqBody)
}