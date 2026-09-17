package sandbox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
)

const (
	Name            = "sandbox"
	SignatureHeader = "X-Sandbox-Signature"
	AccessHeader    = "X-Sandbox-Api-Key"

	statusRequiresConfirmation = "requires_confirmation"
	statusAuthorized           = "authorized"
	statusCaptured             = "captured"
	statusCancelled            = "cancelled"
	statusFailed               = "failed"
	statusRefunded             = "refunded"
)

type intentRequest struct {
	Reference   string `json:"reference"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	ReturnURL   string `json:"return_url"`
	Description string `json:"description"`
	SaveMethod  bool   `json:"save_method"`
	MethodToken string `json:"method_token,omitempty"`
}

type intentResponse struct {
	ID          string `json:"id"`
	Reference   string `json:"reference"`
	Status      string `json:"status"`
	Amount      int64  `json:"amount"`
	Captured    int64  `json:"captured"`
	Refunded    int64  `json:"refunded"`
	Currency    string `json:"currency"`
	RedirectURL string `json:"redirect_url"`
}

type amountRequest struct {
	Amount    int64  `json:"amount"`
	Reference string `json:"reference,omitempty"`
}

type refundResponse struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
}

type transactionsResponse struct {
	Data []intentResponse `json:"data"`
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type webhookData struct {
	IntentID        string `json:"intent_id"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	Reason          string `json:"reason,omitempty"`
	RefundID        string `json:"refund_id,omitempty"`
	RefundReference string `json:"refund_reference,omitempty"`
	MethodToken     string `json:"method_token,omitempty"`
	MethodLabel     string `json:"method_label,omitempty"`
}

type webhook struct {
	ID      string      `json:"id"`
	Type    string      `json:"type"`
	Created int64       `json:"created"`
	Data    webhookData `json:"data"`
}

func Sign(secret string, body []byte, at time.Time) string {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	return "t=" + timestamp + ",v1=" + digest(secret, timestamp, body)
}

func digest(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

type Verifier struct {
	secret    string
	tolerance time.Duration
}

func NewVerifier(secret string, tolerance time.Duration) Verifier {
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}
	return Verifier{secret: secret, tolerance: tolerance}
}

func (v Verifier) Verify(headers map[string]string, body []byte, now time.Time) (application.WebhookEvent, error) {
	timestamp, signatures := parseSignature(headers[strings.ToLower(SignatureHeader)])
	if timestamp == "" || len(signatures) == 0 || v.secret == "" {
		return application.WebhookEvent{}, application.ErrInvalidSignature
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return application.WebhookEvent{}, application.ErrInvalidSignature
	}
	if math.Abs(now.Sub(time.Unix(unix, 0)).Seconds()) > v.tolerance.Seconds() {
		return application.WebhookEvent{}, application.ErrInvalidSignature.WithDetail("timestamp outside tolerance")
	}
	expected := digest(v.secret, timestamp, body)
	valid := false
	for _, candidate := range signatures {
		if hmac.Equal([]byte(candidate), []byte(expected)) {
			valid = true
		}
	}
	if !valid {
		return application.WebhookEvent{}, application.ErrInvalidSignature
	}
	var payload webhook
	if err := json.Unmarshal(body, &payload); err != nil || payload.ID == "" || payload.Type == "" {
		return application.WebhookEvent{}, fmt.Errorf("decode sandbox webhook: %w", application.ErrInvalidWebhook)
	}
	return application.WebhookEvent{
		ID: payload.ID, Type: payload.Type, ProviderPaymentID: payload.Data.IntentID, Amount: payload.Data.Amount,
		Currency: payload.Data.Currency, Reason: payload.Data.Reason, RefundReference: payload.Data.RefundReference,
		ProviderRefundID: payload.Data.RefundID, MethodToken: payload.Data.MethodToken, MethodLabel: payload.Data.MethodLabel,
	}, nil
}

func parseSignature(header string) (string, []string) {
	var (
		timestamp  string
		signatures []string
	)
	for part := range strings.SplitSeq(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestamp = value
		case "v1":
			signatures = append(signatures, value)
		}
	}
	return timestamp, signatures
}
