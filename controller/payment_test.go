package controller

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/stretchr/testify/require"
)

func TestNowPaymentCurrencyConfig(t *testing.T) {
	t.Setenv("PAYMENT_PUBLIC_URL", "https://platform.example")
	t.Setenv("PAYMENT_PACKAGES", "[]")
	t.Setenv("STRIPE_SECRET_KEY", "stripe-key")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "stripe-secret")
	t.Setenv("NOWPAYMENTS_API_KEY", "now-key")
	t.Setenv("NOWPAYMENTS_IPN_SECRET", "now-secret")
	for _, tc := range []struct {
		name, setting string
		codes         []string
	}{
		{name: "empty"},
		{name: "invalid", setting: "usdtsol,invalid"},
		{name: "single", setting: "btc", codes: []string{"btc"}},
		{name: "multiple", setting: " USDTSOL, usdcmatic,usdtsol ", codes: []string{"usdtsol", "usdcmatic"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NOWPAYMENTS_PAY_CURRENCIES", tc.setting)
			cfg, err := loadPaymentConfig()
			require.NoError(t, err)
			require.Contains(t, cfg.providers, stripeProvider)
			require.Equal(t, len(tc.codes) > 0, len(cfg.providers) == 2)
			require.Len(t, cfg.nowCurrencies, len(tc.codes))
			for i, code := range tc.codes {
				require.Equal(t, code, cfg.nowCurrencies[i].Code)
				require.NotEmpty(t, cfg.nowCurrencies[i].Label)
			}
		})
	}
}

func TestLegacyNowPaymentCurrency(t *testing.T) {
	setupChannelTypeTest(t)
	// Simulate the old column layout before running the upgrade migration.
	require.NoError(t, model.DB.Exec(`CREATE TABLE payment_orders (id text PRIMARY KEY, provider text, provider_id text, amount integer, currency text)`).Error)
	id := uuid.NewString()
	require.NoError(t, model.DB.Exec(`INSERT INTO payment_orders (id, provider, provider_id, amount, currency) VALUES (?, 'nowpayments', '9001', 1500, 'usd')`, id).Error)
	require.NoError(t, model.DB.AutoMigrate(&model.PaymentOrder{}))
	var order model.PaymentOrder
	require.NoError(t, model.DB.First(&order, "id = ?", id).Error)
	require.Empty(t, order.PayCurrency)
	payment := nowPayment{Invoice: "9001", Reference: order.ID, Status: "finished", Amount: "15", Currency: "usd",
		PayCurrency: "USDTTRC20", Due: "15", Paid: "15"}
	require.True(t, nowPayMatches(payment, order))
	require.Equal(t, legacyNowCurrency, presentPaymentOrder(order).PayCurrency)
	payment.PayCurrency = "usdtsol"
	require.False(t, nowPayMatches(payment, order))
	order.PayCurrency = "btc"
	payment.PayCurrency, payment.Due, payment.Paid = "btc", "1.5e-5", "0.000015"
	require.True(t, nowPayMatches(payment, order))
}

