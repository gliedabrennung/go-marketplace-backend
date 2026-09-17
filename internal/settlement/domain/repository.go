package domain

import (
	"context"
	"time"
)

type Repository interface {
	FindByOrderAndSeller(ctx context.Context, orderID string, sellerID string) (*Entry, error)
	Save(ctx context.Context, entry *Entry) error
	ListBySeller(ctx context.Context, sellerID string, from, to time.Time) ([]*Entry, error)
}
