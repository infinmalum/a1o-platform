package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/model"
)

const gmpayProvider = "gmpay"

var (
	gmpayTradeID       = regexp.MustCompile(`^[0-9]{20}$`)
	gmpayField         = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	gmpayFieldBoundary = regexp.MustCompile(`&[A-Za-z_][A-Za-z0-9_]*=`)
)

type gmpayConfig struct {
	gateway     *url.URL
	pid, secret string
}

func loadGMPayConfig() gmpayConfig {
	gateway, _ := url.Parse(strings.TrimRight(strings.TrimSpace(os.Getenv("GMPAY_GATEWAY_URL")), "/"))
	return gmpayConfig{gateway: gateway, pid: strings.TrimSpace(os.Getenv("GMPAY_PID")), secret: os.Getenv("GMPAY_API_SECRET")}
}

func (cfg gmpayConfig) enabled() bool {
	u := cfg.gateway
	return u != nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
		u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && epayMerchantID.MatchString(cfg.pid) && cfg.secret != ""
}

func gmpaySignature(fields map[string]string, secret string) ([]byte, error) {
	parts := make([]string, 0, len(fields))
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		value := fields[key]
		if key == "signature" || value == "" {
			continue
		}
		if !gmpayField.MatchString(key) || gmpayFieldBoundary.MatchString(value) {
			return nil, errors.New("ambiguous GMPay signature fields")
		}
		parts = append(parts, key+"="+value)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join(parts, "&")))
	return mac.Sum(nil), nil
}

func createGMPayOrder(ctx context.Context, cfg gmpayConfig, site string, order model.PaymentOrder) (string, string, error) {
	fields := map[string]string{
		"pid": cfg.pid, "order_id": order.ID, "currency": strings.ToUpper(order.Currency),
		"amount":       fmt.Sprintf("%d.%02d", order.Amount/100, order.Amount%100),
		"notify_url":   site + "/api/payments/webhook/gmpay",
		"redirect_url": site + "/api/payments/return/gmpay",
		"name":         order.PackageName,
	}
	// The protocol treats ampersand-plus-field-name as a parameter boundary.
	fields["name"] = gmpayFieldBoundary.ReplaceAllStringFunc(fields["name"], func(s string) string { return "＆" + s[1:] })
	signature, err := gmpaySignature(fields, cfg.secret)
	if err != nil {
		return "", "", err
	}
	fields["signature"] = hex.EncodeToString(signature)
	body, err := json.Marshal(fields)
	if err != nil {
		return "", "", err
	}
	var response struct {
		Code int `json:"status_code"`
		Data struct {
			ID        string        `json:"trade_id"`
			Reference string        `json:"order_id"`
			URL       string        `json:"payment_url"`
			Amount    paymentNumber `json:"amount"`
			Currency  string        `json:"currency"`
		} `json:"data"`
	}
	err = gatewayRequest(ctx, http.MethodPost, cfg.gateway.String()+"/payments/gmpay/v1/order/create-transaction",
		bytes.NewReader(body), map[string]string{"Content-Type": "application/json"}, &response)
	if err != nil {
		return "", "", err
	}
	orderData := response.Data
	total := paymentAmount(orderData.Amount)
	if response.Code != 200 || !gmpayTradeID.MatchString(orderData.ID) || orderData.Reference != order.ID ||
		!strings.EqualFold(orderData.Currency, order.Currency) || total == nil || total.Cmp(big.NewRat(order.Amount, 100)) != 0 {
		return "", "", errors.New("GMPay order response does not match")
	}
	return orderData.ID, orderData.URL, nil
}

func gmpayNotification(raw []byte) (map[string]string, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	if payload == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid GMPay notification")
	}
	fields := make(map[string]string, len(payload))
	for key, value := range payload {
		switch v := value.(type) {
		case string:
			fields[key] = v
		case json.Number:
			fields[key] = string(v)
		case nil:
		default:
			return nil, errors.New("invalid GMPay notification field")
		}
	}
	return fields, nil
}

func GMPayPaymentWebhook(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	cfg := loadGMPayConfig()
	fields, err := gmpayNotification(raw)
	if err != nil || cfg.secret == "" || !epayMerchantID.MatchString(cfg.pid) || fields["pid"] != cfg.pid {
		c.Status(http.StatusBadRequest)
		return
	}
	expected, err := gmpaySignature(fields, cfg.secret)
	signature, decodeErr := hex.DecodeString(fields["signature"])
	if err != nil || decodeErr != nil || !hmac.Equal(expected, signature) || !gmpayTradeID.MatchString(fields["trade_id"]) {
		c.Status(http.StatusBadRequest)
		return
	}
	reference, tradeID := fields["order_id"], fields["trade_id"]
	switch fields["status"] {
	case "1":
		c.Status(http.StatusOK)
	case "2":
		total := paymentAmount(paymentNumber(fields["amount"]))
		processPaidOrder(c, gmpayProvider, reference, func(order model.PaymentOrder) bool {
			return *order.ProviderID == tradeID && total != nil && total.Cmp(big.NewRat(order.Amount, 100)) == 0
		})
	case "3":
		updatePaymentState(c, gmpayProvider, reference, tradeID, "closed")
	default:
		c.Status(http.StatusBadRequest)
		return
	}
	if c.Writer.Status() == http.StatusOK {
		c.String(http.StatusOK, "ok")
	}
}
