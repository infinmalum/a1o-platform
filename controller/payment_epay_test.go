package controller

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/stretchr/testify/require"
)

func TestEPaySignature(t *testing.T) {
	form := url.Values{"pid": {"1001"}, "money": {"99.00"}, "a0": {"2"}, "a": {"双拼"},
		"sign_type": {"MD5"}, "empty": {""}, "sign": {"ignore"}}
	require.Equal(t, "439dc53bf5a8845897ebf3edfe235e4b", hex.EncodeToString(epaySign(form, "legacy-test-key")))
}

func TestEPayCheckoutAndNotification(t *testing.T) {
	setupChannelTypeTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.PaymentOrder{}, &model.Log{}))
	previousLogDB := model.LOG_DB
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	model.LOG_DB = model.DB
	t.Setenv("PAYMENT_PUBLIC_URL", "https://platform.example")
	t.Setenv("PAYMENT_PACKAGES", `[{"id":"cny","name":"额度","cny_amount":9900,"quota":7500},{"id":"usd","name":"USD","amount":1500,"quota":1500}]`)
	t.Setenv("EPAY_GATEWAY_URL", "https://gateway.example/pay/")
	t.Setenv("EPAY_MERCHANT_ID", "1001")
	t.Setenv("EPAY_MERCHANT_KEY", "legacy-test-key")
	t.Setenv("EPAY_PAYMENT_TYPE", "")
	t.Setenv("STRIPE_SECRET_KEY", "stripe-key")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "stripe-secret")
	user := model.User{Username: "epay-owner", Quota: 100}
	require.NoError(t, model.DB.Create(&user).Error)

	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(ctxkey.Id, user.Id) })
	engine.GET("/packages", ListPaymentPackages)
	engine.POST("/orders", CreatePaymentOrder)
	engine.GET("/orders", ListPaymentOrders)
	engine.GET(epayNotifyPath, EPayPaymentWebhook)
	engine.POST(epayNotifyPath, EPayPaymentWebhook)
	engine.GET(epayReturnPath, PaymentReturn)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		return res
	}
	require.Contains(t, send(http.MethodGet, "/packages", "").Body.String(), `"cny_amount":9900`)
	require.Contains(t, send(http.MethodPost, "/orders", `{"package_id":"usd","provider":"epay"}`).Body.String(), `"success":false`)
	require.Contains(t, send(http.MethodPost, "/orders", `{"package_id":"cny","provider":"stripe"}`).Body.String(), `"success":false`)

	createOrder := func() model.PaymentOrder {
		resp := send(http.MethodPost, "/orders", `{"package_id":"cny","provider":"epay"}`)
		var result struct {
			Success bool
			Data    model.PaymentOrder
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		require.True(t, result.Success, resp.Body.String())
		return result.Data
	}
	order := createOrder()
	require.NoError(t, model.DB.Model(&order).UpdateColumn("protocol", nil).Error)
	t.Setenv("EPAY_PAYMENT_TYPE", "wxpay")
	otherOrder := createOrder()
	require.Contains(t, otherOrder.CheckoutURL, "type=wxpay")
	require.Equal(t, "cny", order.Currency)
	require.EqualValues(t, 9900, order.Amount)
	checkout, err := url.Parse(order.CheckoutURL)
	require.NoError(t, err)
	require.Equal(t, "https", checkout.Scheme)
	require.Equal(t, "gateway.example", checkout.Host)
	require.Equal(t, "/pay/submit.php", checkout.Path)
	require.Equal(t, "99.00", checkout.Query().Get("money"))
	require.Equal(t, order.ID, checkout.Query().Get("out_trade_no"))
	require.Equal(t, "https://platform.example"+epayNotifyPath, checkout.Query().Get("notify_url"))
	require.Equal(t, "https://platform.example"+epayReturnPath, checkout.Query().Get("return_url"))
	require.Empty(t, checkout.Query().Get("type"))
	require.True(t, verifyEPayForm(checkout.Query(), loadEPayConfig()))
	require.NotContains(t, order.CheckoutURL, "legacy-test-key")
	require.Contains(t, send(http.MethodGet, "/orders", "").Body.String(), order.ID)

	callback := url.Values{"pid": {"1001"}, "out_trade_no": {order.ID}, "trade_no": {"100123"},
		"money": {"99.00"}, "sign_type": {"MD5"}, "trade_status": {"TRADE_SUCCESS"}, "type": {"alipay"}}
	sign := func() { callback.Set("sign", hex.EncodeToString(epaySign(callback, "legacy-test-key"))) }
	notify := func() *httptest.ResponseRecorder {
		return send(http.MethodGet, epayNotifyPath+"?"+callback.Encode(), "")
	}
	sign()
	ret := send(http.MethodGet, epayReturnPath+"?"+callback.Encode(), "")
	require.Equal(t, http.StatusSeeOther, ret.Code)
	require.Equal(t, "/account?tab=redeem&payment=return", ret.Header().Get("Location"))
	callback.Set("money", "0.01")
	require.Equal(t, http.StatusBadRequest, notify().Code)
	callback.Set("pid", "1002")
	sign()
	require.Equal(t, http.StatusBadRequest, notify().Code)
	callback.Set("pid", "1001")
	sign()
	require.Equal(t, http.StatusBadRequest, notify().Code)
	var stored model.PaymentOrder
	require.NoError(t, model.DB.First(&stored, "id = ?", order.ID).Error)
	require.Equal(t, "review", stored.State)
	require.NoError(t, model.DB.First(&user, user.Id).Error)
	require.EqualValues(t, 100, user.Quota)

	// Changing the catalog or disabling new checkouts leaves pending orders payable at their original price.
	t.Setenv("EPAY_GATEWAY_URL", "")
	t.Setenv("PAYMENT_PACKAGES", "")
	callback.Set("money", "99")
	sign()
	duplicateField := send(http.MethodGet, epayNotifyPath+"?"+callback.Encode()+"&money=99", "")
	require.Equal(t, http.StatusBadRequest, duplicateField.Code)
	require.Equal(t, "success", notify().Body.String())
	replay := send(http.MethodPost, epayNotifyPath, callback.Encode())
	require.Equal(t, http.StatusOK, replay.Code)
	require.Equal(t, "success", replay.Body.String())
	require.NoError(t, model.DB.First(&stored, "id = ?", order.ID).Error)
	require.Equal(t, "credited", stored.State)
	require.NotZero(t, stored.CreditedAt)
	require.Empty(t, stored.CheckoutURL)
	require.NotNil(t, stored.ProviderID)
	require.Equal(t, "100123", *stored.ProviderID)

	callback.Set("out_trade_no", otherOrder.ID)
	sign()
	require.NotEqual(t, "success", notify().Body.String())
	require.NoError(t, model.DB.First(&user, user.Id).Error)
	require.EqualValues(t, 7600, user.Quota)
}

