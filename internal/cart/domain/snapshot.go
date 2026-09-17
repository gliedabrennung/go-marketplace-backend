package domain

import (
	"fmt"
	"time"
)

type ItemSnapshot struct {
	SKU       string
	SellerID  string
	Quantity  int
	Price     int64
	AddedAt   time.Time
	UpdatedAt time.Time
}

type CartSnapshot struct {
	ID        string
	OwnerKind string
	OwnerID   string
	Currency  string
	PromoCode string
	Items     []ItemSnapshot
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
	Version   int
}

func (c *Cart) Snapshot() CartSnapshot {
	snap := CartSnapshot{
		ID: c.id.String(), OwnerKind: string(c.owner.kind), OwnerID: c.owner.id, Currency: c.currency,
		PromoCode: c.promoCode, CreatedAt: c.createdAt, UpdatedAt: c.updatedAt, ExpiresAt: c.expiresAt,
		Version: c.version, Items: make([]ItemSnapshot, 0, len(c.items)),
	}
	for _, item := range c.items {
		snap.Items = append(snap.Items, ItemSnapshot{
			SKU: item.sku, SellerID: item.sellerID, Quantity: item.quantity, Price: item.price,
			AddedAt: item.addedAt, UpdatedAt: item.updatedAt,
		})
	}
	return snap
}

func Rehydrate(s CartSnapshot) (*Cart, error) {
	id, err := ParseCartID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate cart: %w", err)
	}
	owner, err := RehydrateOwner(s.OwnerKind, s.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate cart %s owner: %w", s.ID, err)
	}
	c := &Cart{
		id: id, owner: owner, currency: s.Currency, promoCode: s.PromoCode, createdAt: s.CreatedAt,
		updatedAt: s.UpdatedAt, expiresAt: s.ExpiresAt, version: s.Version, items: make([]Item, 0, len(s.Items)),
	}
	for _, item := range s.Items {
		c.items = append(c.items, Item{
			sku: item.SKU, sellerID: item.SellerID, quantity: item.Quantity, price: item.Price,
			addedAt: item.AddedAt, updatedAt: item.UpdatedAt,
		})
	}
	return c, nil
}
