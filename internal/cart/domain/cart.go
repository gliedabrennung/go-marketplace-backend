package domain

import (
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Item struct {
	sku       string
	sellerID  string
	quantity  int
	price     int64
	addedAt   time.Time
	updatedAt time.Time
}

func (i Item) SKU() string { return i.sku }

func (i Item) SellerID() string { return i.sellerID }

func (i Item) Quantity() int { return i.quantity }

func (i Item) Price() int64 { return i.price }

func (i Item) AddedAt() time.Time { return i.addedAt }

type Offer struct {
	SKU      string
	SellerID string
	Price    int64
	Currency string
}

type Cart struct {
	id        CartID
	owner     Owner
	currency  string
	items     []Item
	promoCode string
	createdAt time.Time
	updatedAt time.Time
	expiresAt time.Time
	version   int
}

func New(id CartID, owner Owner, limits Limits, now time.Time) (*Cart, error) {
	if id.IsZero() || owner.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	c := &Cart{id: id, owner: owner, createdAt: now}
	c.touch(limits, now)
	return c, nil
}

func (c *Cart) Add(offer Offer, quantity, available int, limits Limits, now time.Time) error {
	if err := c.accept(offer); err != nil {
		return err
	}
	index := c.index(offer.SKU)
	total := quantity
	if index >= 0 {
		total += c.items[index].quantity
	} else if len(c.items) >= limits.MaxItems {
		return ErrTooManyItems.WithDetail("limit %d", limits.MaxItems)
	}
	if err := checkQuantity(quantity, total, available, limits); err != nil {
		return err
	}
	if index >= 0 {
		item := &c.items[index]
		item.quantity, item.price, item.sellerID, item.updatedAt = total, offer.Price, offer.SellerID, now
	} else {
		c.items = append(c.items, Item{
			sku: offer.SKU, sellerID: offer.SellerID, quantity: total, price: offer.Price, addedAt: now, updatedAt: now,
		})
	}
	c.currency = offer.Currency
	c.touch(limits, now)
	return nil
}

func (c *Cart) SetQuantity(offer Offer, quantity, available int, limits Limits, now time.Time) error {
	index := c.index(offer.SKU)
	if index < 0 {
		return ErrItemNotFound
	}
	if quantity == 0 {
		return c.Remove(offer.SKU, limits, now)
	}
	if err := c.accept(offer); err != nil {
		return err
	}
	if err := checkQuantity(quantity, quantity, available, limits); err != nil {
		return err
	}
	item := &c.items[index]
	item.quantity, item.price, item.sellerID, item.updatedAt = quantity, offer.Price, offer.SellerID, now
	c.touch(limits, now)
	return nil
}

func (c *Cart) Remove(sku string, limits Limits, now time.Time) error {
	index := c.index(sku)
	if index < 0 {
		return ErrItemNotFound
	}
	c.items = slices.Delete(c.items, index, index+1)
	c.touch(limits, now)
	return nil
}

func (c *Cart) RemoveSKUs(skus []string, limits Limits, now time.Time) bool {
	before := len(c.items)
	c.items = slices.DeleteFunc(c.items, func(item Item) bool { return slices.Contains(skus, item.sku) })
	if len(c.items) == before {
		return false
	}
	c.touch(limits, now)
	return true
}

func (c *Cart) ApplyPromoCode(raw string, limits Limits, now time.Time) error {
	code, err := NormalizePromoCode(raw)
	if err != nil {
		return err
	}
	c.promoCode = code
	c.touch(limits, now)
	return nil
}

func (c *Cart) ClearPromoCode(limits Limits, now time.Time) {
	c.promoCode = ""
	c.touch(limits, now)
}

func (c *Cart) Absorb(other *Cart, available map[string]int, limits Limits, now time.Time) error {
	if other.id == c.id || other.owner == c.owner {
		return ErrSameOwner
	}
	if len(c.items) == 0 || c.currency == other.currency {
		for _, incoming := range other.items {
			c.absorbItem(incoming, available[incoming.sku], limits, now)
		}
		if c.currency == "" {
			c.currency = other.currency
		}
	}
	if c.promoCode == "" {
		c.promoCode = other.promoCode
	}
	c.touch(limits, now)
	return nil
}

func (c *Cart) absorbItem(incoming Item, available int, limits Limits, now time.Time) {
	index := c.index(incoming.sku)
	if index >= 0 {
		existing := &c.items[index]
		existing.quantity = max(existing.quantity, min(existing.quantity+incoming.quantity, available, limits.MaxQuantity))
		existing.updatedAt = now
		return
	}
	quantity := min(incoming.quantity, available, limits.MaxQuantity)
	if len(c.items) >= limits.MaxItems || quantity <= 0 {
		return
	}
	incoming.quantity, incoming.updatedAt = quantity, now
	c.items = append(c.items, incoming)
}

func (c *Cart) accept(offer Offer) error {
	if _, err := NormalizeSKU(offer.SKU); err != nil {
		return err
	}
	if offer.Price <= 0 {
		return ErrInvalidPrice
	}
	if c.currency != "" && len(c.items) > 0 && offer.Currency != c.currency {
		return ErrCurrencyMismatch.WithDetail("cart uses %s", c.currency)
	}
	return nil
}

func checkQuantity(requested, total, available int, limits Limits) error {
	if requested <= 0 || total > limits.MaxQuantity {
		return ErrInvalidQuantity.WithDetail("allowed 1-%d", limits.MaxQuantity)
	}
	if available <= 0 {
		return ErrOfferUnavailable
	}
	if total > available {
		return ErrExceedsStock.WithDetail("available %d", available)
	}
	return nil
}

func (c *Cart) index(sku string) int {
	return slices.IndexFunc(c.items, func(item Item) bool { return item.sku == sku })
}

func (c *Cart) touch(limits Limits, now time.Time) {
	c.updatedAt = now
	if c.owner.IsAnonymous() {
		c.expiresAt = now.Add(limits.AnonymousTTL)
	}
}

func (c *Cart) ID() CartID { return c.id }

func (c *Cart) Owner() Owner { return c.owner }

func (c *Cart) Currency() string { return c.currency }

func (c *Cart) Items() []Item { return slices.Clone(c.items) }

func (c *Cart) Item(sku string) (Item, bool) {
	index := c.index(sku)
	if index < 0 {
		return Item{}, false
	}
	return c.items[index], true
}

func (c *Cart) PromoCode() string { return c.promoCode }

func (c *Cart) IsEmpty() bool { return len(c.items) == 0 }

func (c *Cart) UpdatedAt() time.Time { return c.updatedAt }

func (c *Cart) ExpiresAt() time.Time { return c.expiresAt }

func (c *Cart) Version() int { return c.version }

func (c *Cart) AdvanceVersion() { c.version++ }
