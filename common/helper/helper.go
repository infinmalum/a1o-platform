package helper

import (
	"context"
	"fmt"
	"strconv"

	"github.com/infinmalum/one-gateway/common/random"
)

func Interface2String(inter interface{}) string {
	switch inter := inter.(type) {
	case string:
		return inter
	case int:
		return fmt.Sprintf("%d", inter)
	case float64:
		return fmt.Sprintf("%f", inter)
	}
	return "Not Implemented"
}

func GenRequestID() string {
	return GetTimeString() + random.GetRandomNumberString(8)
}

func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, RequestIdKey, id)
}

func GetRequestID(ctx context.Context) string {
	rawRequestId := ctx.Value(RequestIdKey)
	if rawRequestId == nil {
		return ""
	}
	return rawRequestId.(string)
}

func GetResponseID(requestID string) string {
	return fmt.Sprintf("chatcmpl-%s", requestID)
}

func Max(a int, b int) int {
	if a >= b {
		return a
	} else {
		return b
	}
}

func AssignOrDefault(value string, defaultValue string) string {
	if len(value) != 0 {
		return value
	}
	return defaultValue
}

func MessageWithRequestId(message string, id string) string {
	return fmt.Sprintf("%s (request id: %s)", message, id)
}

func String2Int(str string) int {
	num, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return num
}
