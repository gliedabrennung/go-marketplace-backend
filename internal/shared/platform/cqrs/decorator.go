package cqrs

import (
	"context"
	"reflect"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Handler[C any, R any] interface {
	Handle(ctx context.Context, cmd C) (R, error)
}

type HandlerFunc[C any, R any] func(ctx context.Context, cmd C) (R, error)

func (f HandlerFunc[C, R]) Handle(ctx context.Context, cmd C) (R, error) {
	return f(ctx, cmd)
}

type Logger interface {
	DebugContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

type Metrics interface {
	ObserveCommand(name string, d time.Duration, ok bool)
}

func Decorate[C any, R any](module string, h Handler[C, R], log Logger, m Metrics) Handler[C, R] {
	name := module + "." + typeName[C]()
	return tracingDecorator[C, R]{
		name: name,
		next: loggingDecorator[C, R]{
			name: name,
			log:  log,
			next: metricsDecorator[C, R]{name: name, m: m, next: h},
		},
	}
}

type tracingDecorator[C any, R any] struct {
	name string
	next Handler[C, R]
}

func (d tracingDecorator[C, R]) Handle(ctx context.Context, cmd C) (R, error) {
	ctx, span := otel.Tracer("github.com/gliedabrennung/go-marketplace-backend/cqrs").Start(ctx, d.name)
	defer span.End()
	res, err := d.next.Handle(ctx, cmd)
	if err != nil && kernel.KindOf(err) == kernel.KindInternal {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return res, err
}

type loggingDecorator[C any, R any] struct {
	name string
	log  Logger
	next Handler[C, R]
}

func (d loggingDecorator[C, R]) Handle(ctx context.Context, cmd C) (R, error) {
	d.log.DebugContext(ctx, "handler started", "handler", d.name)
	res, err := d.next.Handle(ctx, cmd)
	switch {
	case err == nil:
		d.log.DebugContext(ctx, "handler succeeded", "handler", d.name)
	case kernel.KindOf(err) == kernel.KindInternal:
		d.log.ErrorContext(ctx, "handler failed", "handler", d.name, "err", err)
	default:
		d.log.WarnContext(ctx, "handler rejected", "handler", d.name, "code", kernel.CodeOf(err), "err", err)
	}
	return res, err
}

type metricsDecorator[C any, R any] struct {
	name string
	m    Metrics
	next Handler[C, R]
}

func (d metricsDecorator[C, R]) Handle(ctx context.Context, cmd C) (R, error) {
	start := time.Now()
	res, err := d.next.Handle(ctx, cmd)
	d.m.ObserveCommand(d.name, time.Since(start), err == nil || kernel.KindOf(err) != kernel.KindInternal)
	return res, err
}

func typeName[T any]() string {
	t := reflect.TypeFor[T]()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}
