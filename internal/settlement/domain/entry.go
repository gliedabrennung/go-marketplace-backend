package domain

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Entry struct {
	id         EntryID
	orderID    string
	sellerID   kernel.SellerID
	gross      kernel.Money
	commission kernel.Money
	net        kernel.Money
	accruedAt  time.Time
}

func Accrue(id EntryID, orderID string, sellerID kernel.SellerID, gross, commission kernel.Money, now time.Time) (*Entry, error) {
	if id.IsZero() || orderID == "" || sellerID.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if gross.Amount() < 0 || commission.Amount() < 0 || commission.Currency() != gross.Currency() {
		return nil, ErrInvalidAmount
	}
	net, err := gross.Sub(commission)
	if err != nil || net.Amount() < 0 {
		return nil, ErrInvalidAmount
	}
	return &Entry{id: id, orderID: orderID, sellerID: sellerID, gross: gross, commission: commission, net: net, accruedAt: now}, nil
}

func (e *Entry) ID() EntryID { return e.id }

func (e *Entry) OrderID() string { return e.orderID }

func (e *Entry) SellerID() kernel.SellerID { return e.sellerID }

func (e *Entry) Gross() kernel.Money { return e.gross }

func (e *Entry) Commission() kernel.Money { return e.commission }

func (e *Entry) Net() kernel.Money { return e.net }

func (e *Entry) AccruedAt() time.Time { return e.accruedAt }
