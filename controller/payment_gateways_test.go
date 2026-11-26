package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/stretchr/testify/require"
)

func TestHostedPaymentGateways(t *testing.T) {
	setupChannelTypeTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.PaymentOrder{}, &model.Log{}))
	oldLogDB, oldTransport := model.LOG_DB, paymentHTTP.Transport
	t.Cleanup(func() { model.LOG_DB, paymentHTTP.Transport = oldLogDB, oldTransport })
	model.LOG_DB = model.DB
	t.Setenv("PAYMENT_PUBLIC_URL", "https://platform.example")
	t.Setenv("PAYMENT_PACKAGES", `[{"id":"standard","name":"Standard","amount":1500,"quota":7500}]`)
	t.Setenv("AIRWALLEX_ENV", "sandbox")
	t.Setenv("AIRWALLEX_CLIENT_ID", "air-client")
	t.Setenv("AIRWALLEX_API_KEY", "air-key")
	t.Setenv("AIRWALLEX_WEBHOOK_SECRET", "air-notify-key")
	t.Setenv("GMPAY_GATEWAY_URL", "https://crypto.example")
	t.Setenv("GMPAY_PID", "100000000001")
	t.Setenv("GMPAY_API_SECRET", "gm-key")

	user := model.User{Username: "hosted-buyer", Quota: 100}
	require.NoError(t, model.DB.Create(&user).Error)
	const gmTradeID = "26071406211234567890"
	sign := func(secret, message string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(message))
		return fmt.Sprintf("%x", mac.Sum(nil))
	}
	airReference := ""
	airStatus := "PENDING"
	airCurrency := "EUR"
	airChecks := 0
	gmCheckout := "https://crypto.example/pay/mock"
	paymentHTTP.Transport = mediaTestTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Host + req.URL.Path {
		case "api.sandbox.airwallex.com/api/v1/authentication/login":
			require.Equal(t, "air-client", req.Header.Get("x-client-id"))
			require.Equal(t, "air-key", req.Header.Get("x-api-key"))
			body = `{"token":"air-token"}`
		case "api.sandbox.airwallex.com/api/v1/pa/payment_links/create":
			require.Equal(t, "Bearer air-token", req.Header.Get("Authorization"))
			var data struct {
				Reference string
				Reusable  *bool
				Amount    json.Number
				Currency  string
			}
			require.NoError(t, json.NewDecoder(req.Body).Decode(&data))
			require.NotEmpty(t, data.Reference)
			require.NotNil(t, data.Reusable)
			require.False(t, *data.Reusable)
			require.Equal(t, json.Number("15.00"), data.Amount)
			require.Equal(t, "USD", data.Currency)
			airReference = data.Reference
			body = fmt.Sprintf(`{"id":"air-link","reference":%q,"amount":15.00,"currency":"USD","reusable":false,"url":"https://www.sandbox.airwallex.com/pay/hk/link/abc"}`, airReference)
		case "api.sandbox.airwallex.com/api/v1/pa/payment_links/air-link":
			require.Equal(t, "Bearer air-token", req.Header.Get("Authorization"))
			airChecks++
			body = fmt.Sprintf(`{"id":"air-link","reference":%q,"amount":15,"currency":"USD","reusable":false,"status":"PAID","latest_successful_payment_intent_id":"int_123"}`, airReference)
		case "api.sandbox.airwallex.com/api/v1/pa/payment_intents/int_123":
			require.Equal(t, "Bearer air-token", req.Header.Get("Authorization"))
			body = fmt.Sprintf(`{"id":"int_123","status":%q,"captured_amount":15,"currency":%q}`, airStatus, airCurrency)
		case "crypto.example/payments/gmpay/v1/order/create-transaction":
			var fields map[string]string
			require.NoError(t, json.NewDecoder(req.Body).Decode(&fields))
			require.Equal(t, "100000000001", fields["pid"])
			require.Empty(t, fields["token"])
			require.Empty(t, fields["network"])
			require.Equal(t, sign("gm-key", "amount=15.00&currency=USD&name=Standard&notify_url=https://platform.example/api/payments/webhook/gmpay&order_id="+fields["order_id"]+"&pid=100000000001&redirect_url=https://platform.example/api/payments/return/gmpay"), fields["signature"])
			body = fmt.Sprintf(`{"status_code":200,"data":{"trade_id":%q,"order_id":%q,"amount":"15.00","currency":"USD","payment_url":%q}}`, gmTradeID, fields["order_id"], gmCheckout)
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})

	engine := gin.New()
	engine.POST("/orders", func(c *gin.Context) { c.Set(ctxkey.Id, user.Id); CreatePaymentOrder(c) })
	engine.GET("/packages", ListPaymentPackages)
	engine.POST("/gmpay", GMPayPaymentWebhook)
	engine.POST("/airwallex", AirwallexPaymentWebhook)
	engine.GET("/return", PaymentReturn)
	send := func(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		return res
	}

	catalog := send(http.MethodGet, "/packages", "", nil).Body.String()
	require.Contains(t, catalog, `"airwallex"`)
	require.Contains(t, catalog, `"gmpay"`)
	buy := func(provider string) model.PaymentOrder {
		res := send(http.MethodPost, "/orders", fmt.Sprintf(`{"package_id":"standard","provider":%q,"amount":1}`, provider), nil)
		var payload struct {
			Success bool
			Data    model.PaymentOrder
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &payload))
		require.True(t, payload.Success, res.Body.String())
		require.EqualValues(t, 1500, payload.Data.Amount)
		return payload.Data
	}
	gm := buy(gmpayProvider)
	require.Equal(t, gmCheckout, gm.CheckoutURL)
	gmCheckout = "https://foreign.example/redirect"
	rejected := send(http.MethodPost, "/orders", `{"package_id":"standard","provider":"gmpay"}`, nil)
	require.Contains(t, rejected.Body.String(), `"success":false`)
	air := buy(airwallexProvider)
	require.Contains(t, air.CheckoutURL, "/pay/hk/link/abc")
	require.Equal(t, http.StatusSeeOther, send(http.MethodGet, "/return", "", nil).Code)

	checkQuota := func(expected int) {
		quota, err := model.GetUserQuota(user.Id)
		require.NoError(t, err)
		require.EqualValues(t, expected, quota)
	}
	gmNotify := func(status int, amount, signatureSecret string) *httptest.ResponseRecorder {
		canonical := fmt.Sprintf("actual_amount=0.015&amount=%s&block_transaction_id=transaction-hash&order_id=%s&pid=100000000001&receive_address=wallet-address&status=%d&token=ETH&trade_id=%s", amount, gm.ID, status, gmTradeID)
		body := fmt.Sprintf(`{"trade_id":%q,"order_id":%q,"pid":"100000000001","status":%d,"amount":%q,"actual_amount":"0.015","block_transaction_id":"transaction-hash","token":"ETH","receive_address":"wallet-address","signature":%q}`, gmTradeID, gm.ID, status, amount, sign(signatureSecret, canonical))
		return send(http.MethodPost, "/gmpay", body, nil)
	}
	require.Equal(t, http.StatusBadRequest, gmNotify(2, "15.00", "wrong-key").Code)
	require.Equal(t, "ok", gmNotify(1, "15.00", "gm-key").Body.String())
	require.Equal(t, "ok", gmNotify(2, "0.01", "gm-key").Body.String())
	checkQuota(100)
	t.Setenv("GMPAY_GATEWAY_URL", "")
	require.Equal(t, "ok", gmNotify(2, "15.00", "gm-key").Body.String())
	require.Equal(t, "ok", gmNotify(2, "15.00", "gm-key").Body.String())
	checkQuota(7600)

	airNotify := func(timestamp time.Time, secret string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"name":"payment_link.paid","data":{"object":{"id":"air-link","reference":%q}}}`, air.ID)
		ts := fmt.Sprint(timestamp.UnixMilli())
		return send(http.MethodPost, "/airwallex", body, map[string]string{"x-timestamp": ts, "x-signature": sign(secret, ts+body)})
	}
	require.Equal(t, http.StatusBadRequest, airNotify(time.Now(), "wrong-key").Code)
	require.Equal(t, http.StatusBadRequest, airNotify(time.Now().Add(-6*time.Minute), "air-notify-key").Code)
	require.Equal(t, http.StatusServiceUnavailable, airNotify(time.Now(), "air-notify-key").Code)
	airStatus = "SUCCEEDED"
	require.Equal(t, http.StatusOK, airNotify(time.Now(), "air-notify-key").Code)
	checkQuota(7600)
	airCurrency = "USD"
	require.Equal(t, http.StatusOK, airNotify(time.Now(), "air-notify-key").Code)
	require.Equal(t, http.StatusOK, airNotify(time.Now(), "air-notify-key").Code)
	require.Equal(t, 3, airChecks)
	checkQuota(15100)

	var logCount int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeTopup).Count(&logCount).Error)
	require.EqualValues(t, 2, logCount)
}
