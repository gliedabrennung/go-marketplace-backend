package domain

import (
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type SagaStatus string

const (
	SagaRunning      SagaStatus = "running"
	SagaCompensating SagaStatus = "compensating"
	SagaCompleted    SagaStatus = "completed"
	SagaCompensated  SagaStatus = "compensated"
	SagaManual       SagaStatus = "manual"
)

type Step string

const (
	StepStockReserved      Step = "stock_reserved"
	StepPromoRedeemed      Step = "promo_redeemed"
	StepAwaitingPayment    Step = "awaiting_payment"
	StepCommittingStock    Step = "committing_stock"
	StepStockCommitted     Step = "stock_committed"
	StepPaymentCaptured    Step = "payment_captured"
	StepCompleted          Step = "completed"
	CompensationRefund     Step = "refund_payment"
	CompensationCancel     Step = "cancel_payment"
	CompensationStock      Step = "return_stock"
	CompensationPromo      Step = "release_promo"
	CompensationCloseOrder Step = "close_order"
)

type SagaSpec struct {
	OrderID       OrderID
	BuyerID       kernel.UserID
	ReservationID string
	PaymentID     string
	PromoCode     string
	Amount        kernel.Money
	Deadline      time.Time
}

type CheckoutSaga struct {
	orderID       OrderID
	buyerID       kernel.UserID
	reservationID string
	paymentID     string
	refundID      string
	promoCode     string
	amount        kernel.Money
	status        SagaStatus
	step          Step
	captured      bool
	compensated   []Step
	reason        string
	lastError     string
	attempts      int
	deadline      time.Time
	createdAt     time.Time
	updatedAt     time.Time
	version       int
}

func StartSaga(spec SagaSpec, now time.Time) (*CheckoutSaga, error) {
	if spec.OrderID.IsZero() || spec.BuyerID.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if strings.TrimSpace(spec.ReservationID) == "" || strings.TrimSpace(spec.PaymentID) == "" {
		return nil, ErrInvalidReference
	}
	return &CheckoutSaga{
		orderID: spec.OrderID, buyerID: spec.BuyerID, reservationID: spec.ReservationID, paymentID: spec.PaymentID,
		promoCode: spec.PromoCode, amount: spec.Amount, status: SagaRunning, step: StepStockReserved,
		deadline: spec.Deadline, createdAt: now, updatedAt: now,
	}, nil
}

func (s *CheckoutSaga) PromoRedeemed(now time.Time) error {
	if err := s.running(); err != nil {
		return err
	}
	s.step, s.updatedAt = StepPromoRedeemed, now
	return nil
}

func (s *CheckoutSaga) AwaitPayment(paymentID string, now time.Time) error {
	if err := s.running(); err != nil {
		return err
	}
	if strings.TrimSpace(paymentID) == "" {
		return ErrInvalidReference
	}
	s.paymentID, s.step, s.updatedAt = paymentID, StepAwaitingPayment, now
	return nil
}

func (s *CheckoutSaga) BeginCommit(paymentID string, now time.Time) (bool, error) {
	if s.status != SagaRunning || s.paymentID != paymentID {
		return false, nil
	}
	switch s.step {
	case StepAwaitingPayment:
		s.step, s.updatedAt = StepCommittingStock, now
		return true, nil
	case StepCommittingStock, StepStockCommitted, StepPaymentCaptured:
		return true, nil
	}
	return false, nil
}

func (s *CheckoutSaga) StockCommitted(now time.Time) error {
	if err := s.running(); err != nil {
		return err
	}
	s.step, s.updatedAt = StepStockCommitted, now
	return nil
}

func (s *CheckoutSaga) PaymentCaptured(now time.Time) error {
	if err := s.running(); err != nil {
		return err
	}
	s.captured, s.step, s.updatedAt = true, StepPaymentCaptured, now
	return nil
}

func (s *CheckoutSaga) Complete(now time.Time) error {
	if s.status == SagaCompleted {
		return nil
	}
	if err := s.running(); err != nil {
		return err
	}
	s.status, s.step, s.attempts, s.lastError, s.updatedAt = SagaCompleted, StepCompleted, 0, "", now
	return nil
}

func (s *CheckoutSaga) RecordError(err error, now time.Time) {
	s.lastError, s.updatedAt = truncate(err.Error()), now
}

func (s *CheckoutSaga) Expired(now time.Time) bool {
	return s.status == SagaRunning && !s.Committing() && !s.deadline.IsZero() && now.After(s.deadline)
}

func (s *CheckoutSaga) DueForCompensation(now time.Time) bool {
	return s.status == SagaCompensating && !now.Before(s.updatedAt.Add(CompensationBackoff(s.attempts)))
}

func CompensationBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	return min(10*time.Second<<min(attempts-1, 10), 10*time.Minute)
}

