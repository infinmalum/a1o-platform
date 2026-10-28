package controller

import (
	"github.com/gin-gonic/gin"
	billingratio "github.com/infinmalum/a1o-platform/relay/billing/ratio"
	"net/http"
)

func GetGroups(c *gin.Context) {
	groupNames := billingratio.GroupRatioNames()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}
