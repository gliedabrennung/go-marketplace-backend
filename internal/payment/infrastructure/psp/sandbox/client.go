package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/breaker"
)

type ClientConfig struct {
	BaseURL   string
	APIKey    string
	Secret    string
	Timeout   time.Duration
	Tolerance time.Duration
	HTTP      *http.Client
	Breaker   *breaker.Breaker
	Observe   func(operation string, d time.Duration, err error)
}

type Client struct {
	base     string
	apiKey   string
	timeout  time.Duration
	http     *http.Client
	breaker  *breaker.Breaker
	observe  func(operation string, d time.Duration, err error)
	verifier Verifier
}

func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout <= 0 || cfg.Timeout > 5*time.Second {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{}
	}
	if cfg.Breaker == nil {
		cfg.Breaker = breaker.New(breaker.Settings{Ignore: Rejected})
	}
	if cfg.Observe == nil {
		cfg.Observe = func(string, time.Duration, error) {}
	}
	return &Client{
		base: strings.TrimRight(cfg.BaseURL, "/"), apiKey: cfg.APIKey, timeout: cfg.Timeout, http: cfg.HTTP,
		breaker: cfg.Breaker, observe: cfg.Observe, verifier: NewVerifier(cfg.Secret, cfg.Tolerance),
	}
}

func Rejected(err error) bool {
	return errors.Is(err, application.ErrProviderRejected)
}

func (c *Client) Name() string { return Name }

func (c *Client) CreateIntent(ctx context.Context, request application.IntentRequest) (application.Intent, error) {
	var out intentResponse
	err := c.call(ctx, "create_intent", http.MethodPost, "/v1/intents", "intent:"+request.PaymentID, intentRequest{
		Reference: request.PaymentID, Amount: request.Amount.Amount(), Currency: string(request.Amount.Currency()),
		ReturnURL: request.ReturnURL, Description: request.Description, SaveMethod: request.SaveMethod,
		MethodToken: request.MethodToken,
	}, &out)
	if err != nil {
		return application.Intent{}, err
	}
	return application.Intent{ProviderPaymentID: out.ID, RedirectURL: out.RedirectURL}, nil
}

func (c *Client) Capture(ctx context.Context, providerPaymentID string, amount kernel.Money, key string) error {
	return c.call(ctx, "capture", http.MethodPost, "/v1/intents/"+url.PathEscape(providerPaymentID)+"/capture", key,
		amountRequest{Amount: amount.Amount()}, nil)
}

func (c *Client) Cancel(ctx context.Context, providerPaymentID, key string) error {
	return c.call(ctx, "cancel", http.MethodPost, "/v1/intents/"+url.PathEscape(providerPaymentID)+"/cancel", key, struct{}{}, nil)
}

func (c *Client) Refund(ctx context.Context, providerPaymentID string, amount kernel.Money, key string) (string, error) {
	var out refundResponse
	err := c.call(ctx, "refund", http.MethodPost, "/v1/intents/"+url.PathEscape(providerPaymentID)+"/refunds", key,
		amountRequest{Amount: amount.Amount(), Reference: strings.TrimPrefix(key, "refund:")}, &out)
	return out.ID, err
}

func (c *Client) Transactions(ctx context.Context, day time.Time) ([]application.ProviderTransaction, error) {
	var out transactionsResponse
	if err := c.call(ctx, "transactions", http.MethodGet, "/v1/transactions?date="+day.UTC().Format(time.DateOnly), "", nil, &out); err != nil {
		return nil, err
	}
	txs := make([]application.ProviderTransaction, 0, len(out.Data))
	for _, it := range out.Data {
		txs = append(txs, application.ProviderTransaction{
			ProviderPaymentID: it.ID, Status: it.Status, Captured: it.Captured, Refunded: it.Refunded, Currency: it.Currency,
		})
	}
	return txs, nil
}

func (c *Client) Verify(headers map[string]string, body []byte, now time.Time) (application.WebhookEvent, error) {
	return c.verifier.Verify(headers, body, now)
}

func (c *Client) call(ctx context.Context, operation, method, path, key string, in, out any) error {
	started := time.Now()
	err := c.breaker.Do(ctx, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()
		var body io.Reader
		if in != nil {
			payload, err := json.Marshal(in)
			if err != nil {
				return err
			}
			body = bytes.NewReader(payload)
		}
		request, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
		if err != nil {
			return err
		}
		request.Header.Set(AccessHeader, c.apiKey)
		request.Header.Set("Content-Type", "application/json")
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		response, err := c.http.Do(request)
		if err != nil {
			return fmt.Errorf("sandbox psp %s %s: %w", method, path, err)
		}
		defer response.Body.Close()
		return decode(response, out)
	})
	c.observe(operation, time.Since(started), err)
	return err
}

func decode(response *http.Response, out any) error {
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("sandbox psp read: %w", err)
	}
	if response.StatusCode >= http.StatusInternalServerError || response.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("sandbox psp answered %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	if response.StatusCode >= http.StatusBadRequest {
		var failed errorBody
		_ = json.Unmarshal(payload, &failed)
		return application.ErrProviderRejected.WithDetail("%s: %s", failed.Error.Code, failed.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("sandbox psp decode: %w", err)
	}
	return nil
}
