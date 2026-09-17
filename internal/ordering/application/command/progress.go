package command

import (
	"context"
	"errors"

	inventoryapi "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type PaymentAuthorized struct {
	OrderID   string
	PaymentID string
}

type PaymentAuthorizedHandler struct {
	base        Base
	compensator *Compensator
}

func NewPaymentAuthorizedHandler(base Base, compensator *Compensator) *PaymentAuthorizedHandler {
	return &PaymentAuthorizedHandler{base: base, compensator: compensator}
}

func (h *PaymentAuthorizedHandler) Handle(ctx context.Context, cmd PaymentAuthorized) (struct{}, error) {
	id, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return struct{}{}, nil
	}
	var (
		proceed bool
		stray   bool
	)
	_, err = h.base.mutateSaga(ctx, id, func(saga *domain.CheckoutSaga) error {
		stray = saga.PaymentID() != cmd.PaymentID
		proceed, err = saga.BeginCommit(cmd.PaymentID, h.base.Clock.Now())
		return err
	})
	if errors.Is(err, domain.ErrSagaNotFound) {
		return struct{}{}, nil
	}
	if err != nil {
		return struct{}{}, err
	}
	if stray {
		return struct{}{}, h.base.Payments.Cancel(ctx, cmd.PaymentID, "superseded by another payment attempt")
	}
	if !proceed {
		return struct{}{}, nil
	}
	h.base.Metrics.SagaStep("payment_authorized", "success")
	return struct{}{}, advance(ctx, h.base, h.compensator, id)
}

func advance(ctx context.Context, base Base, compensator *Compensator, id domain.OrderID) error {
	saga, err := base.saga(ctx, id)
	if err != nil {
		return err
	}
	if saga.Step() == domain.StepCommittingStock {
		if saga, err = commitStock(ctx, base, compensator, saga); err != nil || saga == nil {
			return err
		}
	}
	if saga.Step() == domain.StepStockCommitted {
		if saga, err = capture(ctx, base, compensator, saga); err != nil || saga == nil {
			return err
		}
	}
	if saga.Step() != domain.StepPaymentCaptured {
		return nil
	}
	return complete(ctx, base, compensator, saga)
}

func commitStock(ctx context.Context, base Base, compensator *Compensator, saga *domain.CheckoutSaga) (*domain.CheckoutSaga, error) {
	err := base.Inventory.Commit(ctx, saga.ReservationID())
	if errors.Is(err, inventoryapi.ErrReservationResolved) || errors.Is(err, inventoryapi.ErrReservationNotFound) ||
		errors.Is(err, inventoryapi.ErrReservationExpired) {
		base.Metrics.SagaStep(string(domain.StepStockCommitted), "rejected")
		return nil, compensator.Abort(ctx, saga.OrderID(), "stock reservation is no longer valid")
	}
	if err != nil {
		return nil, recordFailure(ctx, base, saga.OrderID(), err)
	}
	base.Metrics.SagaStep(string(domain.StepStockCommitted), "success")
	return base.mutateSaga(ctx, saga.OrderID(), func(saga *domain.CheckoutSaga) error {
		return saga.StockCommitted(base.Clock.Now())
	})
}

func capture(ctx context.Context, base Base, compensator *Compensator, saga *domain.CheckoutSaga) (*domain.CheckoutSaga, error) {
	err := base.Payments.Capture(ctx, saga.PaymentID(), saga.Amount().Amount())
	if err != nil && kernel.KindOf(err) != kernel.KindInternal {
		base.Metrics.SagaStep(string(domain.StepPaymentCaptured), "rejected")
		return nil, compensator.Abort(ctx, saga.OrderID(), "payment capture was rejected")
	}
	if err != nil {
		return nil, recordFailure(ctx, base, saga.OrderID(), err)
	}
	base.Metrics.SagaStep(string(domain.StepPaymentCaptured), "success")
	return base.mutateSaga(ctx, saga.OrderID(), func(saga *domain.CheckoutSaga) error {
		return saga.PaymentCaptured(base.Clock.Now())
	})
}

func complete(ctx context.Context, base Base, compensator *Compensator, saga *domain.CheckoutSaga) error {
	err := base.mutateBoth(ctx, saga.OrderID(), func(order *domain.Order, saga *domain.CheckoutSaga) error {
		now := base.Clock.Now()
		if err := order.MarkPaid(saga.Amount(), now); err != nil {
			return err
		}
		return saga.Complete(now)
	})
	if errors.Is(err, &domain.TransitionError{}) || errors.Is(err, domain.ErrPaidAmountMismatch) {
		base.Metrics.SagaStep(string(domain.StepCompleted), "rejected")
		return compensator.Abort(ctx, saga.OrderID(), "order could not be marked as paid")
	}
	if err != nil {
		return err
	}
	base.Metrics.SagaStep(string(domain.StepCompleted), "success")
	return nil
}

func recordFailure(ctx context.Context, base Base, id domain.OrderID, cause error) error {
	_, err := base.mutateSaga(ctx, id, func(saga *domain.CheckoutSaga) error {
		saga.RecordError(cause, base.Clock.Now())
		return nil
	})
	return errors.Join(cause, err)
}

type ResumeStalled struct{}

type ResumeStalledHandler struct {
	base        Base
	compensator *Compensator
}

func NewResumeStalledHandler(base Base, compensator *Compensator) *ResumeStalledHandler {
	return &ResumeStalledHandler{base: base, compensator: compensator}
}

func (h *ResumeStalledHandler) Handle(ctx context.Context, _ ResumeStalled) (int, error) {
	var ids []domain.OrderID
	err := h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		var err error
		ids, err = repos.Sagas().Stalled(ctx, h.base.Clock.Now().Add(-h.base.Policy.StalledAfter), h.base.Policy.BatchSize)
		return err
	})
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, id := range ids {
		errs = append(errs, advance(ctx, h.base, h.compensator, id))
	}
	return len(ids), errors.Join(errs...)
}
