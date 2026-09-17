package domain

import (
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type OfferPrice struct {
	sku       SKU
	productID ProductID
	sellerID  kernel.SellerID
	price     kernel.Money
	compareAt kernel.Money
	active    bool
	updatedAt time.Time
	version   int

	events kernel.EventBuffer
}

func NewOfferPrice(sku SKU, product ProductID, seller kernel.SellerID, price kernel.Money, now time.Time) (*OfferPrice, error) {
	if sku.IsZero() || product.IsZero() || seller.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if price.Amount() <= 0 {
		return nil, ErrInvalidPrice
	}
	offer := &OfferPrice{sku: sku, productID: product, sellerID: seller, price: price, active: true, updatedAt: now}
	offer.record(now)
	return offer, nil
}

func (o *OfferPrice) ChangePrice(price kernel.Money, now time.Time) error {
	if price.Amount() <= 0 {
		return ErrInvalidPrice
	}
	if o.price.Equals(price) {
		return nil
	}
	o.price = price
	if !o.compareAt.IsZero() {
		if compare, err := o.compareAt.Compare(price); err != nil || compare <= 0 {
			o.compareAt = kernel.Money{}
		}
	}
	o.updatedAt = now
	o.record(now)
	return nil
}

func (o *OfferPrice) SetCompareAt(compareAt kernel.Money, now time.Time) error {
	if compareAt.IsZero() {
		o.compareAt = kernel.Money{}
		o.updatedAt = now
		return nil
	}
	compare, err := compareAt.Compare(o.price)
	if err != nil {
		return ErrCurrencyMismatch
	}
	if compare <= 0 {
		return ErrInvalidPrice.WithDetail("compare-at price must be greater than the price")
	}
	o.compareAt = compareAt
	o.updatedAt = now
	o.record(now)
	return nil
}

func (o *OfferPrice) SetActive(active bool, now time.Time) {
	if o.active == active {
		return
	}
	o.active, o.updatedAt = active, now
	o.record(now)
}

func (o *OfferPrice) Line(quantity kernel.Quantity, categories []CategoryID) (CartLine, error) {
	if !o.active {
		return CartLine{}, ErrOfferPriceInactive.WithDetail("sku %s", o.sku)
	}
	if quantity.IsZero() {
		return CartLine{}, ErrInvalidQuantity
	}
	return CartLine{
		SKU: o.sku, SellerID: o.sellerID, ProductID: o.productID,
		CategoryPath: categories, UnitPrice: o.price, Quantity: quantity,
	}, nil
}

func (o *OfferPrice) record(now time.Time) {
	o.events.Record(PriceChanged{
		SKU: o.sku.String(), SellerID: o.sellerID.String(), ProductID: o.productID.String(),
		Amount: o.price.Amount(), Currency: string(o.price.Currency()), Active: o.active, At: now,
	})
}

func (o *OfferPrice) SKU() SKU { return o.sku }

func (o *OfferPrice) SellerID() kernel.SellerID { return o.sellerID }

func (o *OfferPrice) ProductID() ProductID { return o.productID }

func (o *OfferPrice) Price() kernel.Money { return o.price }

func (o *OfferPrice) CompareAt() kernel.Money { return o.compareAt }

func (o *OfferPrice) IsActive() bool { return o.active }

func (o *OfferPrice) Version() int { return o.version }

func (o *OfferPrice) AdvanceVersion() { o.version++ }

func (o *OfferPrice) PullEvents() []kernel.DomainEvent { return o.events.Pull() }

type OfferPriceSnapshot struct {
	SKU       string
	ProductID string
	SellerID  string
	Amount    int64
	CompareAt int64
	Currency  string
	Active    bool
	UpdatedAt time.Time
	Version   int
}

func (o *OfferPrice) Snapshot() OfferPriceSnapshot {
	return OfferPriceSnapshot{
		SKU: o.sku.String(), ProductID: o.productID.String(), SellerID: o.sellerID.String(),
		Amount: o.price.Amount(), CompareAt: o.compareAt.Amount(), Currency: string(o.price.Currency()),
		Active: o.active, UpdatedAt: o.updatedAt, Version: o.version,
	}
}

func RehydrateOfferPrice(s OfferPriceSnapshot) (*OfferPrice, error) {
	sku, err := NewSKU(s.SKU)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer price: %w", err)
	}
	product, err := ParseProductID(s.ProductID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer price %s product: %w", s.SKU, err)
	}
	seller, err := kernel.ParseSellerID(s.SellerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer price %s seller: %w", s.SKU, err)
	}
	currency, err := kernel.NewCurrency(s.Currency)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer price %s currency: %w", s.SKU, err)
	}
	price, err := kernel.NewMoney(s.Amount, currency)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer price %s amount: %w", s.SKU, err)
	}
	offer := &OfferPrice{
		sku: sku, productID: product, sellerID: seller, price: price,
		active: s.Active, updatedAt: s.UpdatedAt, version: s.Version,
	}
	if s.CompareAt > 0 {
		if offer.compareAt, err = kernel.NewMoney(s.CompareAt, currency); err != nil {
			return nil, fmt.Errorf("rehydrate offer price %s compare-at: %w", s.SKU, err)
		}
	}
	return offer, nil
}
