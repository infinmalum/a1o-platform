package common

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
)

// MaxRequestBodySize bounds how much of a request body the gateway buffers,
// and how far a gzip-encoded request body may decompress. It sits well above
// the largest legitimate multimodal payloads (base64 images) but stops
// decompression bombs from ballooning gateway memory. It is a variable so
// tests can lower it.
var MaxRequestBodySize = int64(64 << 20) // 64 MiB

func GetRequestBody(c *gin.Context) ([]byte, error) {
	requestBody, _ := c.Get(ctxkey.KeyRequestBody)
	if requestBody != nil {
		return requestBody.([]byte), nil
	}
	requestBody, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, MaxRequestBodySize))
	if err != nil {
		return nil, err
	}
	_ = c.Request.Body.Close()
	c.Set(ctxkey.KeyRequestBody, requestBody)
	return requestBody.([]byte), nil
}

func UnmarshalBodyReusable(c *gin.Context, v any) error {
	requestBody, err := GetRequestBody(c)
	if err != nil {
		return err
	}
	contentType := c.Request.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		err = json.Unmarshal(requestBody, &v)
	} else {
		c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		err = c.ShouldBind(&v)
	}
	if err != nil {
		return err
	}
	// Reset request body
	c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	return nil
}

// SetEventStreamHeaders prepares a writer for server-sent events. Adapters
// pass their transport writer; the native streaming path sets the same fields.
func SetEventStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Accel-Buffering", "no")
}
