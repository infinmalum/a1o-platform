package controller

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/infinmalum/a1o-platform/common/ctxkey"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
)

const (
	epayProvider     = "epay"
	epayNotifyPath   = "/api/payments/webhook/epay"
	epayReturnPath   = "/api/payments/return/epay"
	epayCheckoutPath = "/api/payments/checkout/epay"
	epayV1           = "v1"
	epayV2           = "v2"
	epayMaxForm      = 8192
)

var (
	epayMerchantID = regexp.MustCompile(`^[0-9]{1,32}$`)
	epayFieldName  = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)
	epayMoney      = regexp.MustCompile(`^[0-9]{1,8}(\.[0-9]{1,2})?$`)
)

type epayConfig struct {
	gateway     string
	merchantID  string
	key         string
	paymentType string
	version     string
}

func loadEPayConfig() epayConfig {
	version := strings.ToLower(strings.TrimSpace(os.Getenv("EPAY_VERSION")))
	if version == "" {
		version = epayV1
	}
	return loadEPayVersion(version)
}

func loadEPayVersion(version string) epayConfig {
	prefix := "EPAY_"
	if version == epayV2 {
		prefix = "EPAY_V2_"
	}
	return epayConfig{
		gateway:     strings.TrimRight(strings.TrimSpace(os.Getenv(prefix+"GATEWAY_URL")), "/"),
		merchantID:  strings.TrimSpace(os.Getenv(prefix + "MERCHANT_ID")),
		key:         strings.TrimSpace(os.Getenv("EPAY_MERCHANT_KEY")),
		paymentType: strings.TrimSpace(os.Getenv("EPAY_PAYMENT_TYPE")),
		version:     version,
	}
}

func (cfg epayConfig) checkoutEnabled() bool {
	gateway, err := url.Parse(cfg.gateway)
	if err != nil || gateway.Scheme != "https" || gateway.Hostname() == "" ||
		gateway.User != nil || gateway.RawQuery != "" || gateway.ForceQuery || gateway.Fragment != "" ||
		!epayMerchantID.MatchString(cfg.merchantID) ||
		(cfg.paymentType != "" && !epayFieldName.MatchString(cfg.paymentType)) {
		return false
	}
	switch cfg.version {
	case epayV1:
		return cfg.key != ""
	case epayV2:
		return epayPrivateKey() != nil && epayPlatformKey() != nil
	default:
		return false
	}
}

func (cfg epayConfig) checkout(site string, order model.PaymentOrder) string {
	form := url.Values{
		"pid":          {cfg.merchantID},
		"type":         {cfg.paymentType},
		"out_trade_no": {order.ID},
		"notify_url":   {site + epayNotifyPath},
		"return_url":   {site + epayReturnPath},
		"name":         {strings.ReplaceAll(order.PackageName, "&", "＆")},
		"money":        {fmt.Sprintf("%d.%02d", order.Amount/100, order.Amount%100)},
	}
	if cfg.paymentType == "" {
		form.Del("type")
	}
	if cfg.version == epayV2 {
		return cfg.gateway + "/api/pay/submit?" + form.Encode()
	}
	form.Set("sign_type", "MD5")
	form.Set("sign", hex.EncodeToString(epaySign(form, cfg.key)))
	return cfg.gateway + "/submit.php?" + form.Encode()
}

// The legacy EPay protocol signs sorted, unescaped fields with a merchant-key suffix.
func epaySign(form url.Values, secret string) []byte {
	sum := md5.Sum([]byte(epayCanonical(form) + secret))
	return sum[:]
}

func epayCanonical(form url.Values) string {
	fields := make([]string, 0, len(form))
	for _, key := range slices.Sorted(maps.Keys(form)) {
		if value := form.Get(key); key != "sign" && key != "sign_type" && value != "" {
			fields = append(fields, key+"="+value)
		}
	}
	return strings.Join(fields, "&")
}

func verifyEPayForm(form url.Values, cfg epayConfig) bool {
	for key, values := range form {
		// Unescaped ampersands would make two different forms sign the same string.
		if !epayFieldName.MatchString(key) || len(values) != 1 || strings.Contains(values[0], "&") {
			return false
		}
	}
	if !epayMerchantID.MatchString(cfg.merchantID) || form.Get("pid") != cfg.merchantID {
		return false
	}
	if cfg.version == epayV2 {
		raw := form.Get("timestamp")
		timestamp, err := strconv.ParseInt(raw, 10, 64)
		now := time.Now().Unix()
		if err != nil || form.Get("sign_type") != "RSA" || timestamp < now-300 || timestamp > now+300 {
			return false
		}
		key := epayPlatformKey()
		signature, err := base64.StdEncoding.DecodeString(form.Get("sign"))
		digest := sha256.Sum256([]byte(epayCanonical(form)))
		return err == nil && key != nil && rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) == nil
	}
	signature, err := hex.DecodeString(form.Get("sign"))
	return cfg.key != "" && err == nil && form.Get("sign_type") == "MD5" &&
		subtle.ConstantTimeCompare(signature, epaySign(form, cfg.key)) == 1
}

