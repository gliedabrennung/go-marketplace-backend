package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const MaxCategoryDepth = 6

type Category struct {
	id         CategoryID
	parentID   CategoryID
	name       string
	slug       string
	ancestors  []CategoryID
	attributes []AttributeDefinition
	createdAt  time.Time
	updatedAt  time.Time
	version    int

	events kernel.EventBuffer
}

func CreateCategory(id CategoryID, name, slug string, parent *Category, now time.Time) (*Category, error) {
	if id.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	name, slug, err := normalizeCategoryNaming(name, slug)
	if err != nil {
		return nil, err
	}
	c := &Category{id: id, name: name, slug: slug, createdAt: now, updatedAt: now}
	if parent != nil {
		if parent.Depth() >= MaxCategoryDepth {
			return nil, ErrCategoryTooDeep
		}
		c.parentID = parent.id
		c.ancestors = parent.Path()
	}
	c.events.Record(CategoryCreated{CategoryID: id, ParentID: c.parentID, Name: name, Slug: slug, Path: c.Path(), At: now})
	return c, nil
}

func normalizeCategoryNaming(name, slug string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", "", ErrInvalidCategoryName
	}
	if slug == "" || len(slug) > 100 {
		return "", "", ErrInvalidSlug
	}
	for i := range len(slug) {
		ch := slug[i]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
			return "", "", ErrInvalidSlug
		}
	}
	return name, slug, nil
}

func (c *Category) Rename(name, slug string, now time.Time) error {
	name, slug, err := normalizeCategoryNaming(name, slug)
	if err != nil {
		return err
	}
	if name == c.name && slug == c.slug {
		return nil
	}
	c.name, c.slug, c.updatedAt = name, slug, now
	c.events.Record(CategoryRenamed{CategoryID: c.id, Name: name, Slug: slug, At: now})
	return nil
}

func (c *Category) DefineAttribute(def AttributeDefinition, inherited Schema, descendantCodes []string, now time.Time) error {
	if def.code == "" {
		return ErrInvalidAttributeCode
	}
	if inherited.Has(def.code) || slices.Contains(descendantCodes, def.code) || slices.ContainsFunc(c.attributes, func(a AttributeDefinition) bool {
		return a.code == def.code
	}) {
		return ErrDuplicateAttribute.WithDetail("%q", def.code)
	}
	c.attributes = append(c.attributes, def)
	c.updatedAt = now
	c.events.Record(CategoryAttributeDefined{CategoryID: c.id, Attribute: def, At: now})
	return nil
}

func (c *Category) RemoveAttribute(code string, now time.Time) error {
	idx := slices.IndexFunc(c.attributes, func(a AttributeDefinition) bool { return a.code == code })
	if idx < 0 {
		return ErrAttributeNotFound.WithDetail("%q", code)
	}
	c.attributes = slices.Delete(c.attributes, idx, idx+1)
	c.updatedAt = now
	c.events.Record(CategoryAttributeRemoved{CategoryID: c.id, Code: code, At: now})
	return nil
}

func (c *Category) ID() CategoryID { return c.id }

func (c *Category) ParentID() CategoryID { return c.parentID }

func (c *Category) Name() string { return c.name }

func (c *Category) Slug() string { return c.slug }

func (c *Category) Depth() int { return len(c.ancestors) + 1 }

func (c *Category) Ancestors() []CategoryID { return slices.Clone(c.ancestors) }

func (c *Category) Path() []CategoryID { return append(slices.Clone(c.ancestors), c.id) }

func (c *Category) Attributes() []AttributeDefinition { return slices.Clone(c.attributes) }

func (c *Category) Version() int { return c.version }

func (c *Category) AdvanceVersion() { c.version++ }

func (c *Category) PullEvents() []kernel.DomainEvent { return c.events.Pull() }

type CategorySnapshot struct {
	ID         string
	ParentID   string
	Name       string
	Slug       string
	Ancestors  []string
	Attributes []AttributeSpec
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int
}

func (c *Category) Snapshot() CategorySnapshot {
	snap := CategorySnapshot{
		ID:         c.id.String(),
		ParentID:   c.parentID.String(),
		Name:       c.name,
		Slug:       c.slug,
		Ancestors:  make([]string, len(c.ancestors)),
		Attributes: make([]AttributeSpec, len(c.attributes)),
		CreatedAt:  c.createdAt,
		UpdatedAt:  c.updatedAt,
		Version:    c.version,
	}
	for i, a := range c.ancestors {
		snap.Ancestors[i] = a.String()
	}
	for i, a := range c.attributes {
		snap.Attributes[i] = a.Spec()
	}
	return snap
}

func RehydrateCategory(s CategorySnapshot) (*Category, error) {
	id, err := ParseCategoryID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate category: %w", err)
	}
	c := &Category{id: id, name: s.Name, slug: s.Slug, createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, version: s.Version}
	if s.ParentID != "" {
		if c.parentID, err = ParseCategoryID(s.ParentID); err != nil {
			return nil, fmt.Errorf("rehydrate category %s parent: %w", s.ID, err)
		}
	}
	for _, raw := range s.Ancestors {
		ancestor, err := ParseCategoryID(raw)
		if err != nil {
			return nil, fmt.Errorf("rehydrate category %s ancestors: %w", s.ID, err)
		}
		c.ancestors = append(c.ancestors, ancestor)
	}
	for _, spec := range s.Attributes {
		def, err := NewAttributeDefinition(spec)
		if err != nil {
			return nil, fmt.Errorf("rehydrate category %s attribute %s: %w", s.ID, spec.Code, err)
		}
		c.attributes = append(c.attributes, def)
	}
	return c, nil
}
