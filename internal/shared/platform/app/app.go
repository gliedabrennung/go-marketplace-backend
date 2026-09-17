package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/config"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
)

type Base struct {
	Log      *slog.Logger
	Registry *prometheus.Registry
	Health   *httpx.Health

	closers []func(ctx context.Context) error
}

func NewBase(ctx context.Context, svc config.Service, tr config.Tracing) (*Base, error) {
	log := observability.NewLogger(observability.LogConfig{
		Level:   svc.LogLevel,
		Service: svc.Name,
		Version: svc.Version,
		Env:     svc.Env,
	}, os.Stdout)
	slog.SetDefault(log)

	shutdownTracing, err := observability.SetupTracing(ctx, observability.TracingConfig{
		Endpoint:       tr.Endpoint,
		Insecure:       tr.Insecure,
		SampleRatio:    tr.SampleRatio,
		AlwaysSampleOn: tr.AlwaysSampleOn,
		Service:        svc.Name,
		Version:        svc.Version,
		Env:            svc.Env,
	})
	if err != nil {
		return nil, err
	}

	b := &Base{
		Log:      log,
		Registry: observability.NewRegistry(),
		Health:   httpx.NewHealth(2e9),
	}
	b.OnClose(shutdownTracing)
	return b, nil
}

func (b *Base) OnClose(fn func(ctx context.Context) error) {
	b.closers = append(b.closers, fn)
}

func (b *Base) Close(ctx context.Context) {
	for _, closeFn := range slices.Backward(b.closers) {
		if err := closeFn(ctx); err != nil {
			b.Log.WarnContext(ctx, "close resource failed", "err", err)
		}
	}
}

func OpsHandler(health *httpx.Health, reg *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Liveness)
	mux.HandleFunc("GET /readyz", health.Readiness)
	mux.Handle("GET /metrics", observability.MetricsHandler(reg))
	return mux
}

func RunAll(ctx context.Context, runners ...func(ctx context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, run := range runners {
		wg.Go(func() {
			if err := run(ctx); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				cancel()
			}
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("run: %w", errors.Join(errs...))
	}
	return nil
}
