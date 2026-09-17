package postgres

import (
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

func NewEventCodec() *outbox.Codec {
	c := outbox.NewCodec()
	outbox.Register(c, func(e domain.StockChanged) any {
		return api.StockChangedV1{
			SKU: e.SKU.String(), SellerID: e.SellerID.String(), Available: e.Available, Reserved: e.Reserved,
			Reason: string(e.Reason), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.StockReserved) any {
		return api.StockReservedV1{
			SKU: e.SKU.String(), SellerID: e.SellerID.String(), ReservationID: e.ReservationID.String(),
			OrderID: optionalID(e.OrderID), Quantity: e.Quantity, ExpiresAt: e.ExpiresAt.UTC(), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.ReservationResolved) any {
		return api.ReservationResolvedV1{
			SKU: e.SKU.String(), SellerID: e.SellerID.String(), ReservationID: e.ReservationID.String(),
			OrderID: optionalID(e.OrderID), Quantity: e.Quantity, Status: string(e.Status), OccurredAt: e.At.UTC(),
		}
	})
	return c
}

func optionalID(id domain.OrderID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}
