package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Base struct {
	uow       application.UnitOfWork
	clock     application.Clock
	providers application.Providers
}

func NewBase(uow application.UnitOfWork, clock application.Clock, providers application.Providers) Base {
	return Base{uow: uow, clock: clock, providers: providers}
}

func (b Base) load(ctx context.Context, rawID string) (*domain.Payment, application.Provider, error) {
	id, err := domain.ParsePaymentID(rawID)
	if err != nil {
		return nil, nil, domain.ErrPaymentNotFound
	}
	var payment *domain.Payment
	err = b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		payment, err = repos.Payments().FindByID(ctx, id)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	provider, err := b.providers.Get(payment.Provider())
	if err != nil {
		return nil, nil, err
	}
	return payment, provider, nil
}

func (b Base) mutate(ctx context.Context, id domain.PaymentID, change func(*domain.Payment) error) (*domain.Payment, error) {
	var payment *domain.Payment
	err := b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		found, err := repos.Payments().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if err := change(found); err != nil {
			return err
		}
		payment = found
		return repos.Payments().Save(ctx, found)
	})
	return payment, err
}

type CreatePayment struct {
	PaymentID  string
	OrderID    string
	BuyerID    string
	Amount     int64
	Currency   string
	ReturnURL  string
	SaveMethod bool
	MethodID   string
}

type CreatePaymentResult struct {
	PaymentID   string
	RedirectURL string
	Status      string
}

type CreatePaymentHandler struct {
	base Base
}

func NewCreatePaymentHandler(base Base) *CreatePaymentHandler {
	return &CreatePaymentHandler{base: base}
}

func (h *CreatePaymentHandler) Handle(ctx context.Context, cmd CreatePayment) (CreatePaymentResult, error) {
	request, err := h.request(cmd)
	if err != nil {
		return CreatePaymentResult{}, err
	}
	provider := h.base.providers.Default()
	request.Provider = provider.Name()
	now := h.base.clock.Now()

	var (
		payment *domain.Payment
		token   string
	)
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		existing, err := repos.Payments().FindByID(ctx, request.ID)
		if err == nil {
			payment = existing
			return nil
		}
		if !errors.Is(err, domain.ErrPaymentNotFound) {
			return err
		}
		if !request.MethodID.IsZero() {
			method, err := repos.Methods().FindByID(ctx, request.MethodID)
			if err != nil {
				return err
			}
			if err := method.Usable(request.BuyerID, provider.Name()); err != nil {
				return err
			}
			token = method.Token()
		}
		if payment, err = domain.InitiatePayment(request, now); err != nil {
			return err
		}
		return repos.Payments().Save(ctx, payment)
	})
	if err != nil {
		return CreatePaymentResult{}, err
	}
	if payment.Status() != domain.StatusCreated {
		return result(payment), nil
	}

	intent, err := provider.CreateIntent(ctx, application.IntentRequest{
		PaymentID: payment.ID().String(), OrderID: payment.OrderID().String(), Amount: payment.Amount(),
		ReturnURL: cmd.ReturnURL, Description: "order " + payment.OrderID().String(),
		SaveMethod: payment.SaveMethod(), MethodToken: token,
	})
	if err != nil {
		if _, failErr := h.base.mutate(ctx, payment.ID(), func(p *domain.Payment) error {
			return p.Fail("provider error: "+err.Error(), h.base.clock.Now())
		}); failErr != nil {
			return CreatePaymentResult{}, errors.Join(err, failErr)
		}
		return CreatePaymentResult{}, fmt.Errorf("create payment intent: %w", err)
	}
	payment, err = h.base.mutate(ctx, payment.ID(), func(p *domain.Payment) error {
		return p.AttachIntent(intent.ProviderPaymentID, intent.RedirectURL, h.base.clock.Now())
	})
	if err != nil {
		return CreatePaymentResult{}, err
	}
	return result(payment), nil
}

func (h *CreatePaymentHandler) request(cmd CreatePayment) (domain.PaymentRequest, error) {
	id, err := domain.ParsePaymentID(cmd.PaymentID)
	if err != nil {
		return domain.PaymentRequest{}, kernel.ErrInvalidID
	}
	order, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return domain.PaymentRequest{}, kernel.ErrInvalidID
	}
	buyer, err := kernel.ParseUserID(cmd.BuyerID)
	if err != nil {
		return domain.PaymentRequest{}, kernel.ErrInvalidID
	}
	currency, err := kernel.NewCurrency(cmd.Currency)
	if err != nil {
		return domain.PaymentRequest{}, err
	}
	amount, err := kernel.NewMoney(cmd.Amount, currency)
	if err != nil {
		return domain.PaymentRequest{}, err
	}
	request := domain.PaymentRequest{ID: id, OrderID: order, BuyerID: buyer, Amount: amount, SaveMethod: cmd.SaveMethod}
	if cmd.MethodID != "" {
		if request.MethodID, err = domain.ParseMethodID(cmd.MethodID); err != nil {
			return domain.PaymentRequest{}, domain.ErrMethodNotFound
		}
	}
	return request, nil
}

