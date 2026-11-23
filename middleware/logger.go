package middleware

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/helper"
)

func SetUpLogger(server *gin.Engine) {
	server.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		var requestID string
		if param.Keys != nil {
			requestID = param.Keys[helper.RequestIdKey].(string)
		}
		path := param.Path
		switch param.Request.URL.Path {
		case "/api/payments/webhook/epay", "/api/payments/return/epay":
			path = param.Request.URL.Path
		}
		return fmt.Sprintf("[GIN] %s | %s | %3d | %13v | %15s | %7s %s\n",
			param.TimeStamp.Format("2006/01/02 - 15:04:05"),
			requestID,
			param.StatusCode,
			param.Latency,
			param.ClientIP,
			param.Method,
			path,
		)
	}))
}
