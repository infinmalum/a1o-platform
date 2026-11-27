package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeUploadRulesAndPrivateOriginal(t *testing.T) {
	db := setupAgentControllerDB(t)
	require.NoError(t, db.AutoMigrate(&model.KnowledgeLibrary{}, &model.KnowledgeDocument{}, &model.KnowledgeChunk{}))
	library, err := model.CreateKnowledgeLibrary(context.Background(), 17, "pictures")
	require.NoError(t, err)

	engine := gin.New()
	group := engine.Group("/knowledge", func(c *gin.Context) {
		user := 17
		if c.GetHeader("X-Other-Test-User") == "1" {
			user = 23
		}
		c.Set(ctxkey.Id, user)
	})
	group.GET("/upload-rules", GetKnowledgeUploadRules)
	group.POST("/libraries/:libraryId/documents", UploadKnowledgeDocument)
	group.GET("/documents/:documentId", GetKnowledgeDocument)
	group.GET("/documents/:documentId/file", DownloadKnowledgeDocument)

	rules := httptest.NewRecorder()
	engine.ServeHTTP(rules, httptest.NewRequest(http.MethodGet, "/knowledge/upload-rules", nil))
	require.Equal(t, http.StatusOK, rules.Code)
	require.Contains(t, rules.Body.String(), `"file_bytes"`)
	require.Contains(t, rules.Body.String(), `".png"`)
	tooLarge := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/knowledge/libraries/%d/documents", library.Id), nil)
	tooLarge.Header.Set("Content-Type", "application/octet-stream")
	tooLarge.ContentLength = int64(model.GetKnowledgeLimits().FileBytes()) + 1
	oversize := httptest.NewRecorder()
	engine.ServeHTTP(oversize, tooLarge)
	require.Equal(t, http.StatusRequestEntityTooLarge, oversize.Code)
	require.Contains(t, oversize.Body.String(), "knowledge_file_too_large")

	original, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9ZQmcAAAAASUVORK5CYII=")
	require.NoError(t, err)
	uploadURL := fmt.Sprintf("/knowledge/libraries/%d/documents?name=photo.png", library.Id)
	request := httptest.NewRequest(http.MethodPost, uploadURL, bytes.NewReader(original))
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var uploaded struct {
		Success bool
		Data    model.KnowledgeDocument
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &uploaded))
	require.True(t, uploaded.Success, recorder.Body.String())
	require.Equal(t, "image/png", uploaded.Data.ContentType)

	fileURL := fmt.Sprintf("/knowledge/documents/%d/file", uploaded.Data.Id)
	for _, path := range []string{fileURL, fileURL + "?preview=1"} {
		download := httptest.NewRecorder()
		engine.ServeHTTP(download, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, download.Code)
		require.Equal(t, "image/png", download.Header().Get("Content-Type"))
		require.Equal(t, "no-store", download.Header().Get("Cache-Control"))
		require.Equal(t, original, download.Body.Bytes())
		if path == fileURL {
			require.Contains(t, download.Header().Get("Content-Disposition"), "attachment")
		} else {
			require.Contains(t, download.Header().Get("Content-Disposition"), "inline")
		}
	}
	foreign := httptest.NewRequest(http.MethodGet, fileURL, nil)
	foreign.Header.Set("X-Other-Test-User", "1")
	denied := httptest.NewRecorder()
	engine.ServeHTTP(denied, foreign)
	require.Equal(t, http.StatusNotFound, denied.Code)

	legacy := &model.KnowledgeDocument{LibraryId: library.Id, Name: "old.txt", Body: []byte("legacy original")}
	require.NoError(t, db.Create(legacy).Error)
	preview := httptest.NewRecorder()
	engine.ServeHTTP(preview, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/knowledge/documents/%d", legacy.Id), nil))
	require.Equal(t, http.StatusOK, preview.Code)
	require.Contains(t, preview.Body.String(), "legacy original")
}
