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
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/infinmalum/a1o-platform/model"
)

const (
	airwallexProvider = "airwallex"
	airwallexLinks    = "/api/v1/pa/payment_links"
)

type airwallexConfig struct {
	baseURL, clientID, apiKey, webhookSecret string
}

func loadAirwallexConfig() airwallexConfig {
	cfg := airwallexConfig{
		clientID: os.Getenv("AIRWALLEX_CLIENT_ID"), apiKey: os.Getenv("AIRWALLEX_API_KEY"),
		webhookSecret: os.Getenv("AIRWALLEX_WEBHOOK_SECRET"),
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AIRWALLEX_ENV"))) {
	case "", "production":
		cfg.baseURL = "https://api.airwallex.com"
	case "sandbox":
		cfg.baseURL = "https://api.sandbox.airwallex.com"
	}
	return cfg
}

func (cfg airwallexConfig) enabled() bool {
	return cfg.baseURL != "" && cfg.clientID != "" && cfg.apiKey != "" && cfg.webhookSecret != ""
}

func (cfg airwallexConfig) token(ctx context.Context) (string, error) {
	var response struct {
		Token string `json:"token"`
	}
	err := gatewayRequest(ctx, http.MethodPost, cfg.baseURL+"/api/v1/authentication/login", nil,
		map[string]string{"x-client-id": cfg.clientID, "x-api-key": cfg.apiKey, "Content-Type": "application/json"}, &response)
	if err == nil && response.Token == "" {
		err = errors.New("Airwallex access token missing")
	}
	return response.Token, err
}

type airwallexLink struct {
	ID        string        `json:"id"`
	URL       string        `json:"url"`
	Reference string        `json:"reference"`
	Status    string        `json:"status"`
	Currency  string        `json:"currency"`
	Amount    paymentNumber `json:"amount"`
	Reusable  bool          `json:"reusable"`
	IntentID  string        `json:"latest_successful_payment_intent_id"`
}

func (link airwallexLink) matches(order model.PaymentOrder) bool {
	total := paymentAmount(link.Amount)
	return link.Reference == order.ID && !link.Reusable && strings.EqualFold(link.Currency, order.Currency) &&
		total != nil && total.Cmp(big.NewRat(order.Amount, 100)) == 0
}

func createAirwallexLink(ctx context.Context, cfg airwallexConfig, order model.PaymentOrder) (string, string, error) {
	token, err := cfg.token(ctx)
	if err != nil {
		return "", "", err
	}
	body, err := json.Marshal(map[string]any{
		"title": order.PackageName, "reference": order.ID, "reusable": false,
		"currency": strings.ToUpper(order.Currency),
		"amount":   json.Number(fmt.Sprintf("%d.%02d", order.Amount/100, order.Amount%100)),
	})
	if err != nil {
		return "", "", err
	}
	var link airwallexLink
	err = gatewayRequest(ctx, http.MethodPost, cfg.baseURL+airwallexLinks+"/create", bytes.NewReader(body),
		map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}, &link)
	if err == nil && !link.matches(order) {
		err = errors.New("Airwallex payment link does not match order")
	}
	return link.ID, link.URL, err
}

func verifyAirwallexWebhook(raw []byte, timestamp, signature, secret string, now time.Time) bool {
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || unix < now.Add(-5*time.Minute).UnixMilli() || unix > now.Add(5*time.Minute).UnixMilli() || secret == "" {
		return false
	}
	digest, err := hex.DecodeString(signature)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write(raw)
	return err == nil && hmac.Equal(mac.Sum(nil), digest)
}

func AirwallexPaymentWebhook(c *gin.Context) {
	cfg := loadAirwallexConfig()
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxPaymentResponse))
	if err != nil || !verifyAirwallexWebhook(raw, c.GetHeader("x-timestamp"), c.GetHeader("x-signature"), cfg.webhookSecret, time.Now()) {
		c.Status(http.StatusBadRequest)
		return
	}
	var event struct {
		Name string `json:"name"`
		Data struct {
			Object airwallexLink `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &event) != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if event.Name != "payment_link.paid" {
		c.Status(http.StatusOK)
		return
	}
	link := event.Data.Object
	processConfirmedPayment(c, airwallexProvider, link.Reference, func(order model.PaymentOrder) (bool, error) {
		if link.ID != *order.ProviderID {
			return false, nil
		}
		if !cfg.enabled() {
			return false, errors.New("Airwallex credentials unavailable")
		}
		return confirmAirwallexPayment(c.Request.Context(), cfg, order)
	})
}

func confirmAirwallexPayment(ctx context.Context, cfg airwallexConfig, order model.PaymentOrder) (bool, error) {
	token, err := cfg.token(ctx)
	if err != nil {
		return false, err
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	var link airwallexLink
	if err := gatewayRequest(ctx, http.MethodGet, cfg.baseURL+airwallexLinks+"/"+*order.ProviderID, nil, headers, &link); err != nil {
		return false, err
	}
	if link.ID != *order.ProviderID || !link.matches(order) {
		return false, nil
	}
	if link.Status != "PAID" || !paymentIdentifier.MatchString(link.IntentID) {
		return false, errors.New("Airwallex has not confirmed payment on the link")
	}
	var intent struct {
		ID       string        `json:"id"`
		Status   string        `json:"status"`
		Currency string        `json:"currency"`
		Captured paymentNumber `json:"captured_amount"`
	}
	if err := gatewayRequest(ctx, http.MethodGet, cfg.baseURL+"/api/v1/pa/payment_intents/"+link.IntentID, nil, headers, &intent); err != nil {
		return false, err
	}
	if intent.Status != "SUCCEEDED" {
		return false, errors.New("Airwallex payment intent has not succeeded")
	}
	captured := paymentAmount(intent.Captured)
	return intent.ID == link.IntentID && strings.EqualFold(intent.Currency, order.Currency) &&
		captured != nil && captured.Cmp(big.NewRat(order.Amount, 100)) == 0, nil
}
