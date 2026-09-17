package command

import (
	"context"
	"errors"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func actorFor(principal auth.Principal, order *domain.Order) (domain.Actor, error) {
	switch {
	case principal.UserID == "":
		return domain.Actor{}, auth.ErrUnauthenticated
	case principal.UserID == order.BuyerID().String():
		return domain.Actor{Kind: domain.ActorBuyer, ID: principal.UserID}, nil
	case identity.Can(principal, identity.PermOrdersSupport):
		return domain.Actor{Kind: domain.ActorSupport, ID: principal.UserID}, nil
	}
	return domain.Actor{}, domain.ErrOrderNotFound
}

type CancelOrder struct {
	Actor   auth.Principal
	OrderID string
	Reason  string
}

type CancelOrderHandler struct {
	base        Base
	compensator *Compensator
}

func NewCancelOrderHandler(base Base, compensator *Compensator) *CancelOrderHandler {
	return &CancelOrderHandler{base: base, compensator: compensator}
}

func (h *CancelOrderHandler) Handle(ctx context.Context, cmd CancelOrder) (struct{}, error) {
	id, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return struct{}{}, domain.ErrOrderNotFound
	}
	err = h.base.mutateBoth(ctx, id, func(order *domain.Order, saga *domain.CheckoutSaga) error {
		actor, err := actorFor(cmd.Actor, order)
		if err != nil {
			return err
		}
		if saga.Committing() {
			return domain.ErrCancellationDenied.WithDetail("payment is being processed, try again shortly")
		}
		now := h.base.Clock.Now()
		if err := order.Cancel(actor, cmd.Reason, now); err != nil {
			return err
		}
		return saga.BeginCompensation("order cancelled: "+cmd.Reason, domain.NewReference(), now)
	})
	if err != nil {
		return struct{}{}, err
	}
	h.base.Metrics.SagaStep("order_cancelled", "compensating")
	if err := h.compensator.Run(ctx, id); err != nil {
		h.base.Logger.WarnContext(ctx, "order cancellation compensation deferred", "order_id", id.String(), "err", err)
	}
	return struct{}{}, nil
}

type RetryPayment struct {
	Actor     auth.Principal
	PaymentID string
}

type RetryPaymentResult struct {
	OrderID    string
	PaymentID  string
	PaymentURL string
}

type RetryPaymentHandler struct {
	base  Base
	place *PlaceOrderHandler
}

func NewRetryPaymentHandler(base Base, place *PlaceOrderHandler) *RetryPaymentHandler {
	return &RetryPaymentHandler{base: base, place: place}
}

func (h *RetryPaymentHandler) Handle(ctx context.Context, cmd RetryPayment) (RetryPaymentResult, error) {
	var saga *domain.CheckoutSaga
	err := h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		var err error
		saga, err = repos.Sagas().FindByPayment(ctx, cmd.PaymentID)
		return err
	})
	if errors.Is(err, domain.ErrSagaNotFound) || (err == nil && saga.BuyerID().String() != cmd.Actor.UserID) {
		return RetryPaymentResult{}, paymentapi.ErrPaymentNotFound
	}
	if err != nil {
		return RetryPaymentResult{}, err
	}
	if saga.Status() != domain.SagaRunning || saga.Step() != domain.StepAwaitingPayment || saga.Expired(h.base.Clock.Now()) {
		return RetryPaymentResult{}, application.ErrRetryNotAllowed
	}
	current, err := h.base.Payments.Info(ctx, saga.PaymentID())
	if err != nil {
		return RetryPaymentResult{}, err
	}
	result := RetryPaymentResult{OrderID: saga.OrderID().String(), PaymentID: current.PaymentID, PaymentURL: current.RedirectURL}
	switch current.Status {
	case paymentapi.StatusPending:
		return result, nil
	case paymentapi.StatusFailed, paymentapi.StatusCancelled:
	default:
		return RetryPaymentResult{}, application.ErrRetryNotAllowed
	}
	created, err := h.base.Payments.Create(ctx, paymentapi.CreateRequest{
		PaymentID: domain.NewReference(), OrderID: saga.OrderID().String(), BuyerID: saga.BuyerID().String(),
		Amount: saga.Amount().Amount(), Currency: string(saga.Amount().Currency()), ReturnURL: h.place.returnURL(saga.OrderID()),
	})
	if err != nil {
		return RetryPaymentResult{}, err
	}
	if created.Status != paymentapi.StatusPending {
		return RetryPaymentResult{}, application.ErrPaymentUnavailable.WithDetail("payment status %s", created.Status)
	}
	err = h.base.mutateBoth(ctx, saga.OrderID(), func(order *domain.Order, saga *domain.CheckoutSaga) error {
		now := h.base.Clock.Now()
		if err := saga.ChangePayment(created.PaymentID, now); err != nil {
			return err
		}
		return order.AwaitPayment(created.PaymentID, now)
	})
	if err != nil {
		return RetryPaymentResult{}, errors.Join(err, h.base.Payments.Cancel(ctx, created.PaymentID, "retry aborted"))
	}
	h.base.Metrics.SagaStep("payment_retry", "success")
	return RetryPaymentResult{OrderID: saga.OrderID().String(), PaymentID: created.PaymentID, PaymentURL: created.RedirectURL}, nil
}

type ResumeSaga struct {
	Actor   auth.Principal
	OrderID string
}

type ResumeSagaHandler struct {
	base        Base
	compensator *Compensator
}

func NewResumeSagaHandler(base Base, compensator *Compensator) *ResumeSagaHandler {
	return &ResumeSagaHandler{base: base, compensator: compensator}
}

func (h *ResumeSagaHandler) Handle(ctx context.Context, cmd ResumeSaga) (struct{}, error) {
	if err := identity.Authorize(cmd.Actor, identity.PermOrdersSupport); err != nil {
		return struct{}{}, err
	}
	id, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return struct{}{}, domain.ErrSagaNotFound
	}
	if _, err := h.base.mutateSaga(ctx, id, func(saga *domain.CheckoutSaga) error {
		return saga.Resume(h.base.Clock.Now())
	}); err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.compensator.Run(ctx, id)
}
