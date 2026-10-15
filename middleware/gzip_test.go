package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/one-gateway/common"
)

func gzipped(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGzipDecodeMiddlewareRejectsDecompressionBomb(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := common.MaxRequestBodySize
	common.MaxRequestBodySize = 1024
	t.Cleanup(func() { common.MaxRequestBodySize = previous })

	// 64 KiB of zeros compresses to a few dozen bytes; without the cap the
	// decompressed stream would be buffered in full.
	compressed := gzipped(t, bytes.Repeat([]byte("a"), 64<<10))

	var readErr error
	engine := gin.New()
	engine.Use(GzipDecodeMiddleware())
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		_, readErr = io.ReadAll(c.Request.Body)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if readErr == nil {
		t.Fatal("解压超过上限应报错，而不是静默截断或全量读入")
	}
}

func TestGzipDecodeMiddlewarePassesSmallBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := []byte(`{"model":"gpt-4o","messages":[]}`)
	var readErr error
	var body []byte
	engine := gin.New()
	engine.Use(GzipDecodeMiddleware())
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		body, readErr = io.ReadAll(c.Request.Body)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(gzipped(t, payload)))
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if readErr != nil {
		t.Fatalf("正常 gzip 请求不应报错: %v", readErr)
	}
	if string(body) != string(payload) {
		t.Fatalf("body = %q", string(body))
	}
}