func result(payment *domain.Payment) CreatePaymentResult {
	return CreatePaymentResult{
		PaymentID: payment.ID().String(), RedirectURL: payment.RedirectURL(), Status: string(payment.Status()),
	}
}

type CapturePayment struct {
	PaymentID string
	Amount    int64
}

type CapturePaymentHandler struct {
	base Base
}

func NewCapturePaymentHandler(base Base) *CapturePaymentHandler {
	return &CapturePaymentHandler{base: base}
}

func (h *CapturePaymentHandler) Handle(ctx context.Context, cmd CapturePayment) (struct{}, error) {
	payment, provider, err := h.base.load(ctx, cmd.PaymentID)
	if err != nil {
		return struct{}{}, err
	}
	amount := payment.Authorized()
	if cmd.Amount > 0 {
		if amount, err = kernel.NewMoney(cmd.Amount, payment.Amount().Currency()); err != nil {
			return struct{}{}, err
		}
	}
	if payment.Status() == domain.StatusCaptured && payment.Captured().Equals(amount) {
		return struct{}{}, nil
	}
	if payment.Status() != domain.StatusAuthorized {
		return struct{}{}, &domain.TransitionError{From: payment.Status(), To: domain.StatusCaptured}
	}
	if err := provider.Capture(ctx, payment.ProviderPaymentID(), amount, "capture:"+payment.ID().String()); err != nil {
		return struct{}{}, fmt.Errorf("capture payment: %w", err)
	}
	_, err = h.base.mutate(ctx, payment.ID(), func(p *domain.Payment) error {
		return p.Capture(amount, h.base.clock.Now())
	})
	return struct{}{}, err
}

type CancelPayment struct {
	PaymentID string
	Reason    string
}

type CancelPaymentHandler struct {
	base Base
}

func NewCancelPaymentHandler(base Base) *CancelPaymentHandler {
	return &CancelPaymentHandler{base: base}
}

func (h *CancelPaymentHandler) Handle(ctx context.Context, cmd CancelPayment) (struct{}, error) {
	payment, provider, err := h.base.load(ctx, cmd.PaymentID)
	if err != nil {
		return struct{}{}, err
	}
	switch payment.Status() {
	case domain.StatusCancelled, domain.StatusFailed:
		return struct{}{}, nil
	case domain.StatusPending, domain.StatusAuthorized:
		if err := provider.Cancel(ctx, payment.ProviderPaymentID(), "cancel:"+payment.ID().String()); err != nil {
			return struct{}{}, fmt.Errorf("cancel payment: %w", err)
		}
	}
	_, err = h.base.mutate(ctx, payment.ID(), func(p *domain.Payment) error {
		return p.Cancel(cmd.Reason, h.base.clock.Now())
	})
	return struct{}{}, err
}

type RefundPayment struct {
	PaymentID string
	RefundID  string
	Amount    int64
	Reason    string
}

type RefundPaymentResult struct {
	RefundID string
	Status   string
	Amount   int64
}

type RefundPaymentHandler struct {
	base Base
}

func NewRefundPaymentHandler(base Base) *RefundPaymentHandler {
	return &RefundPaymentHandler{base: base}
}

func (h *RefundPaymentHandler) Handle(ctx context.Context, cmd RefundPayment) (RefundPaymentResult, error) {
	payment, provider, err := h.base.load(ctx, cmd.PaymentID)
	if err != nil {
		return RefundPaymentResult{}, err
	}
	refundID, err := domain.ParseRefundID(cmd.RefundID)
	if err != nil {
		return RefundPaymentResult{}, kernel.ErrInvalidID
	}
	amount := payment.RefundableAmount()
	if cmd.Amount > 0 {
		if amount, err = kernel.NewMoney(cmd.Amount, payment.Amount().Currency()); err != nil {
			return RefundPaymentResult{}, err
		}
	}

	var refund domain.Refund
	payment, err = h.base.mutate(ctx, payment.ID(), func(p *domain.Payment) error {
		refund, err = p.RequestRefund(refundID, amount, cmd.Reason, h.base.clock.Now())
		return err
	})
	if err != nil {
		return RefundPaymentResult{}, err
	}
	if refund.Status() != domain.RefundPending {
		return refundResult(refund), nil
	}

	providerRefundID, err := provider.Refund(ctx, payment.ProviderPaymentID(), refund.Amount(), "refund:"+refundID.String())
	now := h.base.clock.Now()
	payment, mutateErr := h.base.mutate(ctx, payment.ID(), func(p *domain.Payment) error {
		if err != nil {
			return p.FailRefund(refundID, err.Error(), now)
		}
		return p.CompleteRefund(refundID, providerRefundID, now)
	})
	if mutateErr != nil {
		return RefundPaymentResult{}, errors.Join(err, mutateErr)
	}
	if err != nil {
		return RefundPaymentResult{}, fmt.Errorf("refund payment: %w", err)
	}
	refund, err = payment.Refund(refundID)
	if err != nil {
		return RefundPaymentResult{}, err
	}
	return refundResult(refund), nil
}

func refundResult(refund domain.Refund) RefundPaymentResult {
	return RefundPaymentResult{RefundID: refund.ID().String(), Status: string(refund.Status()), Amount: refund.Amount().Amount()}
}
