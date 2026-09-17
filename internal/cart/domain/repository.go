package domain

import (
	"context"
	"time"
)

type Repository interface {
	FindByOwner(ctx context.Context, owner Owner) (*Cart, error)
	Save(ctx context.Context, cart *Cart) error
	Delete(ctx context.Context, cart *Cart) error
	DeleteExpired(ctx context.Context, before time.Time, limit int) (int, error)
}
