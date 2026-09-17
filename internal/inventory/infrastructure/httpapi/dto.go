package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var ErrQuantityRequired = kernel.Validation("INVENTORY_QUANTITY_REQUIRED", "quantity is required")

type stockRequest struct {
	Quantity  *int   `json:"quantity"`
	Reference string `json:"reference"`
}

type stockResponse struct {
	SKU       string    `json:"sku"`
	SellerID  string    `json:"seller_id,omitempty"`
	Available int       `json:"available"`
	Reserved  int       `json:"reserved"`
	UpdatedAt time.Time `json:"updated_at"`
}

type movementResponse struct {
	SKU         string    `json:"sku"`
	Delta       int       `json:"delta"`
	Reason      string    `json:"reason"`
	ReferenceID string    `json:"reference_id"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type reservationLineResponse struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
	Status   string `json:"status"`
}

type reservationResponse struct {
	ReservationID string                    `json:"reservation_id"`
	OrderID       string                    `json:"order_id,omitempty"`
	Status        string                    `json:"status"`
	ExpiresAt     time.Time                 `json:"expires_at"`
	Lines         []reservationLineResponse `json:"lines"`
}

func toStock(view query.StockView) stockResponse {
	return stockResponse{
		SKU: view.SKU, SellerID: view.SellerID, Available: view.Available,
		Reserved: view.Reserved, UpdatedAt: view.UpdatedAt,
	}
}

func toMovement(view query.MovementView) movementResponse {
	return movementResponse{
		SKU: view.SKU, Delta: view.Delta, Reason: view.Reason,
		ReferenceID: view.ReferenceID, OccurredAt: view.OccurredAt,
	}
}

func toReservation(view query.ReservationView) reservationResponse {
	out := reservationResponse{
		ReservationID: view.ReservationID, OrderID: view.OrderID, Status: view.Status,
		ExpiresAt: view.ExpiresAt, Lines: make([]reservationLineResponse, 0, len(view.Lines)),
	}
	for _, line := range view.Lines {
		out.Lines = append(out.Lines, reservationLineResponse{SKU: line.SKU, Quantity: line.Quantity, Status: line.Status})
	}
	return out
}
