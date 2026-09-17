package query

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

var ErrReportNotFound = kernel.NotFound("PAYMENT_RECONCILIATION_NOT_FOUND", "reconciliation report not found")

type RefundView struct {
	ID          string
	Amount      int64
	Reason      string
	Status      string
	CreatedAt   time.Time
	CompletedAt time.Time
}

type PaymentView struct {
	ID            string
	OrderID       string
	BuyerID       string
	Provider      string
	Status        string
	Currency      string
	Amount        int64
	Authorized    int64
	Captured      int64
	Refunded      int64
	RedirectURL   string
	FailureReason string
	Refunds       []RefundView
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type MethodView struct {
	ID        string
	Provider  string
	Label     string
	CreatedAt time.Time
}

type MismatchView struct {
	PaymentID         string
	ProviderPaymentID string
	Field             string
	Internal          string
	Provider          string
}

type ReconciliationView struct {
	Day        time.Time
	Provider   string
	Checked    int
	Mismatches []MismatchView
	CreatedAt  time.Time
}

type ReadModel interface {
	Payment(ctx context.Context, paymentID string) (PaymentView, error)
	OrderPayments(ctx context.Context, orderID string) ([]PaymentView, error)
	Methods(ctx context.Context, buyerID string) ([]MethodView, error)
	Reconciliation(ctx context.Context, provider string, day time.Time) (ReconciliationView, error)
}

func NewPaymentView(s domain.PaymentSnapshot) PaymentView {
	view := PaymentView{
		ID: s.ID, OrderID: s.OrderID, BuyerID: s.BuyerID, Provider: s.Provider, Status: s.Status, Currency: s.Currency,
		Amount: s.Amount, Authorized: s.Authorized, Captured: s.Captured, Refunded: s.Refunded,
		RedirectURL: s.RedirectURL, FailureReason: s.FailureReason, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		Refunds: make([]RefundView, 0, len(s.Refunds)),
	}
	for _, refund := range s.Refunds {
		view.Refunds = append(view.Refunds, RefundView{
			ID: refund.ID, Amount: refund.Amount, Reason: refund.Reason, Status: refund.Status,
			CreatedAt: refund.CreatedAt, CompletedAt: refund.CompletedAt,
		})
	}
	return view
}

func NewReconciliationView(report application.ReconciliationReport) ReconciliationView {
	view := ReconciliationView{
		Day: report.Day, Provider: report.Provider, Checked: report.Checked, CreatedAt: report.CreatedAt,
		Mismatches: make([]MismatchView, 0, len(report.Mismatches)),
	}
	for _, m := range report.Mismatches {
		view.Mismatches = append(view.Mismatches, MismatchView(m))
	}
	return view
}

type GetPayment struct {
	Actor     auth.Principal
	PaymentID string
}

type GetPaymentHandler struct {
	reader ReadModel
}

func NewGetPaymentHandler(reader ReadModel) *GetPaymentHandler {
	return &GetPaymentHandler{reader: reader}
}

func (h *GetPaymentHandler) Handle(ctx context.Context, q GetPayment) (PaymentView, error) {
	if q.Actor.UserID == "" {
		return PaymentView{}, auth.ErrUnauthenticated
	}
	if _, err := domain.ParsePaymentID(q.PaymentID); err != nil {
		return PaymentView{}, domain.ErrPaymentNotFound
	}
	view, err := h.reader.Payment(ctx, q.PaymentID)
	if err != nil {
		return PaymentView{}, err
	}
	if view.BuyerID != q.Actor.UserID && !identity.Can(q.Actor, identity.PermOrdersSupport) {
		return PaymentView{}, domain.ErrPaymentNotFound
	}
	return view, nil
}

type ListSavedMethods struct {
	Actor auth.Principal
}

type ListSavedMethodsHandler struct {
	reader ReadModel
}

func NewListSavedMethodsHandler(reader ReadModel) *ListSavedMethodsHandler {
	return &ListSavedMethodsHandler{reader: reader}
}

func (h *ListSavedMethodsHandler) Handle(ctx context.Context, q ListSavedMethods) ([]MethodView, error) {
	if _, err := kernel.ParseUserID(q.Actor.UserID); err != nil {
		return nil, auth.ErrUnauthenticated
	}
	return h.reader.Methods(ctx, q.Actor.UserID)
}

type GetReconciliation struct {
	Actor    auth.Principal
	Provider string
	Day      time.Time
}

type GetReconciliationHandler struct {
	reader ReadModel
}

func NewGetReconciliationHandler(reader ReadModel) *GetReconciliationHandler {
	return &GetReconciliationHandler{reader: reader}
}

func (h *GetReconciliationHandler) Handle(ctx context.Context, q GetReconciliation) (ReconciliationView, error) {
	if err := identity.Authorize(q.Actor, identity.PermRefundsInitiate); err != nil {
		return ReconciliationView{}, err
	}
	return h.reader.Reconciliation(ctx, q.Provider, q.Day)
}
