package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type ServerConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	DrainDelay        time.Duration
}

type Server struct {
	cfg     ServerConfig
	handler http.Handler
	log     *slog.Logger
	onDrain func()
}

func NewServer(cfg ServerConfig, handler http.Handler, log *slog.Logger, onDrain func()) *Server {
	if onDrain == nil {
		onDrain = func() {}
	}
	return &Server{cfg: cfg, handler: handler, log: log, onDrain: onDrain}
}

func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.handler,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		ReadTimeout:       s.cfg.ReadTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.InfoContext(ctx, "http server listening", "addr", s.cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("listen %s: %w", s.cfg.Addr, err)
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	s.log.InfoContext(ctx, "http server draining", "addr", s.cfg.Addr, "delay", s.cfg.DrainDelay)
	s.onDrain()
	if s.cfg.DrainDelay > 0 {
		timer := time.NewTimer(s.cfg.DrainDelay)
		<-timer.C
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown %s: %w", s.cfg.Addr, err)
	}
	s.log.InfoContext(ctx, "http server stopped", "addr", s.cfg.Addr)
	return <-errCh
}
