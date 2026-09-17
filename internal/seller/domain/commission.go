package domain

import (
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type CategoryCommission struct {
	categoryID CategoryID
	rate       kernel.BasisPoints
	updatedAt  time.Time
	version    int

	events kernel.EventBuffer
}

func SetCategoryCommission(category CategoryID, rate kernel.BasisPoints, by kernel.UserID, now time.Time) (*CategoryCommission, error) {
	if category.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	c := &CategoryCommission{categoryID: category, rate: rate, updatedAt: now}
	c.events.Record(CategoryCommissionChanged{CategoryID: category, Rate: rate, ChangedBy: by, At: now})
	return c, nil
}

func (c *CategoryCommission) Change(rate kernel.BasisPoints, by kernel.UserID, now time.Time) {
	if c.rate == rate {
		return
	}
	c.rate = rate
	c.updatedAt = now
	c.events.Record(CategoryCommissionChanged{CategoryID: c.categoryID, Rate: rate, ChangedBy: by, At: now})
}

func (c *CategoryCommission) CategoryID() CategoryID { return c.categoryID }

func (c *CategoryCommission) Rate() kernel.BasisPoints { return c.rate }

func (c *CategoryCommission) UpdatedAt() time.Time { return c.updatedAt }

func (c *CategoryCommission) Version() int { return c.version }

func (c *CategoryCommission) AdvanceVersion() { c.version++ }

func (c *CategoryCommission) PullEvents() []kernel.DomainEvent { return c.events.Pull() }

func RehydrateCategoryCommission(category string, rateBP int, updatedAt time.Time, version int) (*CategoryCommission, error) {
	id, err := ParseCategoryID(category)
	if err != nil {
		return nil, fmt.Errorf("rehydrate category commission: %w", err)
	}
	rate, err := kernel.NewBasisPoints(rateBP)
	if err != nil {
		return nil, fmt.Errorf("rehydrate category commission %s: %w", category, err)
	}
	return &CategoryCommission{categoryID: id, rate: rate, updatedAt: updatedAt, version: version}, nil
}

type CommissionPolicy struct {
	DefaultRate kernel.BasisPoints
}

func DefaultCommissionPolicy() CommissionPolicy {
	return CommissionPolicy{DefaultRate: kernel.MustBasisPoints(1000)}
}

func (p CommissionPolicy) RateFor(s *Seller, category CategoryID, base *CategoryCommission) kernel.BasisPoints {
	if rate, ok := s.CommissionOverride(category); ok {
		return rate
	}
	if base != nil {
		return base.Rate()
	}
	return p.DefaultRate
}
