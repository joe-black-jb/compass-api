package companies

import (
	"net/http"

	"github.com/compass-api/internal"
	"github.com/compass-api/internal/database"
	"github.com/gin-gonic/gin"
)

func GetCompanies (c *gin.Context) {
	companies := &[]internal.Company{}
	if err := database.Db.Find(companies).Error; err != nil {
		c.IndentedJSON(http.StatusNotFound, err)
	}
	c.IndentedJSON(http.StatusOK, companies)
}