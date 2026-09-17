package domain

import (
	"fmt"
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type ItemSnapshot struct {
	SKU        string
	ProductID  string
	CategoryID string
	SellerID   string
	Title      string
	Quantity   int
	UnitPrice  int64
	Base       int64
	Final      int64
}

type PartSnapshot struct {
	SellerID string
	Subtotal int64
	Discount int64
	Shipping int64
	Total    int64
}

type ChangeSnapshot struct {
	From      string
	To        string
	ActorKind string
	ActorID   string
	Reason    string
	At        time.Time
}

type OrderSnapshot struct {
	ID             string
	BuyerID        string
	Status         string
	Address        Address
	DeliveryMethod string
	PromoCode      string
	Currency       string
	Subtotal       int64
	Discount       int64
	Shipping       int64
	Total          int64
	PaymentID      string
	Items          []ItemSnapshot
	Parts          []PartSnapshot
	History        []ChangeSnapshot
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeliveredAt    time.Time
	Version        int
}

func (o *Order) Snapshot() OrderSnapshot {
	snap := OrderSnapshot{
		ID: o.id.String(), BuyerID: o.buyerID.String(), Status: string(o.status), Address: o.address,
		DeliveryMethod: o.deliveryMethod, PromoCode: o.promoCode, Currency: string(o.total.Currency()),
		Subtotal: o.subtotal.Amount(), Discount: o.discount.Amount(), Shipping: o.shipping.Amount(), Total: o.total.Amount(),
		PaymentID: o.paymentID, CreatedAt: o.createdAt, UpdatedAt: o.updatedAt, DeliveredAt: o.deliveredAt, Version: o.version,
		Items: make([]ItemSnapshot, 0, len(o.items)), Parts: make([]PartSnapshot, 0, len(o.parts)),
		History: make([]ChangeSnapshot, 0, len(o.history)),
	}
	for _, item := range o.items {
		snap.Items = append(snap.Items, ItemSnapshot{
			SKU: item.SKU, ProductID: item.ProductID, CategoryID: item.CategoryID, SellerID: item.SellerID.String(),
			Title: item.Title, Quantity: item.Quantity, UnitPrice: item.UnitPrice.Amount(), Base: item.Base.Amount(),
			Final: item.Final.Amount(),
		})
	}
	for _, part := range o.parts {
		snap.Parts = append(snap.Parts, PartSnapshot{
			SellerID: part.SellerID.String(), Subtotal: part.Subtotal.Amount(), Discount: part.Discount.Amount(),
			Shipping: part.Shipping.Amount(), Total: part.Total.Amount(),
		})
	}
	for _, change := range o.history {
		snap.History = append(snap.History, ChangeSnapshot{
			From: string(change.From), To: string(change.To), ActorKind: string(change.Actor.Kind), ActorID: change.Actor.ID,
			Reason: change.Reason, At: change.At,
		})
	}
	return snap
}

func RehydrateOrder(s OrderSnapshot) (*Order, error) {
	id, err := ParseOrderID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate order: %w", err)
	}
	buyer, err := kernel.ParseUserID(s.BuyerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate order %s buyer: %w", s.ID, err)
	}
	currency, err := kernel.NewCurrency(s.Currency)
	if err != nil {
		return nil, fmt.Errorf("rehydrate order %s currency: %w", s.ID, err)
	}
	status, known := ParseStatus(s.Status)
	if !known {
		return nil, fmt.Errorf("rehydrate order %s: unknown status %q", s.ID, s.Status)
	}
	money := func(amount int64) kernel.Money { return kernel.MustMoney(amount, currency) }
	o := &Order{
		id: id, buyerID: buyer, status: status, address: s.Address, deliveryMethod: s.DeliveryMethod, promoCode: s.PromoCode,
		subtotal: money(s.Subtotal), discount: money(s.Discount), shipping: money(s.Shipping), total: money(s.Total),
		paymentID: s.PaymentID, createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, deliveredAt: s.DeliveredAt, version: s.Version,
		items: make([]Item, 0, len(s.Items)), parts: make([]Part, 0, len(s.Parts)), history: make([]StatusChange, 0, len(s.History)),
	}
	for _, item := range s.Items {
		seller, err := kernel.ParseSellerID(item.SellerID)
		if err != nil {
			return nil, fmt.Errorf("rehydrate order %s item seller: %w", s.ID, err)
		}
		o.items = append(o.items, Item{
			SKU: item.SKU, ProductID: item.ProductID, CategoryID: item.CategoryID, SellerID: seller, Title: item.Title,
			Quantity: item.Quantity, UnitPrice: money(item.UnitPrice), Base: money(item.Base), Final: money(item.Final),
		})
	}
	for _, part := range s.Parts {
		seller, err := kernel.ParseSellerID(part.SellerID)
		if err != nil {
			return nil, fmt.Errorf("rehydrate order %s part seller: %w", s.ID, err)
		}
		o.parts = append(o.parts, Part{
			SellerID: seller, Subtotal: money(part.Subtotal), Discount: money(part.Discount),
			Shipping: money(part.Shipping), Total: money(part.Total),
		})
	}
	for _, change := range s.History {
		o.history = append(o.history, StatusChange{
			From: Status(change.From), To: Status(change.To), Actor: Actor{Kind: ActorKind(change.ActorKind), ID: change.ActorID},
			Reason: change.Reason, At: change.At,
		})
	}
	return o, nil
}

