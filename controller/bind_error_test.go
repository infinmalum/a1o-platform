package controller

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFriendlyBindError(t *testing.T) {
	type payload struct {
		RemainQuota int64 `json:"remain_quota"`
		Count       int   `json:"count"`
		ExpiredTime int   `json:"expired_time"`
	}
	var p payload
	err := json.Unmarshal([]byte(`{"remain_quota": 2.56}`), &p)
	require.Error(t, err)
	require.Equal(t, "额度必须为整数（收到 2.56）", friendlyBindError(err))

	var q payload
	err = json.Unmarshal([]byte(`{"count": 1.5}`), &q)
	require.Error(t, err)
	require.Equal(t, "兑换码个数必须为整数（收到 1.5）", friendlyBindError(err))

	// Fields outside the name table keep the original error text.
	var r payload
	err = json.Unmarshal([]byte(`{"expired_time": 2.56}`), &r)
	require.Error(t, err)
	require.Contains(t, friendlyBindError(err), "expired_time")

	require.Equal(t, "boom", friendlyBindError(errors.New("boom")))
}
