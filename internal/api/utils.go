package api

import "github.com/joe-black-jb/compass-api/internal"

func ConvertTitleBody (title *internal.Title, reqBody *internal.CreateTitleBody) (errors []string, ok bool) {
	// 必須項目: 区分、会社ID、項目名、親項目ID
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
		return errors, false
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
	title.Category = *reqBody.Category
	title.CompanyID = *reqBody.CompanyID
	title.Name = *reqBody.Name
	title.ParentTitleId = *reqBody.ParentTitleId
	title.Depth = *reqBody.Depth
	title.HasValue = *reqBody.HasValue
	title.StatementType = *reqBody.StatementType
	title.FiscalYear = *reqBody.FiscalYear
	title.Order = *reqBody.Order
	if reqBody.Value != nil {
		title.Value = *reqBody.Value
	}
	return nil, true
}

func ConvertUpdateTitleBody (reqBody *internal.CreateTitleBody) (errors []string, result map[string]interface{}) {
	updates := make(map[string]interface{})
	if reqBody.Name != nil{
		updates["Name"] = *reqBody.Name
	}
	if reqBody.Category != nil{
		updates["Category"] = *reqBody.Category
	}
	if reqBody.ParentTitleId != nil{
		updates["ParentTitleId"] = *reqBody.ParentTitleId
	}
	if reqBody.CompanyID != nil{
		updates["CompanyID"] = *reqBody.CompanyID
	}
	if reqBody.Value != nil{
		updates["Value"] = *reqBody.Value
	}
	return nil, updates
}