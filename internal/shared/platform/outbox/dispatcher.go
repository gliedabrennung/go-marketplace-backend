package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type Handler func(ctx context.Context, msg Message) error

type Subscription struct {
	Consumer   string
	EventNames []string
	Handle     Handler
}

type deduplicator interface {
	Once(ctx context.Context, consumerGroup, messageID string, fn func(ctx context.Context, tx pgx.Tx) error) (bool, error)
}

type Dispatcher struct {
	routes  map[string][]Subscription
	inbox   deduplicator
	log     *slog.Logger
	handled *prometheus.CounterVec
}

func NewDispatcher(inbox deduplicator, log *slog.Logger, reg prometheus.Registerer, subscriptions ...Subscription) *Dispatcher {
	d := &Dispatcher{
		routes: make(map[string][]Subscription),
		inbox:  inbox,
		log:    log,
		handled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_handler_total",
			Help: "Domain event deliveries to subscribers by outcome.",
		}, []string{"consumer", "event_name", "outcome"}),
	}
	reg.MustRegister(d.handled)
	for _, s := range subscriptions {
		for _, name := range s.EventNames {
			d.routes[name] = append(d.routes[name], s)
		}
	}
	return d
}

func (d *Dispatcher) Publish(ctx context.Context, msgs []Message) []error {
	errs := make([]error, len(msgs))
	blocked := make(map[string]error)
	for i, m := range msgs {
		if cause, ok := blocked[m.AggregateID]; ok {
			errs[i] = fmt.Errorf("blocked by earlier event of aggregate %s: %w", m.AggregateID, cause)
			continue
		}
		if err := d.dispatch(ctx, m); err != nil {
			errs[i] = err
			blocked[m.AggregateID] = err
		}
	}
	return errs
}

func (d *Dispatcher) dispatch(ctx context.Context, m Message) error {
	msgCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(m.Metadata))
	messageID := strconv.FormatInt(m.ID, 10)
	var failures []error
	for _, sub := range d.routes[m.EventName] {
		_, err := d.inbox.Once(msgCtx, sub.Consumer, messageID, func(ctx context.Context, _ pgx.Tx) error {
			return sub.Handle(ctx, m)
		})
		outcome := "success"
		if err != nil {
			outcome = "failure"
			failures = append(failures, fmt.Errorf("%s: %w", sub.Consumer, err))
			d.log.ErrorContext(msgCtx, "event handler failed",
				"consumer", sub.Consumer, "event_name", m.EventName, "outbox_id", m.ID, "aggregate_id", m.AggregateID, "err", err)
		}
		d.handled.WithLabelValues(sub.Consumer, m.EventName, outcome).Inc()
	}
	return errors.Join(failures...)
}
