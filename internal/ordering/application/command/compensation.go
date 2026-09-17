package command

import (
	"context"
	"errors"
	"fmt"

	inventoryapi "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
)

type Compensator struct {
	base Base
}

func NewCompensator(base Base) *Compensator {
	return &Compensator{base: base}
}

func (c *Compensator) Abort(ctx context.Context, id domain.OrderID, reason string) error {
	_, err := c.base.mutateSaga(ctx, id, func(saga *domain.CheckoutSaga) error {
		return saga.BeginCompensation(reason, domain.NewReference(), c.base.Clock.Now())
	})
	if err != nil {
		return err
	}
	return c.Run(ctx, id)
}

func (c *Compensator) Run(ctx context.Context, id domain.OrderID) error {
	for {
		saga, err := c.base.saga(ctx, id)
		if err != nil {
			return err
		}
		pending := saga.Pending()
		if len(pending) == 0 {
			return nil
		}
		step := pending[0]
		stepErr := c.execute(ctx, saga, step)
		saga, err = c.base.mutateSaga(ctx, id, func(saga *domain.CheckoutSaga) error {
			if stepErr != nil {
				saga.CompensationFailed(stepErr, c.base.Policy.CompensationAttempts, c.base.Clock.Now())
				return nil
			}
			saga.Compensated(step, c.base.Clock.Now())
			return nil
		})
		if err != nil {
			return errors.Join(stepErr, err)
		}
		if stepErr != nil {
			c.base.Metrics.Compensation(string(step), "error")
			if saga.Status() == domain.SagaManual {
				c.base.Logger.ErrorContext(ctx, "checkout saga requires manual intervention",
					"order_id", id.String(), "step", string(step), "err", stepErr)
			}
			return fmt.Errorf("compensate %s: %w", step, stepErr)
		}
		c.base.Metrics.Compensation(string(step), "success")
	}
}

func (c *Compensator) execute(ctx context.Context, saga *domain.CheckoutSaga, step domain.Step) error {
	switch step {
	case domain.CompensationCancel, domain.CompensationRefund:
		return c.settlePayment(ctx, saga)
	case domain.CompensationStock:
		return c.returnStock(ctx, saga.ReservationID())
	case domain.CompensationPromo:
		return c.base.Pricing.Release(ctx, saga.PromoCode(), saga.OrderID().String())
	case domain.CompensationCloseOrder:
		return c.closeOrder(ctx, saga)
	}
	return fmt.Errorf("unknown compensation step %q", step)
}

func (c *Compensator) settlePayment(ctx context.Context, saga *domain.CheckoutSaga) error {
	info, err := c.base.Payments.Info(ctx, saga.PaymentID())
	if errors.Is(err, paymentapi.ErrPaymentNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	switch info.Status {
	case paymentapi.StatusCreated, paymentapi.StatusPending, paymentapi.StatusAuthorized:
		err = c.base.Payments.Cancel(ctx, info.PaymentID, saga.Reason())
		if !paymentapi.IsInvalidTransition(err) {
			return err
		}
		return c.base.Payments.Refund(ctx, info.PaymentID, saga.RefundID(), 0, saga.Reason())
	case paymentapi.StatusCaptured:
		return c.base.Payments.Refund(ctx, info.PaymentID, saga.RefundID(), 0, saga.Reason())
	}
	return nil
}

func (c *Compensator) returnStock(ctx context.Context, reservationID string) error {
	err := c.base.Inventory.Release(ctx, reservationID)
	switch {
	case err == nil, errors.Is(err, inventoryapi.ErrReservationNotFound):
		return nil
	case !errors.Is(err, inventoryapi.ErrReservationResolved):
		return err
	}
	err = c.base.Inventory.Restore(ctx, reservationID)
	if errors.Is(err, inventoryapi.ErrReservationNotFound) || errors.Is(err, inventoryapi.ErrReservationNotCommitted) {
		return nil
	}
	return err
}

func (c *Compensator) closeOrder(ctx context.Context, saga *domain.CheckoutSaga) error {
	return c.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		order, err := repos.Orders().FindByID(ctx, saga.OrderID())
		if errors.Is(err, domain.ErrOrderNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		switch order.Status() {
		case domain.StatusCreated, domain.StatusAwaitingPayment:
			if err := order.Fail(saga.Reason(), c.base.Clock.Now()); err != nil {
				return err
			}
			return repos.Orders().Save(ctx, order)
		}
		return nil
	})
}

type ExpireCheckouts struct{}

type ExpireCheckoutsHandler struct {
	base        Base
	compensator *Compensator
}

func NewExpireCheckoutsHandler(base Base, compensator *Compensator) *ExpireCheckoutsHandler {
	return &ExpireCheckoutsHandler{base: base, compensator: compensator}
}

func (h *ExpireCheckoutsHandler) Handle(ctx context.Context, _ ExpireCheckouts) (int, error) {
	var ids []domain.OrderID
	err := h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		var err error
		ids, err = repos.Sagas().Expired(ctx, h.base.Clock.Now(), h.base.Policy.BatchSize)
		return err
	})
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, id := range ids {
		_, err := h.base.mutateSaga(ctx, id, func(saga *domain.CheckoutSaga) error {
			if !saga.Expired(h.base.Clock.Now()) {
				return errNotExpired
			}
			return saga.BeginCompensation("payment was not authorized in time", domain.NewReference(), h.base.Clock.Now())
		})
		if errors.Is(err, errNotExpired) {
			continue
		}
		if err == nil {
			h.base.Metrics.SagaStep("payment_timeout", "compensating")
			err = h.compensator.Run(ctx, id)
		}
		errs = append(errs, err)
	}
	return len(ids), errors.Join(errs...)
}

var errNotExpired = errors.New("saga is not expired")

type RunCompensations struct{}

type RunCompensationsHandler struct {
	base        Base
	compensator *Compensator
}

func NewRunCompensationsHandler(base Base, compensator *Compensator) *RunCompensationsHandler {
	return &RunCompensationsHandler{base: base, compensator: compensator}
}

func (h *RunCompensationsHandler) Handle(ctx context.Context, _ RunCompensations) (int, error) {
	var ids []domain.OrderID
	err := h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		var err error
		ids, err = repos.Sagas().Compensating(ctx, h.base.Clock.Now(), h.base.Policy.BatchSize)
		return err
	})
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, id := range ids {
		errs = append(errs, h.compensator.Run(ctx, id))
	}
	return len(ids), errors.Join(errs...)
}