func TestEPayV2CheckoutAndNotification(t *testing.T) {
	setupChannelTypeTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.PaymentOrder{}, &model.Log{}))
	previousLogDB := model.LOG_DB
	t.Cleanup(func() { model.LOG_DB = previousLogDB })
	model.LOG_DB = model.DB
	merchantKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	platformKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	platformDER, err := x509.MarshalPKIXPublicKey(&platformKey.PublicKey)
	require.NoError(t, err)

	t.Setenv("PAYMENT_PUBLIC_URL", "https://platform.example")
	t.Setenv("PAYMENT_PACKAGES", `[{"id":"cny","name":"额度","cny_amount":9900,"quota":7500}]`)
	t.Setenv("EPAY_VERSION", "v2")
	t.Setenv("EPAY_V2_GATEWAY_URL", "https://rsa.example/merchant")
	t.Setenv("EPAY_V2_MERCHANT_ID", "2002")
	t.Setenv("EPAY_V2_MERCHANT_PRIVATE_KEY", base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(merchantKey)))
	t.Setenv("EPAY_V2_PLATFORM_PUBLIC_KEY", base64.StdEncoding.EncodeToString(platformDER))
	t.Setenv("EPAY_MERCHANT_ID", "1001")
	t.Setenv("EPAY_MERCHANT_KEY", "legacy-test-key")

	user := model.User{Username: "epay-v2-owner", Quota: 100}
	require.NoError(t, model.DB.Create(&user).Error)
	engine := gin.New()
	actor := user.Id
	engine.Use(func(c *gin.Context) { c.Set(ctxkey.Id, actor) })
	engine.POST("/orders", CreatePaymentOrder)
	engine.GET("/orders", ListPaymentOrders)
	engine.GET(epayCheckoutPath+"/:id", EPayV2Checkout)
	engine.GET(epayNotifyPath, EPayPaymentWebhook)
	engine.POST(epayNotifyPath, EPayPaymentWebhook)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	res := send(http.MethodPost, "/orders", `{"package_id":"cny","provider":"epay"}`)
	var checkout struct {
		Success bool
		Data    model.PaymentOrder
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &checkout))
	require.True(t, checkout.Success, res.Body.String())
	order := checkout.Data
	require.Equal(t, epayCheckoutPath+"/"+order.ID, order.CheckoutURL)
	require.Contains(t, send(http.MethodGet, "/orders", "").Body.String(), order.CheckoutURL)
	actor++
	require.Equal(t, http.StatusNotFound, send(http.MethodGet, order.CheckoutURL, "").Code)
	actor--

	t.Setenv("EPAY_VERSION", "off")
	t.Setenv("EPAY_V2_GATEWAY_URL", "")
	redirect := send(http.MethodGet, order.CheckoutURL, "")
	require.Equal(t, http.StatusSeeOther, redirect.Code)
	target, err := url.Parse(redirect.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "rsa.example", target.Host)
	require.Equal(t, "/merchant/api/pay/submit", target.Path)
	query := target.Query()
	ts, err := strconv.ParseInt(query.Get("timestamp"), 10, 64)
	require.NoError(t, err)
	require.InDelta(t, time.Now().Unix(), ts, 5)
	require.Equal(t, "RSA", query.Get("sign_type"))
	signature, err := base64.StdEncoding.DecodeString(query.Get("sign"))
	require.NoError(t, err)
	digest := sha256.Sum256([]byte("money=99.00&name=额度&notify_url=https://platform.example" + epayNotifyPath +
		"&out_trade_no=" + order.ID + "&pid=2002&return_url=https://platform.example" + epayReturnPath + "&timestamp=" + query.Get("timestamp")))
	require.NoError(t, rsa.VerifyPKCS1v15(&merchantKey.PublicKey, crypto.SHA256, digest[:], signature))

	legacy := url.Values{"pid": {"1001"}, "out_trade_no": {order.ID}, "trade_no": {"legacy-trade"}, "trade_status": {"TRADE_SUCCESS"}, "money": {"99.00"}, "sign_type": {"MD5"}}
	legacy.Set("sign", hex.EncodeToString(epaySign(legacy, "legacy-test-key")))
	require.Equal(t, http.StatusBadRequest, send(http.MethodGet, epayNotifyPath+"?"+legacy.Encode(), "").Code)

	notify := func(key *rsa.PrivateKey, timestamp time.Time) *httptest.ResponseRecorder {
		ts := strconv.FormatInt(timestamp.Unix(), 10)
		form := url.Values{"pid": {"2002"}, "out_trade_no": {order.ID}, "trade_no": {"rsa-trade"}, "trade_status": {"TRADE_SUCCESS"},
			"money": {"99.00"}, "sign_type": {"RSA"}, "timestamp": {ts}}
		digest := sha256.Sum256([]byte("money=99.00&out_trade_no=" + order.ID + "&pid=2002&timestamp=" + ts +
			"&trade_no=rsa-trade&trade_status=TRADE_SUCCESS"))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		require.NoError(t, err)
		form.Set("sign", base64.StdEncoding.EncodeToString(signature))
		return send(http.MethodPost, epayNotifyPath, form.Encode())
	}
	require.Equal(t, http.StatusBadRequest, notify(merchantKey, time.Now()).Code)
	require.Equal(t, http.StatusBadRequest, notify(platformKey, time.Now().Add(-6*time.Minute)).Code)
	quota, err := model.GetUserQuota(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 100, quota)
	require.Equal(t, "success", notify(platformKey, time.Now()).Body.String())
	require.Equal(t, "success", notify(platformKey, time.Now()).Body.String())
	quota, err = model.GetUserQuota(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 7600, quota)
	require.Equal(t, http.StatusNotFound, send(http.MethodGet, order.CheckoutURL, "").Code)
}
