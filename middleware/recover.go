package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/logger"
	relaymodel "github.com/infinmalum/a1o-platform/relay/model"
)

func RelayPanicRecover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Errorf(c.Request.Context(), "relay panic (%T) in %s %s\n%s",
					err, c.Request.Method, c.FullPath(), debug.Stack())
				c.Abort()
				if c.Writer.Written() {
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": "Internal server error",
						"type":    relaymodel.ErrorTypeA1OPanic,
					},
				})
			}
		}()
		c.Next()
	}
}
