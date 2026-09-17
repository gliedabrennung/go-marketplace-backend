package domain

import (
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type OfferCondition string

const (
	ConditionNew         OfferCondition = "new"
	ConditionUsed        OfferCondition = "used"
	ConditionRefurbished OfferCondition = "refurbished"
)

type OfferStatus string

const (
	OfferActive   OfferStatus = "active"
	OfferPaused   OfferStatus = "paused"
	OfferArchived OfferStatus = "archived"
)

const maxProcessingDays = 30

type SellerSKU struct {
	value string
}

func NewSellerSKU(raw string) (SellerSKU, error) {
	if raw == "" || len(raw) > 64 {
		return SellerSKU{}, ErrInvalidSellerSKU
	}
	for i := range len(raw) {
		c := raw[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_'
		if !ok {
			return SellerSKU{}, ErrInvalidSellerSKU
		}
	}
	return SellerSKU{value: raw}, nil
}

func (s SellerSKU) String() string { return s.value }

type OfferTerms struct {
	price          kernel.Money
	condition      OfferCondition
	processingDays int
}

func NewOfferTerms(amount int64, currency, condition string, processingDays int) (OfferTerms, error) {
	if amount <= 0 {
		return OfferTerms{}, ErrInvalidPrice
	}
	price, err := kernel.NewMoney(amount, kernel.Currency(currency))
	if err != nil {
		return OfferTerms{}, err
	}
	c := OfferCondition(condition)
	switch c {
	case ConditionNew, ConditionUsed, ConditionRefurbished:
	default:
		return OfferTerms{}, ErrInvalidCondition.WithDetail("%q", condition)
	}
	if processingDays < 0 || processingDays > maxProcessingDays {
		return OfferTerms{}, ErrInvalidProcessingTime
	}
	return OfferTerms{price: price, condition: c, processingDays: processingDays}, nil
}

func (t OfferTerms) Price() kernel.Money { return t.price }

func (t OfferTerms) Condition() OfferCondition { return t.condition }

func (t OfferTerms) ProcessingDays() int { return t.processingDays }

func (t OfferTerms) Equals(other OfferTerms) bool {
	return t.price.Equals(other.price) && t.condition == other.condition && t.processingDays == other.processingDays
}

type Offer struct {
	id        OfferID
	productID ProductID
	sellerID  kernel.SellerID
	sku       SellerSKU
	terms     OfferTerms
	status    OfferStatus
	createdAt time.Time
	updatedAt time.Time
	version   int

	events kernel.EventBuffer
}

type OfferState struct {
	OfferID        OfferID
	ProductID      ProductID
	SellerID       kernel.SellerID
	SellerSKU      string
	Price          kernel.Money
	Condition      OfferCondition
	ProcessingDays int
	Status         OfferStatus
}

func CreateOffer(id OfferID, product *Product, seller kernel.SellerID, sku SellerSKU, terms OfferTerms, now time.Time) (*Offer, error) {
	if id.IsZero() || seller.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if sku.value == "" {
		return nil, ErrInvalidSellerSKU
	}
	if !product.IsPublished() {
		return nil, ErrProductNotPublished
	}
	o := &Offer{
		id: id, productID: product.id, sellerID: seller, sku: sku, terms: terms,
		status: OfferActive, createdAt: now, updatedAt: now,
	}
	o.events.Record(OfferCreated{Offer: o.State(), At: now})
	return o, nil
}

func (o *Offer) ChangeTerms(seller kernel.SellerID, terms OfferTerms, now time.Time) error {
	if err := o.requireMutableBy(seller); err != nil {
		return err
	}
	if o.terms.Equals(terms) {
		return nil
	}
	o.terms = terms
	o.updatedAt = now
	o.events.Record(OfferUpdated{Offer: o.State(), At: now})
	return nil
}

func (o *Offer) Pause(seller kernel.SellerID, now time.Time) error {
	return o.changeStatus(seller, OfferPaused, now)
}

func (o *Offer) Activate(seller kernel.SellerID, now time.Time) error {
	return o.changeStatus(seller, OfferActive, now)
}

func (o *Offer) Archive(seller kernel.SellerID, now time.Time) error {
	if o.status == OfferArchived && seller == o.sellerID {
		return nil
	}
	return o.changeStatus(seller, OfferArchived, now)
}

func (o *Offer) changeStatus(seller kernel.SellerID, target OfferStatus, now time.Time) error {
	if err := o.requireMutableBy(seller); err != nil {
		return err
	}
	if o.status == target {
		return nil
	}
	o.status = target
	o.updatedAt = now
	o.events.Record(OfferStatusChanged{Offer: o.State(), At: now})
	return nil
}

func (o *Offer) requireMutableBy(seller kernel.SellerID) error {
	if seller != o.sellerID {
		return ErrNotOfferOwner
	}
	if o.status == OfferArchived {
		return ErrOfferArchived
	}
	return nil
}

func (o *Offer) State() OfferState {
	return OfferState{
		OfferID: o.id, ProductID: o.productID, SellerID: o.sellerID, SellerSKU: o.sku.value,
		Price: o.terms.price, Condition: o.terms.condition, ProcessingDays: o.terms.processingDays, Status: o.status,
	}
}

func (o *Offer) ID() OfferID { return o.id }

func (o *Offer) ProductID() ProductID { return o.productID }

func (o *Offer) SellerID() kernel.SellerID { return o.sellerID }

func (o *Offer) SKU() SellerSKU { return o.sku }

func (o *Offer) Terms() OfferTerms { return o.terms }

func (o *Offer) Status() OfferStatus { return o.status }

func (o *Offer) Version() int { return o.version }

func (o *Offer) AdvanceVersion() { o.version++ }

func (o *Offer) PullEvents() []kernel.DomainEvent { return o.events.Pull() }

type OfferSnapshot struct {
	ID             string
	ProductID      string
	SellerID       string
	SellerSKU      string
	PriceAmount    int64
	Currency       string
	Condition      string
	ProcessingDays int
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
}

func (o *Offer) Snapshot() OfferSnapshot {
	return OfferSnapshot{
		ID: o.id.String(), ProductID: o.productID.String(), SellerID: o.sellerID.String(), SellerSKU: o.sku.value,
		PriceAmount: o.terms.price.Amount(), Currency: string(o.terms.price.Currency()),
		Condition: string(o.terms.condition), ProcessingDays: o.terms.processingDays, Status: string(o.status),
		CreatedAt: o.createdAt, UpdatedAt: o.updatedAt, Version: o.version,
	}
}

func RehydrateOffer(s OfferSnapshot) (*Offer, error) {
	id, err := ParseOfferID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer: %w", err)
	}
	productID, err := ParseProductID(s.ProductID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer %s product: %w", s.ID, err)
	}
	sellerID, err := kernel.ParseSellerID(s.SellerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer %s seller: %w", s.ID, err)
	}
	sku, err := NewSellerSKU(s.SellerSKU)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer %s sku: %w", s.ID, err)
	}
	terms, err := NewOfferTerms(s.PriceAmount, s.Currency, s.Condition, s.ProcessingDays)
	if err != nil {
		return nil, fmt.Errorf("rehydrate offer %s terms: %w", s.ID, err)
	}
	return &Offer{
		id: id, productID: productID, sellerID: sellerID, sku: sku, terms: terms, status: OfferStatus(s.Status),
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version,
	}, nil
}
