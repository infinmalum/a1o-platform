package helper

import (
	"context"
	"fmt"

	"github.com/infinmalum/a1o-platform/common/random"
)

func GenRequestID() string {
	return GetTimeString() + random.GetRandomNumberString(8)
}

func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, RequestIdKey, id)
}

func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(RequestIdKey).(string)
	return id
}

func GetResponseID(requestID string) string {
	return fmt.Sprintf("chatcmpl-%s", requestID)
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
