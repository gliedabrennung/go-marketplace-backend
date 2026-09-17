package postgres

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

func NewEventCodec() *outbox.Codec {
	c := outbox.NewCodec()
	outbox.Register(c, func(e domain.OrderCreated) any { return order(e.Order, "", e.At) })
	outbox.Register(c, func(e domain.OrderPaid) any { return order(e.Order, e.PaymentID, e.At) })
	outbox.Register(c, func(e domain.OrderAwaitingPayment) any {
		return api.OrderStatusV1{OrderID: e.OrderID.String(), BuyerID: e.BuyerID.String(), PaymentID: e.PaymentID, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.OrderFailed) any {
		return api.OrderStatusV1{OrderID: e.OrderID.String(), BuyerID: e.BuyerID.String(), Reason: e.Reason, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.OrderCancelled) any {
		return api.OrderStatusV1{
			OrderID: e.OrderID.String(), BuyerID: e.BuyerID.String(), ActorKind: string(e.Actor.Kind), Reason: e.Reason,
			RefundRequired: e.RefundRequired, Total: e.Total.Amount(), Currency: string(e.Total.Currency()), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.OrderShipped) any {
		return api.OrderShipmentV1{OrderID: e.OrderID.String(), BuyerID: e.BuyerID.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.OrderDelivered) any {
		return api.OrderShipmentV1{OrderID: e.OrderID.String(), BuyerID: e.BuyerID.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.OrderCompleted) any { return order(e.Order, "", e.At) })
	return c
}

func order(summary domain.Summary, paymentID string, at time.Time) api.OrderV1 {
	out := api.OrderV1{
		OrderID: summary.OrderID.String(), BuyerID: summary.BuyerID.String(), PaymentID: paymentID, Currency: string(summary.Currency),
		Subtotal: summary.Subtotal.Amount(), Discount: summary.Discount.Amount(), Shipping: summary.Shipping.Amount(),
		Total: summary.Total.Amount(), PromoCode: summary.PromoCode, OccurredAt: at.UTC(),
		Items: make([]api.ItemV1, 0, len(summary.Items)), Parts: make([]api.PartV1, 0, len(summary.Parts)),
	}
	for _, item := range summary.Items {
		out.Items = append(out.Items, api.ItemV1{
			SKU: item.SKU, ProductID: item.ProductID, CategoryID: item.CategoryID, SellerID: item.SellerID.String(),
			Title: item.Title, Quantity: item.Quantity, UnitPrice: item.UnitPrice.Amount(), Subtotal: item.Base.Amount(),
			Total: item.Final.Amount(),
		})
	}
	for _, part := range summary.Parts {
		out.Parts = append(out.Parts, api.PartV1{
			SellerID: part.SellerID.String(), Subtotal: part.Subtotal.Amount(), Discount: part.Discount.Amount(),
			Shipping: part.Shipping.Amount(), Total: part.Total.Amount(),
		})
	}
	return out
}
