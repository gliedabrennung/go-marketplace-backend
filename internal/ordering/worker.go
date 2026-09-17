package ordering

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/command"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/scheduler"
)

const (
	authorizedConsumer = "ordering.checkout_payment_authorized"
	expireJob          = "ordering.expire_checkouts"
	compensationJob    = "ordering.run_compensations"
	stalledJob         = "ordering.resume_stalled_checkouts"
	completionJob      = "ordering.complete_delivered_orders"
)

type Worker struct {
	d          Dependencies
	authorized cqrs.Handler[command.PaymentAuthorized, struct{}]
	expire     cqrs.Handler[command.ExpireCheckouts, int]
	compensate cqrs.Handler[command.RunCompensations, int]
	stalled    cqrs.Handler[command.ResumeStalled, int]
	complete   cqrs.Handler[command.CompleteDeliveredOrders, int]
}

func NewWorker(d Dependencies) *Worker {
	base := d.base()
	compensator := command.NewCompensator(base)
	return &Worker{
		d:          d,
		authorized: decorate(d, command.NewPaymentAuthorizedHandler(base, compensator)),
		expire:     decorate(d, command.NewExpireCheckoutsHandler(base, compensator)),
		compensate: decorate(d, command.NewRunCompensationsHandler(base, compensator)),
		stalled:    decorate(d, command.NewResumeStalledHandler(base, compensator)),
		complete:   decorate(d, command.NewCompleteDeliveredOrdersHandler(base)),
	}
}

func (w *Worker) Subscriptions() []outbox.Subscription {
	return []outbox.Subscription{{
		Consumer:   authorizedConsumer,
		EventNames: []string{paymentapi.EventAuthorized},
		Handle:     w.paymentAuthorized,
	}}
}

func (w *Worker) paymentAuthorized(ctx context.Context, msg outbox.Message) error {
	var event paymentapi.PaymentV1
	if err := json.Unmarshal(msg.Payload, &event); err != nil || event.PaymentID == "" || event.OrderID == "" {
		w.d.Logger.ErrorContext(ctx, "skipping malformed event", "event_name", msg.EventName, "outbox_id", msg.ID, "err", err)
		return nil
	}
	_, err := w.authorized.Handle(ctx, command.PaymentAuthorized{OrderID: event.OrderID, PaymentID: event.PaymentID})
	return err
}

func (w *Worker) Jobs() []scheduler.Job {
	return []scheduler.Job{
		w.job(expireJob, 15*time.Second, func(ctx context.Context) error {
			_, err := w.expire.Handle(ctx, command.ExpireCheckouts{})
			return err
		}),
		w.job(compensationJob, 15*time.Second, func(ctx context.Context) error {
			_, err := w.compensate.Handle(ctx, command.RunCompensations{})
			return err
		}),
		w.job(stalledJob, time.Minute, func(ctx context.Context) error {
			_, err := w.stalled.Handle(ctx, command.ResumeStalled{})
			return err
		}),
		w.job(completionJob, time.Hour, func(ctx context.Context) error {
			_, err := w.complete.Handle(ctx, command.CompleteDeliveredOrders{})
			return err
		}),
	}
}

func (w *Worker) job(name string, interval time.Duration, run func(ctx context.Context) error) scheduler.Job {
	return scheduler.Job{Name: name, Interval: interval, Run: scheduler.Exclusive(w.d.Pool, name, run)}
}
