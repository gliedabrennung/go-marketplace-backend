package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/query"
)

type addressDTO struct {
	Recipient  string `json:"recipient"`
	Phone      string `json:"phone"`
	Country    string `json:"country,omitempty"`
	City       string `json:"city"`
	Line       string `json:"line"`
	PostalCode string `json:"postal_code,omitempty"`
}

type paymentRequest struct {
	MethodID   string `json:"method_id"`
	SaveMethod bool   `json:"save_method"`
}

type placeRequest struct {
	Address        addressDTO     `json:"address"`
	DeliveryMethod string         `json:"delivery_method"`
	Payment        paymentRequest `json:"payment"`
	ExpectedTotal  int64          `json:"expected_total"`
}

type cancelRequest struct {
	Reason string `json:"reason"`
}

type placedDTO struct {
	OrderID    string `json:"order_id"`
	Status     string `json:"status"`
	PaymentID  string `json:"payment_id"`
	PaymentURL string `json:"payment_url"`
	Total      int64  `json:"total"`
	Currency   string `json:"currency"`
}

func placedResponse(result command.PlaceOrderResult) placedDTO {
	return placedDTO(result)
}

type retryDTO struct {
	OrderID    string `json:"order_id"`
	PaymentID  string `json:"payment_id"`
	PaymentURL string `json:"payment_url"`
}

func retryResponse(result command.RetryPaymentResult) retryDTO {
	return retryDTO(result)
}

type itemDTO struct {
	SKU       string `json:"sku"`
	ProductID string `json:"product_id"`
	SellerID  string `json:"seller_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Subtotal  int64  `json:"subtotal"`
	Discount  int64  `json:"discount"`
	Total     int64  `json:"total"`
}

type partDTO struct {
	SellerID string `json:"seller_id"`
	Subtotal int64  `json:"subtotal"`
	Discount int64  `json:"discount"`
	Shipping int64  `json:"shipping"`
	Total    int64  `json:"total"`
}

type changeDTO struct {
	From   string    `json:"from,omitempty"`
	To     string    `json:"to"`
	Actor  string    `json:"actor"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

type paymentDTO struct {
	PaymentID   string     `json:"payment_id"`
	Status      string     `json:"status"`
	RedirectURL string     `json:"redirect_url,omitempty"`
	PayBefore   *time.Time `json:"pay_before,omitempty"`
}

type orderDTO struct {
	ID             string      `json:"id"`
	Status         string      `json:"status"`
	Address        addressDTO  `json:"address"`
	DeliveryMethod string      `json:"delivery_method"`
	PromoCode      string      `json:"promo_code,omitempty"`
	Currency       string      `json:"currency"`
	Subtotal       int64       `json:"subtotal"`
	Discount       int64       `json:"discount"`
	Shipping       int64       `json:"shipping"`
	Total          int64       `json:"total"`
	Items          []itemDTO   `json:"items"`
	Parts          []partDTO   `json:"parts"`
	History        []changeDTO `json:"history"`
	Payment        *paymentDTO `json:"payment,omitempty"`
	Cancellable    bool        `json:"cancellable"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

type summaryDTO struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Currency   string    `json:"currency"`
	Total      int64     `json:"total"`
	ItemsCount int       `json:"items_count"`
	CreatedAt  time.Time `json:"created_at"`
}

type sellerOrderDTO struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Currency  string    `json:"currency"`
	Items     []itemDTO `json:"items"`
	Part      partDTO   `json:"part"`
	CreatedAt time.Time `json:"created_at"`
}

type sagaDTO struct {
	OrderID       string    `json:"order_id"`
	BuyerID       string    `json:"buyer_id"`
	Status        string    `json:"status"`
	Step          string    `json:"step"`
	ReservationID string    `json:"reservation_id"`
	PaymentID     string    `json:"payment_id"`
	Reason        string    `json:"reason,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	Attempts      int       `json:"attempts"`
	Compensated   []string  `json:"compensated"`
	Deadline      time.Time `json:"deadline"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toItems(views []query.ItemView) []itemDTO {
	out := make([]itemDTO, 0, len(views))
	for _, view := range views {
		out = append(out, itemDTO(view))
	}
	return out
}

func toOrder(view query.OrderView) orderDTO {
	out := orderDTO{
		ID: view.ID, Status: view.Status, DeliveryMethod: view.DeliveryMethod, PromoCode: view.PromoCode, Currency: view.Currency,
		Subtotal: view.Subtotal, Discount: view.Discount, Shipping: view.Shipping, Total: view.Total, Items: toItems(view.Items),
		Cancellable: view.Cancellable, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
		Address: addressDTO(view.Address), Parts: make([]partDTO, 0, len(view.Parts)), History: make([]changeDTO, 0, len(view.History)),
	}
	for _, part := range view.Parts {
		out.Parts = append(out.Parts, partDTO(part))
	}
	for _, change := range view.History {
		out.History = append(out.History, changeDTO{From: change.From, To: change.To, Actor: change.ActorKind, Reason: change.Reason, At: change.At})
	}
	if view.Payment != nil {
		out.Payment = &paymentDTO{PaymentID: view.Payment.PaymentID, Status: view.Payment.Status, RedirectURL: view.Payment.RedirectURL}
		if !view.Payment.PayBefore.IsZero() {
			payBefore := view.Payment.PayBefore
			out.Payment.PayBefore = &payBefore
		}
	}
	return out
}

func toSummary(view query.OrderSummaryView) summaryDTO {
	return summaryDTO(view)
}

func toSellerOrder(view query.SellerOrderView) sellerOrderDTO {
	return sellerOrderDTO{
		ID: view.ID, Status: view.Status, Currency: view.Currency, Items: toItems(view.Items), Part: partDTO(view.Part), CreatedAt: view.CreatedAt,
	}
}

func toSaga(view query.SagaView) sagaDTO {
	return sagaDTO(view)
}
