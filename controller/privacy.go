package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
	"gorm.io/gorm"
)

func privacyFailure(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	} else {
		logger.Error(c.Request.Context(), "privacy operation failed")
	}
	c.JSON(status, gin.H{"success": false, "message": http.StatusText(status)})
}

func GetUserMemory(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	enabled, entries, err := model.ListUserMemory(c.Request.Context(), c.GetInt(ctxkey.Id))
	if err != nil {
		privacyFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"enabled": enabled, "available": config.ChatMemoryEnabled, "entries": entries,
	}})
}

func SetUserMemory(c *gin.Context) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := c.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid memory preference"})
		return
	}
	if err := model.SetUserMemory(c.Request.Context(), c.GetInt(ctxkey.Id), *input.Enabled); err != nil {
		privacyFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeleteUserMemory(c *gin.Context) {
	if err := model.DeleteUserMemory(c.Request.Context(), c.GetInt(ctxkey.Id), c.Param("entry")); err != nil {
		privacyFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func ExportUserPrivacyData(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="a1o-personal-data.zip"`)
	if err := model.WritePrivacyExport(c.Request.Context(), c.Writer, c.GetInt(ctxkey.Id)); err != nil {
		// No ZIP directory is written on failure, so a partial archive is not
		// presented as a complete export.
		logger.Error(c.Request.Context(), "privacy export failed")
		if !c.Writer.Written() {
			c.Writer.Header().Del("Content-Disposition")
			privacyFailure(c, err)
		}
	}
}

func privacyBefore(c *gin.Context) (int, bool) {
	before, err := strconv.Atoi(c.DefaultQuery("before", "0"))
	if err != nil || before < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid page cursor"})
		return 0, false
	}
	return before, true
}

func privacyPage[T any](c *gin.Context, rows []T, err error, id func(T) int) {
	if err != nil {
		privacyFailure(c, err)
		return
	}
	next := 0
	if len(rows) > model.ReviewPageSize {
		rows = rows[:model.ReviewPageSize]
		next = id(rows[len(rows)-1])
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"rows": rows, "next": next}})
}

func ListRetainedChats(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if before, ok := privacyBefore(c); ok {
		rows, err := model.ListRetainedChats(c.Request.Context(), before)
		privacyPage(c, rows, err, func(row model.RetainedChat) int { return row.Id })
	}
}

func ListPrivacyReviewAccess(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if before, ok := privacyBefore(c); ok {
		rows, err := model.ListPrivacyReviewAccess(c.Request.Context(), before)
		privacyPage(c, rows, err, func(row model.PrivacyReviewAccess) int { return row.Id })
	}
}

func ReviewRetainedChat(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := strconv.Atoi(c.Param("id"))
	var input struct {
		Reason string `json:"reason"`
		After  int    `json:"after"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err != nil || id <= 0 || c.ShouldBindJSON(&input) != nil || input.After < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid review request"})
		return
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > 300 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Enter a review reason (1–300 characters)"})
		return
	}
	rows, err := model.ReviewRetainedChat(c.Request.Context(), c.GetInt(ctxkey.Id), id, input.After, reason)
	privacyPage(c, rows, err, func(row model.ReviewMessage) int { return row.Id })
}
