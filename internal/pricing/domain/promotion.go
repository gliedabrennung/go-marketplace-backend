package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type PromotionStatus string

const (
	PromotionDraft  PromotionStatus = "draft"
	PromotionActive PromotionStatus = "active"
	PromotionPaused PromotionStatus = "paused"
	PromotionEnded  PromotionStatus = "ended"
)

type Promotion struct {
	id        PromotionID
	name      string
	spec      DiscountSpec
	target    Target
	priority  int
	exclusive bool
	startsAt  time.Time
	endsAt    time.Time
	status    PromotionStatus
	createdAt time.Time
	updatedAt time.Time
	version   int

	events kernel.EventBuffer
}

type PromotionSpec struct {
	Name      string
	Discount  DiscountSpec
	Target    Target
	Priority  int
	Exclusive bool
	StartsAt  time.Time
	EndsAt    time.Time
}

func CreatePromotion(id PromotionID, spec PromotionSpec, now time.Time) (*Promotion, error) {
	if id.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	promotion := &Promotion{id: id, status: PromotionDraft, createdAt: now, updatedAt: now}
	if err := promotion.apply(spec, now); err != nil {
		return nil, err
	}
	promotion.events.Record(PromotionChanged{PromotionID: id, Status: PromotionDraft, At: now})
	return promotion, nil
}

func (p *Promotion) Update(spec PromotionSpec, now time.Time) error {
	if p.status == PromotionEnded {
		return ErrPromotionEnded
	}
	return p.apply(spec, now)
}

func (p *Promotion) apply(spec PromotionSpec, now time.Time) error {
	name := strings.TrimSpace(spec.Name)
	if name == "" || len([]rune(name)) > 200 {
		return ErrInvalidName
	}
	if err := spec.Discount.validate(); err != nil {
		return err
	}
	if spec.Target.isEmpty() {
		return ErrInvalidTarget
	}
	if !spec.EndsAt.IsZero() && !spec.EndsAt.After(spec.StartsAt) {
		return ErrInvalidPeriod
	}
	if _, err := newRule(p.id, spec.Discount, spec.Target, spec.Priority, spec.Exclusive); err != nil {
		return err
	}
	p.name, p.spec, p.target = name, spec.Discount, cloneTarget(spec.Target)
	p.priority, p.exclusive = spec.Priority, spec.Exclusive
	p.startsAt, p.endsAt = spec.StartsAt, spec.EndsAt
	p.updatedAt = now
	return nil
}

func (p *Promotion) Activate(now time.Time) error { return p.transition(PromotionActive, now) }

func (p *Promotion) Pause(now time.Time) error { return p.transition(PromotionPaused, now) }

func (p *Promotion) End(now time.Time) error { return p.transition(PromotionEnded, now) }

func (p *Promotion) transition(target PromotionStatus, now time.Time) error {
	if p.status == target {
		return nil
	}
	if p.status == PromotionEnded {
		return ErrPromotionEnded
	}
	p.status, p.updatedAt = target, now
	p.events.Record(PromotionChanged{PromotionID: p.id, Status: target, At: now})
	return nil
}

func (p *Promotion) IsRunning(now time.Time) bool {
	if p.status != PromotionActive {
		return false
	}
	if !p.startsAt.IsZero() && now.Before(p.startsAt) {
		return false
	}
	return p.endsAt.IsZero() || now.Before(p.endsAt)
}

func (p *Promotion) Rule() (DiscountRule, error) {
	return newRule(p.id, p.spec, p.target, p.priority, p.exclusive)
}

func (p *Promotion) ID() PromotionID { return p.id }

func (p *Promotion) Name() string { return p.name }

func (p *Promotion) Status() PromotionStatus { return p.status }

func (p *Promotion) Priority() int { return p.priority }

func (p *Promotion) Version() int { return p.version }

func (p *Promotion) AdvanceVersion() { p.version++ }

func (p *Promotion) PullEvents() []kernel.DomainEvent { return p.events.Pull() }

type PromotionSnapshot struct {
	ID          string
	Name        string
	Kind        string
	BasisPoints int
	Amount      int64
	Currency    string
	BuyQuantity int
	FreeUnits   int
	SKUs        []string
	Sellers     []string
	Categories  []string
	Priority    int
	Exclusive   bool
	Status      string
	StartsAt    time.Time
	EndsAt      time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

func (p *Promotion) Snapshot() PromotionSnapshot {
	snap := PromotionSnapshot{
		ID: p.id.String(), Name: p.name, Kind: string(p.spec.Kind), BasisPoints: p.spec.BasisPoints,
		Amount: p.spec.Amount, Currency: p.spec.Currency, BuyQuantity: p.spec.BuyQuantity, FreeUnits: p.spec.FreeUnits,
		Priority: p.priority, Exclusive: p.exclusive, Status: string(p.status),
		StartsAt: p.startsAt, EndsAt: p.endsAt, CreatedAt: p.createdAt, UpdatedAt: p.updatedAt, Version: p.version,
		SKUs: make([]string, 0, len(p.target.SKUs)), Sellers: make([]string, 0, len(p.target.Sellers)),
		Categories: make([]string, 0, len(p.target.Categories)),
	}
	for _, sku := range p.target.SKUs {
		snap.SKUs = append(snap.SKUs, sku.String())
	}
	for _, seller := range p.target.Sellers {
		snap.Sellers = append(snap.Sellers, seller.String())
	}
	for _, category := range p.target.Categories {
		snap.Categories = append(snap.Categories, category.String())
	}
	return snap
}

func RehydratePromotion(s PromotionSnapshot) (*Promotion, error) {
	id, err := ParsePromotionID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate promotion: %w", err)
	}
	target, err := restoreTarget(s)
	if err != nil {
		return nil, fmt.Errorf("rehydrate promotion %s target: %w", s.ID, err)
	}
	if !slices.Contains([]PromotionStatus{PromotionDraft, PromotionActive, PromotionPaused, PromotionEnded}, PromotionStatus(s.Status)) {
		return nil, fmt.Errorf("rehydrate promotion %s: unknown status %q", s.ID, s.Status)
	}
	return &Promotion{
		id: id, name: s.Name, target: target, priority: s.Priority, exclusive: s.Exclusive,
		spec: DiscountSpec{
			Kind: DiscountKind(s.Kind), BasisPoints: s.BasisPoints, Amount: s.Amount,
			Currency: s.Currency, BuyQuantity: s.BuyQuantity, FreeUnits: s.FreeUnits,
		},
		status: PromotionStatus(s.Status), startsAt: s.StartsAt, endsAt: s.EndsAt,
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version,
	}, nil
}

func restoreTarget(s PromotionSnapshot) (Target, error) {
	target := Target{}
	for _, raw := range s.SKUs {
		sku, err := NewSKU(raw)
		if err != nil {
			return Target{}, err
		}
		target.SKUs = append(target.SKUs, sku)
	}
	for _, raw := range s.Sellers {
		seller, err := kernel.ParseSellerID(raw)
		if err != nil {
			return Target{}, err
		}
		target.Sellers = append(target.Sellers, seller)
	}
	for _, raw := range s.Categories {
		category, err := ParseCategoryID(raw)
		if err != nil {
			return Target{}, err
		}
		target.Categories = append(target.Categories, category)
	}
	return target, nil
}

func cloneTarget(t Target) Target {
	return Target{
		SKUs: slices.Clone(t.SKUs), Sellers: slices.Clone(t.Sellers), Categories: slices.Clone(t.Categories),
	}
}