type SagaSnapshot struct {
	OrderID       string
	BuyerID       string
	ReservationID string
	PaymentID     string
	RefundID      string
	PromoCode     string
	Currency      string
	Amount        int64
	Status        string
	Step          string
	Captured      bool
	Compensated   []string
	Reason        string
	LastError     string
	Attempts      int
	Deadline      time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Version       int
}

func (s *CheckoutSaga) Snapshot() SagaSnapshot {
	snap := SagaSnapshot{
		OrderID: s.orderID.String(), BuyerID: s.buyerID.String(), ReservationID: s.reservationID, PaymentID: s.paymentID,
		RefundID: s.refundID, PromoCode: s.promoCode, Currency: string(s.amount.Currency()), Amount: s.amount.Amount(),
		Status: string(s.status), Step: string(s.step), Captured: s.captured, Reason: s.reason, LastError: s.lastError,
		Attempts: s.attempts, Deadline: s.deadline, CreatedAt: s.createdAt, UpdatedAt: s.updatedAt, Version: s.version,
		Compensated: make([]string, 0, len(s.compensated)),
	}
	for _, step := range s.compensated {
		snap.Compensated = append(snap.Compensated, string(step))
	}
	return snap
}

func RehydrateSaga(s SagaSnapshot) (*CheckoutSaga, error) {
	id, err := ParseOrderID(s.OrderID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate saga: %w", err)
	}
	buyer, err := kernel.ParseUserID(s.BuyerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate saga %s buyer: %w", s.OrderID, err)
	}
	currency, err := kernel.NewCurrency(s.Currency)
	if err != nil {
		return nil, fmt.Errorf("rehydrate saga %s currency: %w", s.OrderID, err)
	}
	status := SagaStatus(s.Status)
	if !slices.Contains([]SagaStatus{SagaRunning, SagaCompensating, SagaCompleted, SagaCompensated, SagaManual}, status) {
		return nil, fmt.Errorf("rehydrate saga %s: unknown status %q", s.OrderID, s.Status)
	}
	saga := &CheckoutSaga{
		orderID: id, buyerID: buyer, reservationID: s.ReservationID, paymentID: s.PaymentID, refundID: s.RefundID,
		promoCode: s.PromoCode, amount: kernel.MustMoney(s.Amount, currency), status: status, step: Step(s.Step),
		captured: s.Captured, reason: s.Reason, lastError: s.LastError, attempts: s.Attempts, deadline: s.Deadline,
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version, compensated: make([]Step, 0, len(s.Compensated)),
	}
	for _, step := range s.Compensated {
		saga.compensated = append(saga.compensated, Step(step))
	}
	return saga, nil
}
