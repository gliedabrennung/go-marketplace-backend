package domain

import (
	"context"
	"time"
)

type OrderRepository interface {
	FindByID(ctx context.Context, id OrderID) (*Order, error)
	Save(ctx context.Context, order *Order) error
	DeliveredBefore(ctx context.Context, before time.Time, limit int) ([]OrderID, error)
}

type SagaRepository interface {
	FindByOrder(ctx context.Context, id OrderID) (*CheckoutSaga, error)
	FindByPayment(ctx context.Context, paymentID string) (*CheckoutSaga, error)
	Expired(ctx context.Context, now time.Time, limit int) ([]OrderID, error)
	Compensating(ctx context.Context, now time.Time, limit int) ([]OrderID, error)
	Stalled(ctx context.Context, before time.Time, limit int) ([]OrderID, error)
	Save(ctx context.Context, saga *CheckoutSaga) error
}
