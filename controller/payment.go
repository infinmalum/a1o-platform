package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	stripeProvider     = "stripe"
	nowProvider        = "nowpayments"
	legacyNowCurrency  = "usdttrc20"
	maxPaymentResponse = 1 << 20
)

var paymentHTTP = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type quotaPackage struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Amount    int64  `json:"amount"`
	CNYAmount int64  `json:"cny_amount,omitempty"`
	Quota     int64  `json:"quota"`
}

func (p quotaPackage) price(provider string) (int64, string) {
	if provider == epayProvider {
		return p.CNYAmount, "cny"
	}
	return p.Amount, "usd"
}

type paymentConfig struct {
	site, stripeKey, stripeSecret, nowKey, nowSecret string
	packages                                         []quotaPackage
	providers                                        []string
	nowCurrencies                                    []nowCurrencyOption
	epay                                             epayConfig
}

type nowCurrencyOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

var nowCurrencyLabels = map[string]string{
	legacyNowCurrency: "USDT · TRON (TRC20)",
	"usdterc20":       "USDT · Ethereum (ERC20)",
	"usdtbsc":         "USDT · BNB Smart Chain (BEP20)",
	"usdtsol":         "USDT · Solana",
	"usdtmatic":       "USDT · Polygon",
	"usdc":            "USDC · Ethereum (ERC20)",
	"usdcsol":         "USDC · Solana",
	"usdcmatic":       "USDC · Polygon",
	"btc":             "BTC · Bitcoin",
	"eth":             "ETH · Ethereum",
	"etharb":          "ETH · Arbitrum",
	"sol":             "SOL · Solana",
}

func parseNowCurrencies(raw string) ([]nowCurrencyOption, error) {
	options := []nowCurrencyOption{}
	if strings.TrimSpace(raw) == "" {
		return options, nil
	}
	seen := make(map[string]bool)
	for _, entry := range strings.Split(raw, ",") {
		code := strings.ToLower(strings.TrimSpace(entry))
		label, ok := nowCurrencyLabels[code]
		if !ok {
			return nil, fmt.Errorf("unsupported code %q in NOWPAYMENTS_PAY_CURRENCIES", code)
		}
		if !seen[code] {
			options = append(options, nowCurrencyOption{Code: code, Label: label})
			seen[code] = true
		}
	}
	return options, nil
}

func loadPaymentConfig() (paymentConfig, error) {
	cfg := paymentConfig{
		site:      strings.TrimRight(os.Getenv("PAYMENT_PUBLIC_URL"), "/"),
		stripeKey: os.Getenv("STRIPE_SECRET_KEY"), stripeSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		nowKey: os.Getenv("NOWPAYMENTS_API_KEY"), nowSecret: os.Getenv("NOWPAYMENTS_IPN_SECRET"),
		packages: []quotaPackage{}, providers: []string{}, nowCurrencies: []nowCurrencyOption{},
	}
	raw := os.Getenv("PAYMENT_PACKAGES")
	if raw == "" {
		return cfg, nil
	}
	site, err := url.Parse(cfg.site)
	if err != nil || site.Scheme != "https" || site.Hostname() == "" || site.User != nil ||
		site.Path != "" || site.RawQuery != "" || site.Fragment != "" || site.ForceQuery {
		return cfg, errors.New("PAYMENT_PUBLIC_URL must be an HTTPS origin")
	}
	if len(raw) > 32768 || json.Unmarshal([]byte(raw), &cfg.packages) != nil || cfg.packages == nil || len(cfg.packages) > 24 {
		return cfg, errors.New("invalid PAYMENT_PACKAGES")
	}
	ids := map[string]bool{}
	for _, pack := range cfg.packages {
		if !paymentIdentifier.MatchString(pack.ID) || ids[pack.ID] || strings.TrimSpace(pack.Name) == "" ||
			len(pack.Name) > 100 || (pack.Amount == 0 && pack.CNYAmount == 0) ||
			(pack.Amount != 0 && (pack.Amount < 50 || pack.Amount > 99999999)) ||
			(pack.CNYAmount != 0 && (pack.CNYAmount < 1 || pack.CNYAmount > 99999999)) ||
			pack.Quota < 1 || pack.Quota > 1_000_000_000_000 {
			return cfg, fmt.Errorf("invalid payment package %q", pack.ID)
		}
		ids[pack.ID] = true
	}
	if cfg.stripeKey != "" && cfg.stripeSecret != "" {
		cfg.providers = append(cfg.providers, stripeProvider)
	}
	if cfg.nowKey != "" && cfg.nowSecret != "" {
		options, err := parseNowCurrencies(os.Getenv("NOWPAYMENTS_PAY_CURRENCIES"))
		if err != nil {
			logger.Error(context.Background(), "NOWPayments: "+err.Error())
		} else if len(options) > 0 {
			cfg.nowCurrencies = options
			cfg.providers = append(cfg.providers, nowProvider)
		}
	}
	cfg.epay = loadEPayConfig()
	if cfg.epay.checkoutEnabled() {
		cfg.providers = append(cfg.providers, epayProvider)
	}
	return cfg, nil
}

var paymentIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var decimalPaymentAmount = regexp.MustCompile(`^[0-9]{1,20}(\.[0-9]{1,20})?([eE][+-]?[0-9]{1,2})?$`)

func ListPaymentPackages(c *gin.Context) {
	cfg, err := loadPaymentConfig()
	if err != nil {
		paymentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"packages": cfg.packages, "providers": cfg.providers, "nowpayments_currencies": cfg.nowCurrencies,
	}})
}

type paymentOrderResponse struct {
	model.PaymentOrder
	PayCurrencyLabel string `json:"pay_currency_label,omitempty"`
}

func presentPaymentOrder(order model.PaymentOrder) paymentOrderResponse {
	if order.Provider == nowProvider {
		order.PayCurrency = orderNowCurrency(order)
	}
	return paymentOrderResponse{PaymentOrder: order, PayCurrencyLabel: nowCurrencyLabels[order.PayCurrency]}
}

func ListPaymentOrders(c *gin.Context) {
	rows := []model.PaymentOrder{}
	if err := model.DB.WithContext(c.Request.Context()).
		Where("user_id = ? AND (provider_id IS NOT NULL OR (provider = ? AND checkout_url <> ''))", c.GetInt(ctxkey.Id), epayProvider).
		Order("created_at desc, id desc").Limit(20).Find(&rows).Error; err != nil {
		paymentError(c, err)
		return
	}
	orders := make([]paymentOrderResponse, 0, len(rows))
	for _, row := range rows {
		orders = append(orders, presentPaymentOrder(row))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": orders})
}

func CreatePaymentOrder(c *gin.Context) {
	var input struct {
		Package     string `json:"package_id"`
		Provider    string `json:"provider"`
		PayCurrency string `json:"pay_currency"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.ShouldBindJSON(&input); err != nil {
		paymentError(c, err)
		return
	}
	cfg, err := loadPaymentConfig()
	if err != nil {
		paymentError(c, err)
		return
	}
	var selected quotaPackage
	for _, pack := range cfg.packages {
		if pack.ID == input.Package {
			selected = pack
			break
		}
	}
	amount, currency := selected.price(input.Provider)
	if selected.ID == "" || amount == 0 || !slices.Contains(cfg.providers, input.Provider) {
		paymentError(c, errors.New("payment package or provider unavailable"))
		return
	}
	payCurrency := strings.ToLower(strings.TrimSpace(input.PayCurrency))
	if input.Provider == nowProvider {
		if payCurrency == "" && len(cfg.nowCurrencies) == 1 {
			payCurrency = cfg.nowCurrencies[0].Code
		}
		if !slices.ContainsFunc(cfg.nowCurrencies, func(option nowCurrencyOption) bool {
			return option.Code == payCurrency
		}) {
			paymentError(c, errors.New("NOWPayments currency unavailable"))
			return
		}
	} else if payCurrency != "" {
		paymentError(c, errors.New("crypto payment currency requires NOWPayments"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 20*time.Second)
	defer cancel()
	order := model.PaymentOrder{ID: uuid.NewString(), UserID: c.GetInt(ctxkey.Id),
		PackageName: selected.Name, Amount: amount, Currency: currency,
		Quota: selected.Quota, Provider: input.Provider, PayCurrency: payCurrency}
	db := model.DB.WithContext(ctx)
	if order.Provider == epayProvider {
		order.CheckoutURL = cfg.epay.checkout(cfg.site, order)
		if err := db.Create(&order).Error; err != nil {
			paymentError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": presentPaymentOrder(order)})
		return
	}
	if err := db.Create(&order).Error; err != nil {
		paymentError(c, err)
		return
	}
	var id, checkoutURL string
	switch order.Provider {
	case stripeProvider:
		id, checkoutURL, err = createStripeCheckout(ctx, cfg, order)
	case nowProvider:
		id, checkoutURL, err = createNowInvoice(ctx, cfg, order)
	}
	if err == nil {
		target, parseErr := url.Parse(checkoutURL)
		if !paymentIdentifier.MatchString(id) || parseErr != nil || target.Scheme != "https" ||
			target.User != nil || target.Port() != "" || !allowedCheckoutHost(order.Provider, target.Hostname()) {
			err = errors.New("invalid gateway checkout")
		}
	}
	if err == nil {
		err = db.Model(&order).Updates(map[string]any{"provider_id": id, "checkout_url": checkoutURL}).Error
	}
	if err != nil {
		paymentError(c, err)
		return
	}
	order.CheckoutURL = checkoutURL
	c.JSON(http.StatusOK, gin.H{"success": true, "data": presentPaymentOrder(order)})
}

func allowedCheckoutHost(provider, host string) bool {
	switch provider {
	case stripeProvider:
		return host == "checkout.stripe.com"
	case nowProvider:
		return host == "nowpayments.io"
	default:
		return false
	}
}

type stripeCheckout struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Reference string `json:"client_reference_id"`
	Amount    int64  `json:"amount_total"`
	Currency  string `json:"currency"`
	Status    string `json:"payment_status"`
	Mode      string `json:"mode"`
}

func createStripeCheckout(ctx context.Context, cfg paymentConfig, order model.PaymentOrder) (string, string, error) {
	target := cfg.site + "/account?tab=redeem"
	form := url.Values{
		"mode": {"payment"}, "client_reference_id": {order.ID},
		"success_url": {target + "&payment=return"}, "cancel_url": {target},
		"line_items[0][quantity]":                       {"1"},
		"line_items[0][price_data][currency]":           {order.Currency},
		"line_items[0][price_data][unit_amount]":        {strconv.FormatInt(order.Amount, 10)},
		"line_items[0][price_data][product_data][name]": {order.PackageName},
		"adaptive_pricing[enabled]":                     {"false"},
	}
	var response stripeCheckout
	err := gatewayRequest(ctx, http.MethodPost, "https://api.stripe.com/v1/checkout/sessions",
		strings.NewReader(form.Encode()), map[string]string{"Authorization": "Bearer " + cfg.stripeKey,
			"Content-Type": "application/x-www-form-urlencoded", "Idempotency-Key": order.ID}, &response)
	return response.ID, response.URL, err
}

type paymentNumber string

func (n *paymentNumber) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*n = ""
		return nil
	}
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
	} else {
		value = string(raw)
	}
	*n = paymentNumber(value)
	return nil
}

type nowPayment struct {
	ID          paymentNumber `json:"payment_id"`
	Invoice     paymentNumber `json:"invoice_id"`
	Reference   string        `json:"order_id"`
	Amount      paymentNumber `json:"price_amount"`
	Currency    string        `json:"price_currency"`
	PayCurrency string        `json:"pay_currency"`
	Due         paymentNumber `json:"pay_amount"`
	Paid        paymentNumber `json:"actually_paid"`
	Status      string        `json:"payment_status"`
}

func createNowInvoice(ctx context.Context, cfg paymentConfig, order model.PaymentOrder) (string, string, error) {
	data, err := json.Marshal(map[string]any{
		"price_amount":   json.Number(fmt.Sprintf("%d.%02d", order.Amount/100, order.Amount%100)),
		"price_currency": order.Currency, "pay_currency": order.PayCurrency, "order_id": order.ID,
		"order_description": order.PackageName, "is_fixed_rate": true,
		"ipn_callback_url": cfg.site + "/api/payments/webhook/nowpayments",
		"success_url":      cfg.site + "/account?tab=redeem&payment=return",
		"cancel_url":       cfg.site + "/account?tab=redeem",
	})
	if err != nil {
		return "", "", err
	}
	var response struct {
		ID  paymentNumber `json:"id"`
		URL string        `json:"invoice_url"`
	}
	err = gatewayRequest(ctx, http.MethodPost, "https://api.nowpayments.io/v1/invoice", bytes.NewReader(data),
		map[string]string{"x-api-key": cfg.nowKey, "Content-Type": "application/json"}, &response)
	return string(response.ID), response.URL, err
}

func gatewayRequest(ctx context.Context, method, endpoint string, body io.Reader, headers map[string]string, result any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := paymentHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gateway returned HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxPaymentResponse+1))
	if err != nil {
		return err
	}
	if len(payload) > maxPaymentResponse {
		return errors.New("gateway response exceeds limit")
	}
	return json.Unmarshal(payload, result)
}

func verifyStripeWebhook(raw []byte, header, secret string, now time.Time) bool {
	var timestamp string
	var signatures []string
	for _, field := range strings.Split(header, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(field), "=")
		switch key {
		case "t":
			timestamp = value
		case "v1":
			signatures = append(signatures, value)
		}
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || unix < now.Unix()-300 || unix > now.Unix()+300 || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(raw)
	for _, signature := range signatures {
		decoded, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(mac.Sum(nil), decoded) {
			return true
		}
	}
	return false
}

func verifyNowWebhook(raw []byte, header, secret string) bool {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if secret == "" || decoder.Decode(&payload) != nil || payload == nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(payload) != nil {
		return false
	}
	signature, err := hex.DecodeString(header)
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))
	return err == nil && hmac.Equal(mac.Sum(nil), signature)
}

func paymentAmount(n paymentNumber) *big.Rat {
	if !decimalPaymentAmount.MatchString(string(n)) {
		return nil
	}
	value, _ := new(big.Rat).SetString(string(n))
	return value
}

func orderNowCurrency(order model.PaymentOrder) string {
	if order.PayCurrency == "" {
		// Orders created before the pay_currency column used USDT on TRON.
		return legacyNowCurrency
	}
	return order.PayCurrency
}

func nowPayMatches(payment nowPayment, order model.PaymentOrder) bool {
	total, due, paid := paymentAmount(payment.Amount), paymentAmount(payment.Due), paymentAmount(payment.Paid)
	return order.ProviderID != nil && string(payment.Invoice) == *order.ProviderID &&
		payment.Reference == order.ID && payment.Status == "finished" &&
		strings.EqualFold(payment.Currency, order.Currency) && strings.EqualFold(payment.PayCurrency, orderNowCurrency(order)) &&
		total != nil && total.Cmp(big.NewRat(order.Amount, 100)) == 0 &&
		due != nil && due.Sign() > 0 && paid != nil && paid.Cmp(due) >= 0
}

func StripePaymentWebhook(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxPaymentResponse))
	if err != nil || !verifyStripeWebhook(raw, c.GetHeader("Stripe-Signature"), os.Getenv("STRIPE_WEBHOOK_SECRET"), time.Now()) {
		c.Status(http.StatusBadRequest)
		return
	}
	var event struct {
		Type string `json:"type"`
		Data struct {
			Object stripeCheckout `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &event) != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	session := event.Data.Object
	if event.Type == "checkout.session.expired" {
		updatePaymentState(c, stripeProvider, session.Reference, session.ID, "closed")
		return
	}
	if event.Type != "checkout.session.completed" && event.Type != "checkout.session.async_payment_succeeded" {
		c.Status(http.StatusOK)
		return
	}
	if session.Status != "paid" || session.Mode != "payment" {
		c.Status(http.StatusOK)
		return
	}
	processPaidOrder(c, stripeProvider, session.Reference, func(order model.PaymentOrder) bool {
		return order.ProviderID != nil && session.ID == *order.ProviderID && session.Amount == order.Amount &&
			strings.EqualFold(session.Currency, order.Currency)
	})
}

func NowPaymentWebhook(c *gin.Context) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxPaymentResponse))
	if err != nil || !verifyNowWebhook(raw, c.GetHeader("x-nowpayments-sig"), os.Getenv("NOWPAYMENTS_IPN_SECRET")) {
		c.Status(http.StatusBadRequest)
		return
	}
	var event nowPayment
	if json.Unmarshal(raw, &event) != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	switch event.Status {
	case "expired", "failed", "refunded":
		updatePaymentState(c, nowProvider, event.Reference, string(event.Invoice), "closed")
		return
	case "partially_paid":
		updatePaymentState(c, nowProvider, event.Reference, string(event.Invoice), "review")
		return
	case "finished":
	default:
		c.Status(http.StatusOK)
		return
	}
	if !paymentIdentifier.MatchString(string(event.ID)) {
		c.Status(http.StatusBadRequest)
		return
	}
	var confirmed nowPayment
	ctx := c.Request.Context()
	err = gatewayRequest(ctx, http.MethodGet, "https://api.nowpayments.io/v1/payment/"+string(event.ID),
		nil, map[string]string{"x-api-key": os.Getenv("NOWPAYMENTS_API_KEY")}, &confirmed)
	if err != nil {
		logger.Error(ctx, "NOWPayments confirmation failed: "+err.Error())
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if confirmed.ID != event.ID {
		c.Status(http.StatusBadRequest)
		return
	}
	processPaidOrder(c, nowProvider, confirmed.Reference, func(order model.PaymentOrder) bool {
		return nowPayMatches(confirmed, order)
	})
}