func TestCheckoutCreditsOnlyVerifiedOrders(t *testing.T) {
	setupChannelTypeTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.PaymentOrder{}, &model.Log{}))
	oldLogDB, oldTransport := model.LOG_DB, paymentHTTP.Transport
	t.Cleanup(func() { model.LOG_DB, paymentHTTP.Transport = oldLogDB, oldTransport })
	model.LOG_DB = model.DB
	t.Setenv("PAYMENT_PUBLIC_URL", "https://platform.example")
	t.Setenv("PAYMENT_PACKAGES", `[{"id":"standard","name":"Standard","amount":1500,"quota":7500}]`)
	t.Setenv("STRIPE_SECRET_KEY", "stripe-key")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "stripe-secret")
	t.Setenv("NOWPAYMENTS_API_KEY", "now-key")
	t.Setenv("NOWPAYMENTS_IPN_SECRET", "now-secret")
	t.Setenv("NOWPAYMENTS_PAY_CURRENCIES", "usdtsol,usdcmatic")
	user := model.User{Username: "checkout-owner", Quota: 100}
	require.NoError(t, model.DB.Create(&user).Error)
	orderIDs := map[string]string{}
	nowPaid := "15.25"
	nowConfirmedCurrency := "usdcmatic"
	paymentHTTP.Transport = mediaTestTransport(func(req *http.Request) (*http.Response, error) {
		var response string
		switch req.URL.Host + req.URL.Path {
		case "api.stripe.com/v1/checkout/sessions":
			require.Equal(t, "Bearer stripe-key", req.Header.Get("Authorization"))
			form, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Contains(t, string(form), "unit_amount%5D=1500")
			orderIDs[stripeProvider] = req.Header.Get("Idempotency-Key")
			response = `{"id":"cs_mock","url":"https://checkout.stripe.com/c/pay/cs_mock"}`
		case "api.nowpayments.io/v1/invoice":
			require.Equal(t, "now-key", req.Header.Get("x-api-key"))
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), `"price_amount":15.00`)
			require.Contains(t, string(body), `"pay_currency":"usdcmatic"`)
			response = `{"id":9001,"invoice_url":"https://nowpayments.io/payment/?iid=9001"}`
		case "api.nowpayments.io/v1/payment/8001":
			require.Equal(t, "now-key", req.Header.Get("x-api-key"))
			response = fmt.Sprintf(`{"payment_id":8001,"invoice_id":9001,"order_id":%q,"payment_status":"finished","price_amount":15,"price_currency":"usd","pay_currency":%q,"pay_amount":15.25,"actually_paid":%s}`, orderIDs[nowProvider], nowConfirmedCurrency, nowPaid)
		default:
			t.Fatalf("unexpected gateway request %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	engine := gin.New()
	engine.POST("/orders", func(c *gin.Context) { c.Set(ctxkey.Id, user.Id); CreatePaymentOrder(c) })
	engine.GET("/orders", func(c *gin.Context) { c.Set(ctxkey.Id, user.Id); ListPaymentOrders(c) })
	engine.GET("/packages", ListPaymentPackages)
	engine.GET("/other", func(c *gin.Context) { c.Set(ctxkey.Id, user.Id+1); ListPaymentOrders(c) })
	engine.POST("/stripe", StripePaymentWebhook)
	engine.POST("/now", NowPaymentWebhook)
	send := func(method, path, body, header, signature string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if header != "" {
			req.Header.Set(header, signature)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	catalog := send(http.MethodGet, "/packages", "", "", "").Body.String()
	require.Contains(t, catalog, `"code":"usdtsol"`)
	require.Contains(t, catalog, `"code":"usdcmatic"`)
	require.NotContains(t, catalog, `"code":"usdttrc20"`)
	for _, code := range []string{"", "usdttrc20"} {
		response := send(http.MethodPost, "/orders", fmt.Sprintf(`{"package_id":"standard","provider":"nowpayments","pay_currency":%q}`, code), "", "")
		require.Contains(t, response.Body.String(), `"success":false`)
	}
	var orderCount int64
	require.NoError(t, model.DB.Model(&model.PaymentOrder{}).Count(&orderCount).Error)
	require.Zero(t, orderCount)
	for _, selection := range []struct{ provider, currency string }{{stripeProvider, ""}, {nowProvider, "usdcmatic"}} {
		provider := selection.provider
		response := send(http.MethodPost, "/orders", fmt.Sprintf(`{"package_id":"standard","provider":%q,"pay_currency":%q,"quota":123456,"amount":1}`, provider, selection.currency), "", "")
		require.Contains(t, response.Body.String(), `"success":true`)
		var order model.PaymentOrder
		require.NoError(t, model.DB.Where("provider = ?", provider).First(&order).Error)
		require.EqualValues(t, 7500, order.Quota)
		require.EqualValues(t, 1500, order.Amount)
		require.Equal(t, selection.currency, order.PayCurrency)
		require.NotNil(t, order.ProviderID)
		orderIDs[provider] = order.ID
	}
	require.Contains(t, send(http.MethodGet, "/orders", "", "", "").Body.String(), `"pay_currency_label":"USDC · Polygon"`)
	require.JSONEq(t, `{"success":true,"data":[]}`, send(http.MethodGet, "/other", "", "", "").Body.String())
	stripe := func(amount int) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"type":"checkout.session.completed","data":{"object":{"id":"cs_mock","client_reference_id":%q,"amount_total":%d,"currency":"usd","payment_status":"paid","mode":"payment"}}}`, orderIDs[stripeProvider], amount)
		ts := time.Now().Unix()
		mac := hmac.New(sha256.New, []byte("stripe-secret"))
		fmt.Fprintf(mac, "%d.%s", ts, body)
		return send(http.MethodPost, "/stripe", body, "Stripe-Signature", fmt.Sprintf("t=%d,v1=%x", ts, mac.Sum(nil)))
	}
	require.Equal(t, http.StatusBadRequest, send(http.MethodPost, "/stripe", "{}", "Stripe-Signature", "bad").Code)
	require.Equal(t, http.StatusOK, stripe(100).Code)
	quota, err := model.GetUserQuota(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 100, quota)
	require.Equal(t, http.StatusOK, stripe(1500).Code)
	require.Equal(t, http.StatusOK, stripe(1500).Code)
	now := func() *httptest.ResponseRecorder {
		canonical := `{"payment_id":8001,"payment_status":"finished"}`
		mac := hmac.New(sha512.New, []byte("now-secret"))
		mac.Write([]byte(canonical))
		return send(http.MethodPost, "/now", `{"payment_status":"finished","payment_id":8001}`, "x-nowpayments-sig", hex.EncodeToString(mac.Sum(nil)))
	}
	// A different network cannot credit the order even if it is enabled.
	nowConfirmedCurrency = "usdtsol"
	require.Equal(t, http.StatusOK, now().Code)
	quota, err = model.GetUserQuota(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 7600, quota)
	nowConfirmedCurrency = "usdcmatic"
	nowPaid = "14.99"
	require.Equal(t, http.StatusOK, now().Code)
	quota, err = model.GetUserQuota(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 7600, quota)
	t.Setenv("NOWPAYMENTS_PAY_CURRENCIES", "btc")
	nowPaid = "15.25"
	require.Equal(t, http.StatusOK, now().Code)
	require.Equal(t, http.StatusOK, now().Code)
	quota, err = model.GetUserQuota(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 15100, quota)
	var logCount int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeTopup).Count(&logCount).Error)
	require.EqualValues(t, 2, logCount)
}
