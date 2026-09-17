package api

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
)

var (
	ErrInsufficientStock       = domain.ErrInsufficientStock
	ErrStockNotFound           = domain.ErrStockNotFound
	ErrReservationNotFound     = domain.ErrReservationNotFound
	ErrReservationResolved     = domain.ErrReservationResolved
	ErrReservationExpired      = domain.ErrReservationExpired
	ErrReservationNotCommitted = domain.ErrReservationNotCommitted
)

type ReserveLine struct {
	SKU      string
	Quantity int
}

type Reservation struct {
	ReservationID string
	ExpiresAt     time.Time
}

type Reserver interface {
	Reserve(ctx context.Context, reservationID, orderID string, lines []ReserveLine) (Reservation, error)
	Commit(ctx context.Context, reservationID string) error
	Release(ctx context.Context, reservationID string) error
	Restore(ctx context.Context, reservationID string) error
}

type Availability interface {
	Available(ctx context.Context, skus []string) (map[string]int, error)
}
