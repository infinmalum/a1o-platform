package middleware

import (
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
)

// PlaygroundRelay runs after UserAuth on the fixed Playground aliases only.
// The canonical path keeps request parsing, dispatch, and protocol errors
// identical to the public API. The billing token stays on the server.
func PlaygroundRelay(prefix string) gin.HandlerFunc {
	return sessionRelay(prefix, "application/json", 0)
}

// StudioAudioRelay caps browser uploads before the public relay parses them.
func StudioAudioRelay(prefix string, multipart bool) gin.HandlerFunc {
	if multipart {
		return sessionRelay(prefix, "multipart/form-data", (16<<20)+(64<<10))
	}
	return sessionRelay(prefix, "application/json", 32<<10)
}

func sessionRelay(prefix, contentType string, maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if site := c.GetHeader("Sec-Fetch-Site"); maxBytes > 0 && site != "" && site != "same-origin" {
			abortWithMessage(c, http.StatusForbidden, "same-origin request is required")
			return
		}
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || mediaType != contentType {
			abortWithMessage(c, http.StatusUnsupportedMediaType, contentType+" is required")
			return
		}

		token, err := model.EnsureChatToken(c.GetInt(ctxkey.Id))
		if err != nil {
			abortWithMessage(c, http.StatusInternalServerError, "Chat billing token unavailable")
			return
		}

		original := c.Request
		request := original.Clone(original.Context())
		request.URL.Path = strings.TrimPrefix(request.URL.Path, prefix)
		request.URL.RawPath = strings.TrimPrefix(request.URL.RawPath, prefix)
		request.RequestURI = request.URL.RequestURI()
		if maxBytes > 0 {
			if request.ContentLength > maxBytes {
				abortWithMessage(c, http.StatusRequestEntityTooLarge, "request body is too large")
				return
			}
			request.Body = http.MaxBytesReader(c.Writer, request.Body, maxBytes)
		}
		request.Header.Set("Authorization", "Bearer sk-"+token.Key)
		c.Request = request
		defer func() { c.Request = original }()
		c.Next()
	}
}