func (s *CheckoutSaga) Committing() bool {
	return s.status == SagaRunning && (s.step == StepCommittingStock || s.step == StepStockCommitted || s.step == StepPaymentCaptured)
}

func (s *CheckoutSaga) BeginCompensation(reason, refundID string, now time.Time) error {
	switch s.status {
	case SagaCompensating, SagaCompensated, SagaManual:
		return nil
	}
	if strings.TrimSpace(refundID) == "" {
		return ErrInvalidReference
	}
	s.status, s.reason, s.refundID, s.attempts, s.updatedAt = SagaCompensating, truncate(reason), refundID, 0, now
	return nil
}

func (s *CheckoutSaga) Pending() []Step {
	if s.status != SagaCompensating {
		return nil
	}
	plan := []Step{CompensationCancel}
	if s.captured {
		plan[0] = CompensationRefund
	}
	plan = append(plan, CompensationStock)
	if s.promoCode != "" {
		plan = append(plan, CompensationPromo)
	}
	plan = append(plan, CompensationCloseOrder)
	out := plan[:0]
	for _, step := range plan {
		if !s.done(step) {
			out = append(out, step)
		}
	}
	return out
}

func (s *CheckoutSaga) Compensated(step Step, now time.Time) {
	if !s.done(step) {
		s.compensated = append(s.compensated, step)
	}
	s.updatedAt = now
	if s.status == SagaCompensating && len(s.Pending()) == 0 {
		s.status, s.lastError = SagaCompensated, ""
	}
}

func (s *CheckoutSaga) CompensationFailed(err error, maxAttempts int, now time.Time) {
	s.attempts++
	s.lastError, s.updatedAt = truncate(err.Error()), now
	if s.attempts >= maxAttempts {
		s.status = SagaManual
	}
}

func (s *CheckoutSaga) Resume(now time.Time) error {
	if s.status != SagaManual {
		return ErrSagaNotManual
	}
	s.status, s.attempts, s.updatedAt = SagaCompensating, 0, now
	return nil
}

func (s *CheckoutSaga) ChangePayment(paymentID string, now time.Time) error {
	if s.status != SagaRunning || s.step != StepAwaitingPayment {
		return ErrSagaNotRunning
	}
	if strings.TrimSpace(paymentID) == "" {
		return ErrInvalidReference
	}
	s.paymentID, s.updatedAt = paymentID, now
	return nil
}

func (s *CheckoutSaga) done(step Step) bool {
	for _, completed := range s.compensated {
		if completed == step {
			return true
		}
	}
	return false
}

func (s *CheckoutSaga) running() error {
	if s.status != SagaRunning {
		return ErrSagaNotRunning.WithDetail("status %s", s.status)
	}
	return nil
}

func truncate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}

func (s *CheckoutSaga) OrderID() OrderID { return s.orderID }

func (s *CheckoutSaga) BuyerID() kernel.UserID { return s.buyerID }

func (s *CheckoutSaga) ReservationID() string { return s.reservationID }

func (s *CheckoutSaga) PaymentID() string { return s.paymentID }

func (s *CheckoutSaga) RefundID() string { return s.refundID }

func (s *CheckoutSaga) PromoCode() string { return s.promoCode }

func (s *CheckoutSaga) Amount() kernel.Money { return s.amount }

func (s *CheckoutSaga) Status() SagaStatus { return s.status }

func (s *CheckoutSaga) Step() Step { return s.step }

func (s *CheckoutSaga) Captured() bool { return s.captured }

func (s *CheckoutSaga) Reason() string { return s.reason }

func (s *CheckoutSaga) LastError() string { return s.lastError }

func (s *CheckoutSaga) Attempts() int { return s.attempts }

func (s *CheckoutSaga) Deadline() time.Time { return s.deadline }

func (s *CheckoutSaga) Version() int { return s.version }

func (s *CheckoutSaga) AdvanceVersion() { s.version++ }
