package domain

import (
	"context"
	"time"
)

type StockRepository interface {
	FindBySKU(ctx context.Context, sku SKU) (*StockItem, error)
	Lock(ctx context.Context, skus []SKU) ([]*StockItem, error)
	LockByReservation(ctx context.Context, id ReservationID) ([]*StockItem, error)
	LockExpired(ctx context.Context, before time.Time, limit int) ([]*StockItem, error)
	Save(ctx context.Context, item *StockItem) error
}
