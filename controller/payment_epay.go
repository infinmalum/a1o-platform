package controller

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/model"
)

const (
	epayProvider   = "epay"
	epayNotifyPath = "/api/payments/webhook/epay"
	epayReturnPath = "/api/payments/return/epay"
	epayMaxForm    = 8192
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
}

func loadEPayConfig() epayConfig {
	return epayConfig{
		gateway:     strings.TrimRight(strings.TrimSpace(os.Getenv("EPAY_GATEWAY_URL")), "/"),
		merchantID:  strings.TrimSpace(os.Getenv("EPAY_MERCHANT_ID")),
		key:         strings.TrimSpace(os.Getenv("EPAY_MERCHANT_KEY")),
		paymentType: strings.TrimSpace(os.Getenv("EPAY_PAYMENT_TYPE")),
	}
}

func (cfg epayConfig) checkoutEnabled() bool {
	gateway, err := url.Parse(cfg.gateway)
	return err == nil && gateway.Scheme == "https" && gateway.Hostname() != "" &&
		gateway.User == nil && gateway.RawQuery == "" && !gateway.ForceQuery && gateway.Fragment == "" &&
		epayMerchantID.MatchString(cfg.merchantID) && cfg.key != "" &&
		(cfg.paymentType == "" || epayFieldName.MatchString(cfg.paymentType))
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
		"sign_type":    {"MD5"},
	}
	if cfg.paymentType == "" {
		form.Del("type")
	}
	form.Set("sign", hex.EncodeToString(epaySign(form, cfg.key)))
	return cfg.gateway + "/submit.php?" + form.Encode()
}

// The legacy EPay protocol signs sorted, unescaped fields with a merchant-key suffix.
func epaySign(form url.Values, secret string) []byte {
	fields := make([]string, 0, len(form))
	for _, key := range slices.Sorted(maps.Keys(form)) {
		if value := form.Get(key); key != "sign" && key != "sign_type" && value != "" {
			fields = append(fields, key+"="+value)
		}
	}
	sum := md5.Sum([]byte(strings.Join(fields, "&") + secret))
	return sum[:]
}

func verifyEPayForm(form url.Values, cfg epayConfig) bool {
	for key, values := range form {
		// Unescaped ampersands would make two different forms sign the same string.
		if !epayFieldName.MatchString(key) || len(values) != 1 || strings.Contains(values[0], "&") {
			return false
		}
	}
	signature, err := hex.DecodeString(form.Get("sign"))
	return epayMerchantID.MatchString(cfg.merchantID) && cfg.key != "" && err == nil &&
		form.Get("pid") == cfg.merchantID && form.Get("sign_type") == "MD5" &&
		subtle.ConstantTimeCompare(signature, epaySign(form, cfg.key)) == 1
}

func EPayPaymentReturn(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusSeeOther, "/account?tab=redeem&payment=return")
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
	if err != nil || !verifyEPayForm(form, loadEPayConfig()) {
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
	if query.RowsAffected == 0 {
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
