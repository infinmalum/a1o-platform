package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/stretchr/testify/require"
)

func TestChatUploadHandlesOversizedAndUnreadableFiles(t *testing.T) {
	engine := gin.New()
	engine.POST("/files", UploadChatFile)
	originalLimit := config.ChatMaxFileBytes
	t.Cleanup(func() { config.ChatMaxFileBytes = originalLimit })

	send := func(contentType string, body *bytes.Buffer) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/files", body)
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		return response
	}

	for _, test := range []struct {
		limit int64
		size  int
		code  string
		shown string
	}{
		{1024, 1025, "file_too_large", "1 KiB"},
		{1024, 2 * (64 << 10), "file_too_large", "1 KiB"},
		{1 << 20, maxChatTextFileBytes + 1, "text_file_too_large", "256 KiB"},
	} {
		config.ChatMaxFileBytes = test.limit
		var body bytes.Buffer
		upload := multipart.NewWriter(&body)
		file, err := upload.CreateFormFile("file", "oversized.txt")
		require.NoError(t, err)
		_, err = file.Write(bytes.Repeat([]byte("X"), test.size))
		require.NoError(t, err)
		require.NoError(t, upload.Close())
		response := send(upload.FormDataContentType(), &body)
		require.Contains(t, response.Body.String(), test.code)
		require.Contains(t, response.Body.String(), test.shown)
	}

	response := send("multipart/form-data; boundary=missing", bytes.NewBufferString("malformed"))
	require.Contains(t, response.Body.String(), "upload_read_failed")
	require.NotContains(t, response.Body.String(), "multipart")
}
