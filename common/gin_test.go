package common

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetRequestBodyRejectsOverLimitBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := MaxRequestBodySize
	MaxRequestBodySize = 64
	t.Cleanup(func() { MaxRequestBodySize = previous })

	engine := gin.New()
	engine.POST("/", func(c *gin.Context) {
		if _, err := GetRequestBody(c); err == nil {
			t.Error("超过上限的请求体应返回错误")
		}
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte("x"), 128)))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
}

func TestGetRequestBodyAllowsWithinLimitBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := MaxRequestBodySize
	MaxRequestBodySize = 64
	t.Cleanup(func() { MaxRequestBodySize = previous })

	engine := gin.New()
	engine.POST("/", func(c *gin.Context) {
		body, err := GetRequestBody(c)
		if err != nil {
			t.Errorf("未超限请求不应报错: %v", err)
		}
		if string(body) != "hello" {
			t.Errorf("body = %q", string(body))
		}
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("hello"))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
}
