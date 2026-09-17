package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/query"
)

type webhookResponse struct {
	Received  bool `json:"received"`
	Duplicate bool `json:"duplicate"`
}

type refundResponse struct {
	ID          string     `json:"id"`
	Amount      int64      `json:"amount"`
	Reason      string     `json:"reason,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type paymentResponse struct {
	ID            string           `json:"id"`
	OrderID       string           `json:"order_id"`
	Provider      string           `json:"provider"`
	Status        string           `json:"status"`
	Currency      string           `json:"currency"`
	Amount        int64            `json:"amount"`
	Authorized    int64            `json:"authorized"`
	Captured      int64            `json:"captured"`
	Refunded      int64            `json:"refunded"`
	RedirectURL   string           `json:"redirect_url,omitempty"`
	FailureReason string           `json:"failure_reason,omitempty"`
	Refunds       []refundResponse `json:"refunds"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type methodResponse struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

type methodsResponse struct {
	Items []methodResponse `json:"items"`
}

type mismatchResponse struct {
	PaymentID         string `json:"payment_id,omitempty"`
	ProviderPaymentID string `json:"provider_payment_id"`
	Field             string `json:"field"`
	Internal          string `json:"internal"`
	Provider          string `json:"provider"`
}

type reconciliationResponse struct {
	Day        string             `json:"day"`
	Provider   string             `json:"provider"`
	Checked    int                `json:"checked"`
	Mismatches []mismatchResponse `json:"mismatches"`
	CreatedAt  time.Time          `json:"created_at"`
}

func toPayment(view query.PaymentView) paymentResponse {
	out := paymentResponse{
		ID: view.ID, OrderID: view.OrderID, Provider: view.Provider, Status: view.Status, Currency: view.Currency,
		Amount: view.Amount, Authorized: view.Authorized, Captured: view.Captured, Refunded: view.Refunded,
		FailureReason: view.FailureReason, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
		Refunds: make([]refundResponse, 0, len(view.Refunds)),
	}
	if view.Status == "pending" {
		out.RedirectURL = view.RedirectURL
	}
	for _, refund := range view.Refunds {
		item := refundResponse{ID: refund.ID, Amount: refund.Amount, Reason: refund.Reason, Status: refund.Status, CreatedAt: refund.CreatedAt}
		if !refund.CompletedAt.IsZero() {
			completed := refund.CompletedAt
			item.CompletedAt = &completed
		}
		out.Refunds = append(out.Refunds, item)
	}
	return out
}

func toReconciliation(view query.ReconciliationView) reconciliationResponse {
	out := reconciliationResponse{
		Day: view.Day.Format(time.DateOnly), Provider: view.Provider, Checked: view.Checked, CreatedAt: view.CreatedAt,
		Mismatches: make([]mismatchResponse, 0, len(view.Mismatches)),
	}
	for _, m := range view.Mismatches {
		out.Mismatches = append(out.Mismatches, mismatchResponse(m))
	}
	return out
}
