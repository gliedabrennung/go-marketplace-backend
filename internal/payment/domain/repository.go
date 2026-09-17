package domain

import (
	"context"
	"time"
)

type PaymentRepository interface {
	FindByID(ctx context.Context, id PaymentID) (*Payment, error)
	FindByProviderID(ctx context.Context, provider, providerPaymentID string) (*Payment, error)
	Save(ctx context.Context, payment *Payment) error
}

type MethodRepository interface {
	FindByID(ctx context.Context, id MethodID) (*SavedMethod, error)
	FindByToken(ctx context.Context, provider, token string) (*SavedMethod, error)
	Save(ctx context.Context, method *SavedMethod) error
}

type WebhookLog interface {
	Record(ctx context.Context, provider, eventID, eventType string, receivedAt time.Time) (bool, error)
}
