package domain

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const maxVariantAxes = 3

type VariantMember struct {
	productID  ProductID
	axisValues map[string]string
}

func (m VariantMember) ProductID() ProductID { return m.productID }

func (m VariantMember) AxisValues() map[string]string { return maps.Clone(m.axisValues) }

type VariantGroup struct {
	id         VariantGroupID
	categoryID CategoryID
	sellerID   kernel.SellerID
	axes       []string
	members    []VariantMember
	createdAt  time.Time
	updatedAt  time.Time
	version    int

	events kernel.EventBuffer
}

func CreateVariantGroup(id VariantGroupID, owner kernel.SellerID, class Classification, axes []string, now time.Time) (*VariantGroup, error) {
	if id.IsZero() || owner.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if len(axes) == 0 || len(axes) > maxVariantAxes {
		return nil, ErrInvalidVariantAxes
	}
	for i, code := range axes {
		def, ok := class.schema.Definition(code)
		if !ok || slices.Contains(axes[:i], code) || (def.typ != AttributeEnum && def.typ != AttributeString) {
			return nil, ErrInvalidVariantAxes.WithDetail("%q", code)
		}
	}
	g := &VariantGroup{
		id: id, categoryID: class.categoryID, sellerID: owner, axes: slices.Clone(axes), createdAt: now, updatedAt: now,
	}
	g.events.Record(VariantGroupCreated{GroupID: id, CategoryID: class.categoryID, SellerID: owner, Axes: g.Axes(), At: now})
	return g, nil
}

func (g *VariantGroup) AddProduct(owner kernel.SellerID, product *Product, now time.Time) error {
	if owner != g.sellerID {
		return ErrNotVariantOwner
	}
	if product.sellerID != g.sellerID {
		return ErrNotProductAuthor
	}
	if product.categoryID != g.categoryID {
		return ErrVariantCategoryMismatch
	}
	values := make(map[string]string, len(g.axes))
	for _, axis := range g.axes {
		v, ok := product.attributes[axis]
		if !ok {
			return ErrVariantAxisMissing.WithDetail("%q", axis)
		}
		values[axis] = v.String()
	}
	for _, m := range g.members {
		if m.productID == product.id {
			return nil
		}
		if maps.Equal(m.axisValues, values) {
			return ErrVariantConflict
		}
	}
	g.members = append(g.members, VariantMember{productID: product.id, axisValues: values})
	g.updatedAt = now
	g.events.Record(VariantGroupChanged{GroupID: g.id, ProductIDs: g.ProductIDs(), At: now})
	return nil
}

func (g *VariantGroup) RemoveProduct(owner kernel.SellerID, productID ProductID, now time.Time) error {
	if owner != g.sellerID {
		return ErrNotVariantOwner
	}
	idx := slices.IndexFunc(g.members, func(m VariantMember) bool { return m.productID == productID })
	if idx < 0 {
		return ErrVariantMemberNotFound
	}
	g.members = slices.Delete(g.members, idx, idx+1)
	g.updatedAt = now
	g.events.Record(VariantGroupChanged{GroupID: g.id, ProductIDs: g.ProductIDs(), At: now})
	return nil
}

func (g *VariantGroup) ID() VariantGroupID { return g.id }

func (g *VariantGroup) CategoryID() CategoryID { return g.categoryID }

func (g *VariantGroup) SellerID() kernel.SellerID { return g.sellerID }

func (g *VariantGroup) Axes() []string { return slices.Clone(g.axes) }

func (g *VariantGroup) Members() []VariantMember { return slices.Clone(g.members) }

func (g *VariantGroup) ProductIDs() []ProductID {
	out := make([]ProductID, len(g.members))
	for i, m := range g.members {
		out[i] = m.productID
	}
	return out
}

func (g *VariantGroup) Version() int { return g.version }

func (g *VariantGroup) AdvanceVersion() { g.version++ }

func (g *VariantGroup) PullEvents() []kernel.DomainEvent { return g.events.Pull() }

type VariantMemberSnapshot struct {
	ProductID  string
	AxisValues map[string]string
}

type VariantGroupSnapshot struct {
	ID         string
	CategoryID string
	SellerID   string
	Axes       []string
	Members    []VariantMemberSnapshot
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int
}

func (g *VariantGroup) Snapshot() VariantGroupSnapshot {
	snap := VariantGroupSnapshot{
		ID: g.id.String(), CategoryID: g.categoryID.String(), SellerID: g.sellerID.String(),
		Axes: g.Axes(), Members: make([]VariantMemberSnapshot, len(g.members)),
		CreatedAt: g.createdAt, UpdatedAt: g.updatedAt, Version: g.version,
	}
	for i, m := range g.members {
		snap.Members[i] = VariantMemberSnapshot{ProductID: m.productID.String(), AxisValues: maps.Clone(m.axisValues)}
	}
	return snap
}

func RehydrateVariantGroup(s VariantGroupSnapshot) (*VariantGroup, error) {
	id, err := ParseVariantGroupID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate variant group: %w", err)
	}
	categoryID, err := ParseCategoryID(s.CategoryID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate variant group %s category: %w", s.ID, err)
	}
	sellerID, err := kernel.ParseSellerID(s.SellerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate variant group %s seller: %w", s.ID, err)
	}
	g := &VariantGroup{
		id: id, categoryID: categoryID, sellerID: sellerID, axes: slices.Clone(s.Axes),
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version,
	}
	for _, m := range s.Members {
		productID, err := ParseProductID(m.ProductID)
		if err != nil {
			return nil, fmt.Errorf("rehydrate variant group %s member: %w", s.ID, err)
		}
		g.members = append(g.members, VariantMember{productID: productID, axisValues: maps.Clone(m.AxisValues)})
	}
	return g, nil
}
