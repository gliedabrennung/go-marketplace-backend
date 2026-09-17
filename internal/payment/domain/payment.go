package domain

import (
	"slices"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Status string

const (
	StatusCreated    Status = "created"
	StatusPending    Status = "pending"
	StatusAuthorized Status = "authorized"
	StatusCaptured   Status = "captured"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
	StatusRefunded   Status = "refunded"
)

var transitions = map[Status][]Status{
	StatusCreated:    {StatusPending, StatusFailed, StatusCancelled},
	StatusPending:    {StatusAuthorized, StatusFailed, StatusCancelled},
	StatusAuthorized: {StatusCaptured, StatusCancelled},
	StatusCaptured:   {StatusRefunded},
}

func (s Status) IsTerminal() bool {
	return s == StatusFailed || s == StatusCancelled || s == StatusRefunded
}

type RefundStatus string

const (
	RefundPending   RefundStatus = "pending"
	RefundSucceeded RefundStatus = "succeeded"
	RefundFailed    RefundStatus = "failed"
)

type Refund struct {
	id               RefundID
	amount           kernel.Money
	reason           string
	status           RefundStatus
	providerRefundID string
	createdAt        time.Time
	completedAt      time.Time
}

func (r Refund) ID() RefundID { return r.id }

func (r Refund) Amount() kernel.Money { return r.amount }

func (r Refund) Status() RefundStatus { return r.status }

func (r Refund) ProviderRefundID() string { return r.providerRefundID }

type Payment struct {
	id                PaymentID
	orderID           OrderID
	buyerID           kernel.UserID
	provider          string
	providerPaymentID string
	redirectURL       string
	methodID          MethodID
	saveMethod        bool
	status            Status
	amount            kernel.Money
	authorized        kernel.Money
	captured          kernel.Money
	refunded          kernel.Money
	refunds           []Refund
	failureReason     string
	createdAt         time.Time
	updatedAt         time.Time
	version           int

	events kernel.EventBuffer
}

type PaymentRequest struct {
	ID         PaymentID
	OrderID    OrderID
	BuyerID    kernel.UserID
	Provider   string
	Amount     kernel.Money
	MethodID   MethodID
	SaveMethod bool
}

func InitiatePayment(req PaymentRequest, now time.Time) (*Payment, error) {
	if req.ID.IsZero() || req.OrderID.IsZero() || req.BuyerID.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if strings.TrimSpace(req.Provider) == "" {
		return nil, ErrInvalidProvider
	}
	if req.Amount.Amount() <= 0 {
		return nil, ErrInvalidAmount
	}
	zero := kernel.ZeroLike(req.Amount)
	return &Payment{
		id: req.ID, orderID: req.OrderID, buyerID: req.BuyerID, provider: req.Provider,
		methodID: req.MethodID, saveMethod: req.SaveMethod && req.MethodID.IsZero(),
		status: StatusCreated, amount: req.Amount, authorized: zero, captured: zero, refunded: zero,
		createdAt: now, updatedAt: now,
	}, nil
}

func (p *Payment) AttachIntent(providerPaymentID, redirectURL string, now time.Time) error {
	if p.status == StatusPending && p.providerPaymentID == providerPaymentID {
		return nil
	}
	if providerPaymentID == "" || redirectURL == "" {
		return ErrInvalidIntent
	}
	if err := p.transition(StatusPending, now); err != nil {
		return err
	}
	p.providerPaymentID, p.redirectURL = providerPaymentID, redirectURL
	p.events.Record(PaymentPending{PaymentID: p.id, OrderID: p.orderID, Amount: p.amount, At: now})
	return nil
}

func (p *Payment) Authorize(amount kernel.Money, now time.Time) error {
	if p.status == StatusAuthorized || p.status == StatusCaptured {
		return nil
	}
	if err := p.allows(StatusAuthorized); err != nil {
		return err
	}
	if !amount.Equals(p.amount) {
		return ErrAmountMismatch.WithDetail("expected %s, got %s", p.amount, amount)
	}
	if err := p.transition(StatusAuthorized, now); err != nil {
		return err
	}
	p.authorized = amount
	p.events.Record(PaymentAuthorized{PaymentID: p.id, OrderID: p.orderID, Amount: amount, At: now})
	return nil
}

func (p *Payment) Fail(reason string, now time.Time) error {
	if p.status == StatusFailed {
		return nil
	}
	if err := p.transition(StatusFailed, now); err != nil {
		return err
	}
	p.failureReason = strings.TrimSpace(reason)
	p.events.Record(PaymentFailed{PaymentID: p.id, OrderID: p.orderID, Reason: p.failureReason, At: now})
	return nil
}

func (p *Payment) Capture(amount kernel.Money, now time.Time) error {
	if p.status == StatusCaptured && amount.Equals(p.captured) {
		return nil
	}
	if err := p.allows(StatusCaptured); err != nil {
		return err
	}
	if amount.Amount() <= 0 {
		return ErrInvalidAmount
	}
	compare, err := amount.Compare(p.authorized)
	if err != nil {
		return ErrCurrencyMismatch
	}
	if compare > 0 {
		return ErrCaptureExceedsHold
	}
	if err := p.transition(StatusCaptured, now); err != nil {
		return err
	}
	p.captured = amount
	p.events.Record(PaymentCaptured{PaymentID: p.id, OrderID: p.orderID, Amount: amount, At: now})
	return nil
}

func (p *Payment) Cancel(reason string, now time.Time) error {
	if p.status == StatusCancelled {
		return nil
	}
	if err := p.transition(StatusCancelled, now); err != nil {
		return err
	}
	p.failureReason = strings.TrimSpace(reason)
	p.events.Record(PaymentCancelled{PaymentID: p.id, OrderID: p.orderID, Reason: p.failureReason, At: now})
	return nil
}

func (p *Payment) RequestRefund(id RefundID, amount kernel.Money, reason string, now time.Time) (Refund, error) {
	if existing, err := p.Refund(id); err == nil {
		return existing, nil
	}
	if id.IsZero() {
		return Refund{}, kernel.ErrInvalidID
	}
	if p.status != StatusCaptured {
		return Refund{}, &TransitionError{From: p.status, To: StatusRefunded}
	}
	if amount.Amount() <= 0 {
		return Refund{}, ErrInvalidAmount
	}
	outstanding, err := p.outstandingRefunds()
	if err != nil {
		return Refund{}, err
	}
	total, err := outstanding.Add(amount)
	if err != nil {
		return Refund{}, ErrCurrencyMismatch
	}
	if compare, err := total.Compare(p.captured); err != nil || compare > 0 {
		return Refund{}, ErrRefundExceedsCharge
	}
	refund := Refund{id: id, amount: amount, reason: strings.TrimSpace(reason), status: RefundPending, createdAt: now}
	p.refunds = append(p.refunds, refund)
	p.updatedAt = now
	p.events.Record(RefundRequested{
		PaymentID: p.id, OrderID: p.orderID, RefundID: id, Amount: amount, Reason: refund.reason, At: now,
	})
	return refund, nil
}

func (p *Payment) CompleteRefund(id RefundID, providerRefundID string, now time.Time) error {
	refund, err := p.pendingRefund(id)
	if err != nil || refund == nil {
		return err
	}
	refunded, err := p.refunded.Add(refund.amount)
	if err != nil {
		return ErrCurrencyMismatch
	}
	refund.status, refund.providerRefundID, refund.completedAt = RefundSucceeded, providerRefundID, now
	p.refunded, p.updatedAt = refunded, now
	if refunded.Equals(p.captured) {
		p.status = StatusRefunded
	}
	p.events.Record(RefundCompleted{
		PaymentID: p.id, OrderID: p.orderID, RefundID: id, Amount: refund.amount, Refunded: refunded, Succeeded: true, At: now,
	})
	return nil
}

func (p *Payment) FailRefund(id RefundID, reason string, now time.Time) error {
	refund, err := p.pendingRefund(id)
	if err != nil || refund == nil {
		return err
	}
	refund.status, refund.reason, refund.completedAt = RefundFailed, strings.TrimSpace(reason), now
	p.updatedAt = now
	p.events.Record(RefundCompleted{
		PaymentID: p.id, OrderID: p.orderID, RefundID: id, Amount: refund.amount, Refunded: p.refunded,
		Reason: refund.reason, At: now,
	})
	return nil
}

func (p *Payment) pendingRefund(id RefundID) (*Refund, error) {
	index := slices.IndexFunc(p.refunds, func(r Refund) bool { return r.id == id })
	if index < 0 {
		return nil, ErrRefundNotFound
	}
	refund := &p.refunds[index]
	if refund.status != RefundPending {
		return nil, nil
	}
	return refund, nil
}

func (p *Payment) outstandingRefunds() (kernel.Money, error) {
	total := kernel.ZeroLike(p.amount)
	for _, refund := range p.refunds {
		if refund.status == RefundFailed {
			continue
		}
		next, err := total.Add(refund.amount)
		if err != nil {
			return kernel.Money{}, ErrCurrencyMismatch
		}
		total = next
	}
	return total, nil
}

func (p *Payment) RefundableAmount() kernel.Money {
	outstanding, err := p.outstandingRefunds()
	if err != nil {
		return kernel.ZeroLike(p.amount)
	}
	left, err := p.captured.Sub(outstanding)
	if err != nil {
		return kernel.ZeroLike(p.amount)
	}
	return left
}

func (p *Payment) transition(target Status, now time.Time) error {
	if err := p.allows(target); err != nil {
		return err
	}
	p.status, p.updatedAt = target, now
	return nil
}

func (p *Payment) allows(target Status) error {
	if p.status.IsTerminal() {
		return ErrPaymentTerminal.WithDetail("status %s", p.status)
	}
	if !slices.Contains(transitions[p.status], target) {
		return &TransitionError{From: p.status, To: target}
	}
	return nil
}

func (p *Payment) Refund(id RefundID) (Refund, error) {
	index := slices.IndexFunc(p.refunds, func(r Refund) bool { return r.id == id })
	if index < 0 {
		return Refund{}, ErrRefundNotFound
	}
	return p.refunds[index], nil
}

func (p *Payment) ID() PaymentID { return p.id }

func (p *Payment) OrderID() OrderID { return p.orderID }

func (p *Payment) BuyerID() kernel.UserID { return p.buyerID }

func (p *Payment) Provider() string { return p.provider }

func (p *Payment) ProviderPaymentID() string { return p.providerPaymentID }

func (p *Payment) RedirectURL() string { return p.redirectURL }

func (p *Payment) MethodID() MethodID { return p.methodID }

func (p *Payment) SaveMethod() bool { return p.saveMethod }

func (p *Payment) Status() Status { return p.status }

func (p *Payment) Amount() kernel.Money { return p.amount }

func (p *Payment) Authorized() kernel.Money { return p.authorized }

func (p *Payment) Captured() kernel.Money { return p.captured }

func (p *Payment) Refunded() kernel.Money { return p.refunded }

func (p *Payment) Refunds() []Refund { return slices.Clone(p.refunds) }

func (p *Payment) FailureReason() string { return p.failureReason }

func (p *Payment) Version() int { return p.version }

func (p *Payment) AdvanceVersion() { p.version++ }

func (p *Payment) PullEvents() []kernel.DomainEvent { return p.events.Pull() }
