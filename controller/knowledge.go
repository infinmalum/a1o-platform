package controller

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
)

var knowledgeImportSlot = make(chan struct{}, 1)

func knowledgeResponse(c *gin.Context, data any, err error) {
	if err != nil {
		if errors.Is(err, model.ErrKnowledgeNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "knowledge_not_found", "message": err.Error()})
		} else if errors.Is(err, model.ErrKnowledgeLimit) {
			c.JSON(http.StatusOK, gin.H{"success": false, "code": "knowledge_capacity", "message": err.Error()})
		} else if errors.Is(err, model.ErrKnowledgeFileTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "code": "knowledge_file_too_large",
				"limit": fmt.Sprintf("%d MiB", model.GetKnowledgeLimits().FileMiB)})
		} else if errors.Is(err, model.ErrKnowledgeInvalidDocument) {
			c.JSON(http.StatusOK, gin.H{"success": false, "code": "knowledge_invalid_file", "message": err.Error()})
		} else {
			chatError(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func knowledgeID(c *gin.Context, param string) int {
	id, _ := strconv.Atoi(c.Param(param))
	return id
}

func ListKnowledgeLibraries(c *gin.Context) {
	data, err := model.ListKnowledgeLibrariesPage(c.Request.Context(), c.GetInt(ctxkey.Id), studioListPage(c, "offset"))
	knowledgeResponse(c, data, err)
}

func CreateKnowledgeLibrary(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var request struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		chatError(c, err)
		return
	}
	data, err := model.CreateKnowledgeLibrary(c.Request.Context(), c.GetInt(ctxkey.Id), request.Name)
	knowledgeResponse(c, data, err)
}

func ListKnowledgeDocuments(c *gin.Context) {
	data, err := model.ListKnowledgeDocuments(c.Request.Context(), c.GetInt(ctxkey.Id), knowledgeID(c, "libraryId"))
	knowledgeResponse(c, data, err)
}

func UploadKnowledgeDocument(c *gin.Context) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/octet-stream" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"success": false, "message": "请选择要上传的资料文件"})
		return
	}
	limits := model.GetKnowledgeLimits()
	if c.Request.ContentLength > int64(limits.FileBytes()) {
		knowledgeResponse(c, nil, model.ErrKnowledgeFileTooLarge)
		return
	}
	select {
	case knowledgeImportSlot <- struct{}{}:
		defer func() { <-knowledgeImportSlot }()
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "message": "正在导入其他资料，请稍后重试"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, int64(limits.FileBytes())))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			knowledgeResponse(c, nil, model.ErrKnowledgeFileTooLarge)
		} else {
			chatError(c, err)
		}
		return
	}
	data, err := model.ImportKnowledgeDocument(c.Request.Context(), c.GetInt(ctxkey.Id),
		knowledgeID(c, "libraryId"), c.Query("name"), body)
	knowledgeResponse(c, data, err)
}

func GetKnowledgeDocument(c *gin.Context) {
	document, text, err := model.GetKnowledgeDocumentPreview(c.Request.Context(), c.GetInt(ctxkey.Id), knowledgeID(c, "documentId"))
	if err != nil {
		knowledgeResponse(c, nil, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	knowledgeResponse(c, gin.H{"document": document, "text": text}, nil)
}

func GetKnowledgeUploadRules(c *gin.Context) {
	limits := model.GetKnowledgeLimits()
	c.Header("Cache-Control", "no-store")
	knowledgeResponse(c, gin.H{
		"file_bytes": limits.FileBytes(), "library_bytes": limits.LibraryBytes(),
		"max_documents": model.KnowledgeMaxDocuments, "extensions": model.KnowledgeExtensions(),
	}, nil)
}

func DownloadKnowledgeDocument(c *gin.Context) {
	document, err := model.GetKnowledgeDocument(c.Request.Context(), c.GetInt(ctxkey.Id), knowledgeID(c, "documentId"))
	if err != nil {
		knowledgeResponse(c, nil, err)
		return
	}
	contentType := document.ContentType
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	disposition := "attachment"
	if c.Query("preview") == "1" && strings.HasPrefix(contentType, "image/") {
		disposition = "inline"
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": document.Name}))
	c.Data(http.StatusOK, contentType, document.Body)
}

func DeleteKnowledgeDocument(c *gin.Context) {
	err := model.DeleteKnowledgeDocument(c.Request.Context(), c.GetInt(ctxkey.Id),
		knowledgeID(c, "libraryId"), knowledgeID(c, "documentId"))
	knowledgeResponse(c, nil, err)
}

func DeleteKnowledgeLibrary(c *gin.Context) {
	err := model.DeleteKnowledgeLibrary(c.Request.Context(), c.GetInt(ctxkey.Id), knowledgeID(c, "libraryId"))
	knowledgeResponse(c, nil, err)
}

func SetChatKnowledgeLibrary(c *gin.Context) {
	var request struct {
		LibraryId *int `json:"library_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := c.ShouldBindJSON(&request); err != nil || request.LibraryId == nil || *request.LibraryId < 0 {
		chatError(c, errors.New("无效的资料库 ID"))
		return
	}
	err := model.SetChatKnowledgeLibrary(c.Request.Context(), c.GetInt(ctxkey.Id), knowledgeID(c, "id"), *request.LibraryId)
	knowledgeResponse(c, nil, err)
}
