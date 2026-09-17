package domain

import (
	"fmt"
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type RefundSnapshot struct {
	ID               string
	Amount           int64
	Reason           string
	Status           string
	ProviderRefundID string
	CreatedAt        time.Time
	CompletedAt      time.Time
}

type PaymentSnapshot struct {
	ID                string
	OrderID           string
	BuyerID           string
	Provider          string
	ProviderPaymentID string
	RedirectURL       string
	MethodID          string
	SaveMethod        bool
	Status            string
	Currency          string
	Amount            int64
	Authorized        int64
	Captured          int64
	Refunded          int64
	FailureReason     string
	Refunds           []RefundSnapshot
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Version           int
}

func (p *Payment) Snapshot() PaymentSnapshot {
	snap := PaymentSnapshot{
		ID: p.id.String(), OrderID: p.orderID.String(), BuyerID: p.buyerID.String(), Provider: p.provider,
		ProviderPaymentID: p.providerPaymentID, RedirectURL: p.redirectURL, SaveMethod: p.saveMethod,
		Status: string(p.status), Currency: string(p.amount.Currency()), Amount: p.amount.Amount(),
		Authorized: p.authorized.Amount(), Captured: p.captured.Amount(), Refunded: p.refunded.Amount(),
		FailureReason: p.failureReason, CreatedAt: p.createdAt, UpdatedAt: p.updatedAt, Version: p.version,
		Refunds: make([]RefundSnapshot, 0, len(p.refunds)),
	}
	if !p.methodID.IsZero() {
		snap.MethodID = p.methodID.String()
	}
	for _, refund := range p.refunds {
		snap.Refunds = append(snap.Refunds, RefundSnapshot{
			ID: refund.id.String(), Amount: refund.amount.Amount(), Reason: refund.reason, Status: string(refund.status),
			ProviderRefundID: refund.providerRefundID, CreatedAt: refund.createdAt, CompletedAt: refund.completedAt,
		})
	}
	return snap
}

func RehydratePayment(s PaymentSnapshot) (*Payment, error) {
	id, err := ParsePaymentID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate payment: %w", err)
	}
	order, err := ParseOrderID(s.OrderID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate payment %s order: %w", s.ID, err)
	}
	buyer, err := kernel.ParseUserID(s.BuyerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate payment %s buyer: %w", s.ID, err)
	}
	currency, err := kernel.NewCurrency(s.Currency)
	if err != nil {
		return nil, fmt.Errorf("rehydrate payment %s currency: %w", s.ID, err)
	}
	amounts := make([]kernel.Money, 4)
	for i, raw := range []int64{s.Amount, s.Authorized, s.Captured, s.Refunded} {
		if amounts[i], err = kernel.NewMoney(raw, currency); err != nil {
			return nil, fmt.Errorf("rehydrate payment %s amount: %w", s.ID, err)
		}
	}
	status := Status(s.Status)
	if !slices.Contains([]Status{StatusCreated, StatusPending, StatusAuthorized, StatusCaptured, StatusFailed, StatusCancelled, StatusRefunded}, status) {
		return nil, fmt.Errorf("rehydrate payment %s: unknown status %q", s.ID, s.Status)
	}
	p := &Payment{
		id: id, orderID: order, buyerID: buyer, provider: s.Provider, providerPaymentID: s.ProviderPaymentID,
		redirectURL: s.RedirectURL, saveMethod: s.SaveMethod, status: status,
		amount: amounts[0], authorized: amounts[1], captured: amounts[2], refunded: amounts[3],
		failureReason: s.FailureReason, createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version,
		refunds: make([]Refund, 0, len(s.Refunds)),
	}
	if s.MethodID != "" {
		if p.methodID, err = ParseMethodID(s.MethodID); err != nil {
			return nil, fmt.Errorf("rehydrate payment %s method: %w", s.ID, err)
		}
	}
	for _, snap := range s.Refunds {
		refundID, err := ParseRefundID(snap.ID)
		if err != nil {
			return nil, fmt.Errorf("rehydrate payment %s refund: %w", s.ID, err)
		}
		amount, err := kernel.NewMoney(snap.Amount, currency)
		if err != nil {
			return nil, fmt.Errorf("rehydrate payment %s refund amount: %w", s.ID, err)
		}
		p.refunds = append(p.refunds, Refund{
			id: refundID, amount: amount, reason: snap.Reason, status: RefundStatus(snap.Status),
			providerRefundID: snap.ProviderRefundID, createdAt: snap.CreatedAt, completedAt: snap.CompletedAt,
		})
	}
	return p, nil
}
