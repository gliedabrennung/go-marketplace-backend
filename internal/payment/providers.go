package payment

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/psp"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/psp/sandbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/breaker"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/config"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
)

func NewProviders(cfg config.Payments, metrics *observability.ExternalMetrics) application.Providers {
	name := sandbox.Name
	return psp.NewRegistry(sandbox.NewClient(sandbox.ClientConfig{
		BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Secret: cfg.WebhookSecret, Timeout: cfg.Timeout,
		Tolerance: cfg.WebhookTolerance,
		HTTP:      &http.Client{Transport: http.DefaultTransport},
		Breaker: breaker.New(breaker.Settings{
			Failures: cfg.BreakerFailures, Cooldown: cfg.BreakerCooldown, Ignore: sandbox.Rejected,
			OnChange: func(state breaker.State) { metrics.SetBreakerState(name, breakerGauge(state)) },
		}),
		Observe: func(operation string, d time.Duration, err error) { metrics.ObserveCall(name, operation, d, err) },
	}))
}

func NewSandboxServer(cfg config.Payments, log *slog.Logger) http.Handler {
	return http.StripPrefix("/sandbox/psp", sandbox.NewServer(sandbox.ServerConfig{
		APIKey: cfg.APIKey, Secret: cfg.WebhookSecret, PublicURL: cfg.SandboxPublicURL,
		WebhookURL: cfg.SandboxWebhookURL, Log: log,
	}))
}

func breakerGauge(state breaker.State) float64 {
	switch state {
	case breaker.StateHalfOpen:
		return 1
	case breaker.StateOpen:
		return 2
	default:
		return 0
	}
}
