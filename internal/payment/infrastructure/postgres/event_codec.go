package postgres

import (
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

func NewEventCodec() *outbox.Codec {
	c := outbox.NewCodec()
	outbox.Register(c, func(e domain.PaymentPending) any {
		return api.PaymentV1{
			PaymentID: e.PaymentID.String(), OrderID: e.OrderID.String(), Amount: e.Amount.Amount(),
			Currency: string(e.Amount.Currency()), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.PaymentAuthorized) any {
		return api.PaymentV1{
			PaymentID: e.PaymentID.String(), OrderID: e.OrderID.String(), Amount: e.Amount.Amount(),
			Currency: string(e.Amount.Currency()), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.PaymentFailed) any {
		return api.PaymentV1{PaymentID: e.PaymentID.String(), OrderID: e.OrderID.String(), Reason: e.Reason, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.PaymentCaptured) any {
		return api.PaymentV1{
			PaymentID: e.PaymentID.String(), OrderID: e.OrderID.String(), Amount: e.Amount.Amount(),
			Currency: string(e.Amount.Currency()), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.PaymentCancelled) any {
		return api.PaymentV1{PaymentID: e.PaymentID.String(), OrderID: e.OrderID.String(), Reason: e.Reason, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.RefundRequested) any {
		return api.RefundV1{
			PaymentID: e.PaymentID.String(), OrderID: e.OrderID.String(), RefundID: e.RefundID.String(),
			Amount: e.Amount.Amount(), Currency: string(e.Amount.Currency()), Reason: e.Reason, OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.RefundCompleted) any {
		return api.RefundV1{
			PaymentID: e.PaymentID.String(), OrderID: e.OrderID.String(), RefundID: e.RefundID.String(),
			Amount: e.Amount.Amount(), Refunded: e.Refunded.Amount(), Currency: string(e.Amount.Currency()),
			Succeeded: e.Succeeded, Reason: e.Reason, OccurredAt: e.At.UTC(),
		}
	})
	return c
}
