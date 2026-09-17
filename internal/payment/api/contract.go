package api

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
)

var (
	ErrPaymentNotFound = domain.ErrPaymentNotFound
	ErrPaymentTerminal = domain.ErrPaymentTerminal
)

const (
	StatusCreated    = string(domain.StatusCreated)
	StatusPending    = string(domain.StatusPending)
	StatusAuthorized = string(domain.StatusAuthorized)
	StatusCaptured   = string(domain.StatusCaptured)
	StatusFailed     = string(domain.StatusFailed)
	StatusCancelled  = string(domain.StatusCancelled)
	StatusRefunded   = string(domain.StatusRefunded)
)

var ErrInvalidTransition error = &domain.TransitionError{}

func IsInvalidTransition(err error) bool {
	return errors.Is(err, ErrInvalidTransition)
}

type CreateRequest struct {
	PaymentID  string
	OrderID    string
	BuyerID    string
	Amount     int64
	Currency   string
	ReturnURL  string
	SaveMethod bool
	MethodID   string
}

type Created struct {
	PaymentID   string
	RedirectURL string
	Status      string
}

type Info struct {
	PaymentID   string
	OrderID     string
	Status      string
	RedirectURL string
	Amount      int64
	Authorized  int64
	Captured    int64
	Refunded    int64
	Currency    string
}

type Payments interface {
	Create(ctx context.Context, request CreateRequest) (Created, error)
	Capture(ctx context.Context, paymentID string, amount int64) error
	Cancel(ctx context.Context, paymentID, reason string) error
	Refund(ctx context.Context, paymentID, refundID string, amount int64, reason string) error
	Info(ctx context.Context, paymentID string) (Info, error)
}
