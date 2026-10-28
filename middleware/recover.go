package middleware

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common"
	"github.com/infinmalum/a1o-platform/common/logger"
	relaymodel "github.com/infinmalum/a1o-platform/relay/model"
	"net/http"
	"runtime/debug"
)

func RelayPanicRecover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				ctx := c.Request.Context()
				logger.Errorf(ctx, "panic detected: %v", err)
				logger.Errorf(ctx, "stacktrace from panic: %s", string(debug.Stack()))
				logger.Errorf(ctx, "request: %s %s", c.Request.Method, c.Request.URL.Path)
				body, _ := common.GetRequestBody(c)
				logger.Errorf(ctx, "request body: %s", string(body))
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf("Panic detected, error: %v. Please submit an issue with the related log here: https://github.com/infinmalum/a1o-platform/issues", err),
						"type":    relaymodel.ErrorTypeA1OPanic,
					},
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}
