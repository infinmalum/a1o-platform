package middleware

import (
	"compress/gzip"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/one-gateway/common"
)

func GzipDecodeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Content-Encoding") == "gzip" {
			gzipReader, err := gzip.NewReader(c.Request.Body)
			if err != nil {
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			defer gzipReader.Close()

			// Replace the request body with the decompressed data, capped so
			// a small compressed payload cannot expand into an OOM; reads
			// past the cap fail loudly instead of silently truncating.
			c.Request.Body = http.MaxBytesReader(c.Writer, io.NopCloser(gzipReader), common.MaxRequestBodySize)
		}

		// Continue processing the request
		c.Next()
	}
}