func epayKeyDER(name string) []byte {
	value := strings.TrimSpace(os.Getenv(name))
	if block, _ := pem.Decode([]byte(value)); block != nil {
		return block.Bytes
	}
	decoded, _ := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(value), ""))
	return decoded
}

func epayPrivateKey() *rsa.PrivateKey {
	der := epayKeyDER("EPAY_V2_MERCHANT_PRIVATE_KEY")
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key
	}
	parsed, _ := x509.ParsePKCS8PrivateKey(der)
	key, _ := parsed.(*rsa.PrivateKey)
	return key
}

func epayPlatformKey() *rsa.PublicKey {
	der := epayKeyDER("EPAY_V2_PLATFORM_PUBLIC_KEY")
	if key, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return key
	}
	parsed, _ := x509.ParsePKIXPublicKey(der)
	key, _ := parsed.(*rsa.PublicKey)
	return key
}

func signEPayV2(form url.Values, key *rsa.PrivateKey) error {
	if key == nil {
		return errors.New("EPay V2 merchant private key unavailable")
	}
	digest := sha256.Sum256([]byte(epayCanonical(form)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return err
	}
	form.Set("sign", base64.StdEncoding.EncodeToString(signature))
	form.Set("sign_type", "RSA")
	return nil
}

func EPayV2Checkout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.String() != c.Param("id") {
		c.Status(http.StatusNotFound)
		return
	}
	var order model.PaymentOrder
	query := model.DB.WithContext(c.Request.Context()).
		Where("id = ? AND user_id = ? AND provider = ? AND protocol = ? AND credited_at = 0 AND checkout_url <> ''",
			id.String(), c.GetInt(ctxkey.Id), epayProvider, epayV2).Find(&order)
	if query.Error != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if query.RowsAffected == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	target, err := url.Parse(order.CheckoutURL)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	form := target.Query()
	form.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	if cfg := loadEPayVersion(epayV2); form.Get("pid") != cfg.merchantID ||
		signEPayV2(form, epayPrivateKey()) != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	target.RawQuery = form.Encode()
	c.Redirect(http.StatusSeeOther, target.String())
}

func EPayPaymentWebhook(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	raw := c.Request.URL.RawQuery
	if c.Request.Method == http.MethodPost {
		if raw != "" {
			c.String(http.StatusBadRequest, "fail")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, epayMaxForm))
		if err != nil {
			c.String(http.StatusBadRequest, "fail")
			return
		}
		raw = string(body)
	}
	if len(raw) > epayMaxForm {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	form, err := url.ParseQuery(raw)
	version := epayV1
	if form.Get("sign_type") == "RSA" {
		version = epayV2
	}
	if err != nil || !verifyEPayForm(form, loadEPayVersion(version)) {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if form.Get("trade_status") != "TRADE_SUCCESS" {
		c.String(http.StatusOK, "success")
		return
	}
	reference, tradeID, money := form.Get("out_trade_no"), form.Get("trade_no"), form.Get("money")
	id, err := uuid.Parse(reference)
	if err != nil || id.String() != reference || !paymentIdentifier.MatchString(tradeID) || !epayMoney.MatchString(money) {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	ctx := c.Request.Context()
	db := model.DB.WithContext(ctx)
	var order model.PaymentOrder
	query := db.Where("id = ? AND provider = ?", reference, epayProvider).Limit(1).Find(&order)
	if query.Error != nil {
		c.String(http.StatusServiceUnavailable, "fail")
		return
	}
	orderVersion := order.Protocol
	if orderVersion == "" {
		orderVersion = epayV1
	}
	if query.RowsAffected == 0 || orderVersion != version {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	total := paymentAmount(paymentNumber(money))
	if order.Currency != "cny" || total == nil || total.Cmp(big.NewRat(order.Amount, 100)) != 0 ||
		(order.ProviderID != nil && *order.ProviderID != tradeID) {
		if err := db.Model(&order).Where("credited_at = 0 AND state = ?", "pending").Update("state", "review").Error; err != nil {
			c.String(http.StatusServiceUnavailable, "fail")
			return
		}
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if order.CreditedAt == 0 {
		if err := model.CreditPaymentOrder(ctx, order, tradeID); err != nil {
			logger.Error(ctx, "failed to credit EPay order "+order.ID+": "+err.Error())
			c.String(http.StatusServiceUnavailable, "fail")
			return
		}
	}
	c.String(http.StatusOK, "success")
}
