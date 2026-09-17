package query

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

var (
	ErrUnknownStatus = kernel.Validation("ORDER_UNKNOWN_STATUS", "status filter is not recognized")
	ErrNotMember     = kernel.NotFound("ORDER_SELLER_NOT_FOUND", "seller not found")
)

type ItemView struct {
	SKU       string
	ProductID string
	SellerID  string
	Title     string
	Quantity  int
	UnitPrice int64
	Subtotal  int64
	Discount  int64
	Total     int64
}

type PartView struct {
	SellerID string
	Subtotal int64
	Discount int64
	Shipping int64
	Total    int64
}

type ChangeView struct {
	From      string
	To        string
	ActorKind string
	Reason    string
	At        time.Time
}

type PaymentView struct {
	PaymentID   string
	Status      string
	RedirectURL string
	PayBefore   time.Time
}

type OrderView struct {
	ID             string
	BuyerID        string
	Status         string
	Address        domain.Address
	DeliveryMethod string
	PromoCode      string
	Currency       string
	Subtotal       int64
	Discount       int64
	Shipping       int64
	Total          int64
	Items          []ItemView
	Parts          []PartView
	History        []ChangeView
	Payment        *PaymentView
	Cancellable    bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type OrderSummaryView struct {
	ID         string
	Status     string
	Currency   string
	Total      int64
	ItemsCount int
	CreatedAt  time.Time
}

type SellerOrderView struct {
	ID        string
	Status    string
	Currency  string
	Items     []ItemView
	Part      PartView
	CreatedAt time.Time
}

type SagaView struct {
	OrderID       string
	BuyerID       string
	Status        string
	Step          string
	ReservationID string
	PaymentID     string
	Reason        string
	LastError     string
	Attempts      int
	Compensated   []string
	Deadline      time.Time
	UpdatedAt     time.Time
}

type SagaState struct {
	Status    string
	Step      string
	PaymentID string
	Deadline  time.Time
}

type ReadModel interface {
	Order(ctx context.Context, id string) (domain.OrderSnapshot, SagaState, error)
	BuyerOrders(ctx context.Context, buyerID, status string, limit int, after *pagination.Keyset) (pagination.Page[OrderSummaryView], error)
	SellerOrders(ctx context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[SellerOrderView], error)
	Sagas(ctx context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[SagaView], error)
}

func NewItemView(item domain.ItemSnapshot) ItemView {
	return ItemView{
		SKU: item.SKU, ProductID: item.ProductID, SellerID: item.SellerID, Title: item.Title, Quantity: item.Quantity,
		UnitPrice: item.UnitPrice, Subtotal: item.Base, Discount: item.Base - item.Final, Total: item.Final,
	}
}

func NewOrderView(snap domain.OrderSnapshot) OrderView {
	status, _ := domain.ParseStatus(snap.Status)
	view := OrderView{
		ID: snap.ID, BuyerID: snap.BuyerID, Status: snap.Status, Address: snap.Address, DeliveryMethod: snap.DeliveryMethod,
		PromoCode: snap.PromoCode, Currency: snap.Currency, Subtotal: snap.Subtotal, Discount: snap.Discount,
		Shipping: snap.Shipping, Total: snap.Total, Cancellable: status.Cancellable(), CreatedAt: snap.CreatedAt,
		UpdatedAt: snap.UpdatedAt, Items: make([]ItemView, 0, len(snap.Items)), Parts: make([]PartView, 0, len(snap.Parts)),
		History: make([]ChangeView, 0, len(snap.History)),
	}
	for _, item := range snap.Items {
		view.Items = append(view.Items, NewItemView(item))
	}
	for _, part := range snap.Parts {
		view.Parts = append(view.Parts, PartView(part))
	}
	for _, change := range snap.History {
		view.History = append(view.History, ChangeView{From: change.From, To: change.To, ActorKind: change.ActorKind, Reason: change.Reason, At: change.At})
	}
	return view
}

type GetOrder struct {
	Actor   auth.Principal
	OrderID string
}

type GetOrderHandler struct {
	reader   ReadModel
	payments application.Payments
}

func NewGetOrderHandler(reader ReadModel, payments application.Payments) *GetOrderHandler {
	return &GetOrderHandler{reader: reader, payments: payments}
}

func (h *GetOrderHandler) Handle(ctx context.Context, q GetOrder) (OrderView, error) {
	if q.Actor.UserID == "" {
		return OrderView{}, auth.ErrUnauthenticated
	}
	if _, err := domain.ParseOrderID(q.OrderID); err != nil {
		return OrderView{}, domain.ErrOrderNotFound
	}
	snap, saga, err := h.reader.Order(ctx, q.OrderID)
	if err != nil {
		return OrderView{}, err
	}
	if snap.BuyerID != q.Actor.UserID && !identity.Can(q.Actor, identity.PermOrdersSupport) {
		return OrderView{}, domain.ErrOrderNotFound
	}
	view := NewOrderView(snap)
	committing := saga.Step == string(domain.StepCommittingStock) || saga.Step == string(domain.StepStockCommitted) ||
		saga.Step == string(domain.StepPaymentCaptured)
	view.Cancellable = view.Cancellable && (saga.Status != string(domain.SagaRunning) || !committing)
	if snap.PaymentID == "" {
		return view, nil
	}
	view.Payment = &PaymentView{PaymentID: snap.PaymentID}
	if snap.Status == string(domain.StatusAwaitingPayment) {
		view.Payment.PayBefore = saga.Deadline
	}
	info, err := h.payments.Info(ctx, snap.PaymentID)
	if err != nil {
		return OrderView{}, err
	}
	view.Payment.Status = info.Status
	if info.Status == paymentapi.StatusPending && snap.Status == string(domain.StatusAwaitingPayment) {
		view.Payment.RedirectURL = info.RedirectURL
	}
	return view, nil
}

type ListOrders struct {
	Actor  auth.Principal
	Status string
	Limit  int
	Cursor string
}

type ListOrdersHandler struct {
	reader ReadModel
}

func NewListOrdersHandler(reader ReadModel) *ListOrdersHandler {
	return &ListOrdersHandler{reader: reader}
}

func (h *ListOrdersHandler) Handle(ctx context.Context, q ListOrders) (pagination.Page[OrderSummaryView], error) {
	if _, err := kernel.ParseUserID(q.Actor.UserID); err != nil {
		return pagination.Page[OrderSummaryView]{}, auth.ErrUnauthenticated
	}
	after, err := page(q.Status, q.Cursor)
	if err != nil {
		return pagination.Page[OrderSummaryView]{}, err
	}
	return h.reader.BuyerOrders(ctx, q.Actor.UserID, q.Status, pagination.NormalizeLimit(q.Limit), after)
}

type ListSellerOrders struct {
	Actor    auth.Principal
	SellerID string
	Status   string
	Limit    int
	Cursor   string
}

type ListSellerOrdersHandler struct {
	reader  ReadModel
	sellers application.SellerMembership
}

func NewListSellerOrdersHandler(reader ReadModel, sellers application.SellerMembership) *ListSellerOrdersHandler {
	return &ListSellerOrdersHandler{reader: reader, sellers: sellers}
}

func (h *ListSellerOrdersHandler) Handle(ctx context.Context, q ListSellerOrders) (pagination.Page[SellerOrderView], error) {
	if q.Actor.UserID == "" {
		return pagination.Page[SellerOrderView]{}, auth.ErrUnauthenticated
	}
	if _, err := kernel.ParseSellerID(q.SellerID); err != nil {
		return pagination.Page[SellerOrderView]{}, ErrNotMember
	}
	_, member, err := h.sellers.MemberRole(ctx, q.SellerID, q.Actor.UserID)
	if err != nil {
		return pagination.Page[SellerOrderView]{}, err
	}
	if !member && !identity.Can(q.Actor, identity.PermOrdersSupport) {
		return pagination.Page[SellerOrderView]{}, ErrNotMember
	}
	after, err := page(q.Status, q.Cursor)
	if err != nil {
		return pagination.Page[SellerOrderView]{}, err
	}
	if status, _ := domain.ParseStatus(q.Status); q.Status != "" && !status.VisibleToSeller() {
		return pagination.Page[SellerOrderView]{Items: []SellerOrderView{}}, nil
	}
	return h.reader.SellerOrders(ctx, q.SellerID, q.Status, pagination.NormalizeLimit(q.Limit), after)
}

type ListSagas struct {
	Actor  auth.Principal
	Status string
	Limit  int
	Cursor string
}

type ListSagasHandler struct {
	reader ReadModel
}

func NewListSagasHandler(reader ReadModel) *ListSagasHandler {
	return &ListSagasHandler{reader: reader}
}

func (h *ListSagasHandler) Handle(ctx context.Context, q ListSagas) (pagination.Page[SagaView], error) {
	if err := identity.Authorize(q.Actor, identity.PermOrdersSupport); err != nil {
		return pagination.Page[SagaView]{}, err
	}
	switch domain.SagaStatus(q.Status) {
	case "", domain.SagaRunning, domain.SagaCompensating, domain.SagaCompleted, domain.SagaCompensated, domain.SagaManual:
	default:
		return pagination.Page[SagaView]{}, ErrUnknownStatus.WithDetail("%q", q.Status)
	}
	keyset, ok, err := pagination.DecodeKeyset(q.Cursor)
	if err != nil {
		return pagination.Page[SagaView]{}, err
	}
	var after *pagination.Keyset
	if ok {
		after = &keyset
	}
	return h.reader.Sagas(ctx, q.Status, pagination.NormalizeLimit(q.Limit), after)
}

func page(status, cursor string) (*pagination.Keyset, error) {
	if _, known := domain.ParseStatus(status); status != "" && !known {
		return nil, ErrUnknownStatus.WithDetail("%q", status)
	}
	keyset, ok, err := pagination.DecodeKeyset(cursor)
	if err != nil || !ok {
		return nil, err
	}
	return &keyset, nil
}
