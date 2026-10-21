package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// bindFieldNames gives the money-bearing request fields a human name for the
// bind-error toast; everything else falls back to the raw error text.
var bindFieldNames = map[string]string{
	"count":        "兑换码个数",
	"quota":        "额度",
	"remain_quota": "额度",
}

// friendlyBindError rewrites the raw json.UnmarshalTypeError dump that
// surfaces verbatim in the console toast when someone types 2.56 into an
// integer quota field into a per-field hint.
func friendlyBindError(err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if name, ok := bindFieldNames[typeErr.Field]; ok {
			// Value carries the JSON kind, e.g. "number 2.56"; the hint
			// only needs the literal the user actually typed.
			value := strings.TrimPrefix(typeErr.Value, "number ")
			return fmt.Sprintf("%s必须为整数（收到 %s）", name, value)
		}
	}
	return err.Error()
}