func processPaidOrder(c *gin.Context, provider, reference string, matches func(model.PaymentOrder) bool) {
	if _, err := uuid.Parse(reference); err != nil {
		c.Status(http.StatusOK)
		return
	}
	ctx := c.Request.Context()
	var order model.PaymentOrder
	query := model.DB.WithContext(ctx).Where("id = ? AND provider = ?", reference, provider).Limit(1).Find(&order)
	if query.Error != nil || (query.RowsAffected != 0 && order.ProviderID == nil) {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if query.RowsAffected == 0 || order.CreditedAt != 0 {
		c.Status(http.StatusOK)
		return
	}
	if !matches(order) {
		logger.Error(ctx, "payment does not match order "+order.ID)
		updatePaymentState(c, provider, reference, *order.ProviderID, "review")
		return
	}
	if err := model.CreditPaymentOrder(ctx, order, *order.ProviderID); err != nil {
		logger.Error(ctx, "failed to credit payment order "+order.ID+": "+err.Error())
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Status(http.StatusOK)
}

func updatePaymentState(c *gin.Context, provider, reference, providerID, state string) {
	if _, err := uuid.Parse(reference); err != nil || !paymentIdentifier.MatchString(providerID) {
		c.Status(http.StatusOK)
		return
	}
	query := model.DB.WithContext(c.Request.Context()).Model(&model.PaymentOrder{}).
		Where("id = ? AND provider = ? AND provider_id = ? AND credited_at = 0", reference, provider, providerID)
	if state == "review" {
		query = query.Where("state = ?", "pending")
	}
	if err := query.Update("state", state).Error; err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Status(http.StatusOK)
}

func paymentError(c *gin.Context, err error) {
	logger.Error(c.Request.Context(), "checkout: "+err.Error())
	c.JSON(http.StatusOK, gin.H{"success": false, "message": "Checkout is unavailable. Please try again."})
}
