package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/reqctx"
)

type LogConfig struct {
	Level   string
	Service string
	Version string
	Env     string
}

const masked = "***"

var secretKeys = map[string]struct{}{
	"password":          {},
	"new_password":      {},
	"old_password":      {},
	"secret":            {},
	"token":             {},
	"access_token":      {},
	"refresh_token":     {},
	"authorization":     {},
	"cookie":            {},
	"otp":               {},
	"verification_code": {},
	"card_number":       {},
	"pan":               {},
	"cvv":               {},
	"cvc":               {},
	"iban":              {},
	"account_number":    {},
}

var personalKeys = map[string]struct{}{
	"phone": {},
	"email": {},
}

func NewLogger(cfg LogConfig, w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(cfg.Level),
		ReplaceAttr: replaceAttr,
	})
	return slog.New(contextHandler{Handler: h}).With(
		slog.String("service", cfg.Service),
		slog.String("version", cfg.Version),
		slog.String("env", cfg.Env),
	)
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 {
		switch a.Key {
		case slog.TimeKey:
			a.Key = "timestamp"
			return a
		case slog.MessageKey:
			a.Key = "message"
			return a
		case slog.LevelKey:
			return a
		}
	}
	key := strings.ToLower(a.Key)
	if _, ok := secretKeys[key]; ok {
		return slog.String(a.Key, masked)
	}
	if _, ok := personalKeys[key]; ok {
		return slog.String(a.Key, maskTail(a.Value.String()))
	}
	return a
}

func maskTail(s string) string {
	const visible = 2
	if len(s) <= visible {
		return masked
	}
	return masked + s[len(s)-visible:]
}

type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	if id := reqctx.RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	if id := reqctx.UserID(ctx); id != "" {
		r.AddAttrs(slog.String("user_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}
