package companies

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type company struct {
	ID string `json:id`
	Name string `json:name`
}

var companies = []company{
	{ID: "1", Name: "YONEX"},
	{ID: "2", Name: "ミズノ"},
}

func GetCompanies (c *gin.Context) {
	fmt.Println("request❗️❗️: ", c)
	c.IndentedJSON(http.StatusOK, companies)
}