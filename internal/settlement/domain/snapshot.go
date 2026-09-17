package domain

import (
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type EntrySnapshot struct {
	ID         string
	OrderID    string
	SellerID   string
	Currency   string
	Gross      int64
	Commission int64
	Net        int64
	AccruedAt  time.Time
}

func (e *Entry) Snapshot() EntrySnapshot {
	return EntrySnapshot{
		ID: e.id.String(), OrderID: e.orderID, SellerID: e.sellerID.String(), Currency: string(e.gross.Currency()),
		Gross: e.gross.Amount(), Commission: e.commission.Amount(), Net: e.net.Amount(), AccruedAt: e.accruedAt,
	}
}

func RehydrateEntry(s EntrySnapshot) (*Entry, error) {
	id, err := ParseEntryID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate settlement entry: %w", err)
	}
	seller, err := kernel.ParseSellerID(s.SellerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate settlement entry %s seller: %w", s.ID, err)
	}
	currency, err := kernel.NewCurrency(s.Currency)
	if err != nil {
		return nil, fmt.Errorf("rehydrate settlement entry %s currency: %w", s.ID, err)
	}
	return &Entry{
		id: id, orderID: s.OrderID, sellerID: seller, gross: kernel.MustMoney(s.Gross, currency),
		commission: kernel.MustMoney(s.Commission, currency), net: kernel.MustMoney(s.Net, currency), accruedAt: s.AccruedAt,
	}, nil
}
