package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/model"
)

func studioListPage(c *gin.Context, key string) model.ListPage {
	raw, present := c.GetQuery(key)
	if !present {
		return model.ListPage{}
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		offset = 0
	}
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil || limit <= 0 || limit > 51 {
		limit = 25
	}
	return model.ListPage{Offset: offset, Limit: limit}
}
