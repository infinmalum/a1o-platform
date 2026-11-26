package controller

import (
	"errors"
	"net/http"

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
