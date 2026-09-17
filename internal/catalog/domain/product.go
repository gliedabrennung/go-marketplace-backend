package domain

import (
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type ProductStatus string

const (
	ProductStatusDraft        ProductStatus = "draft"
	ProductStatusOnModeration ProductStatus = "on_moderation"
	ProductStatusPublished    ProductStatus = "published"
	ProductStatusRejected     ProductStatus = "rejected"
)

var productTransitions = map[ProductStatus][]ProductStatus{
	ProductStatusDraft:        {ProductStatusOnModeration},
	ProductStatusOnModeration: {ProductStatusPublished, ProductStatusRejected},
	ProductStatusRejected:     {ProductStatusDraft, ProductStatusOnModeration},
	ProductStatusPublished:    {},
}

type ProductContent struct {
	Title       string
	Description string
	Brand       string
	Attributes  map[string]string
}

type Product struct {
	id              ProductID
	categoryID      CategoryID
	sellerID        kernel.SellerID
	title           string
	description     string
	brand           string
	attributes      map[string]AttributeValue
	images          []Image
	status          ProductStatus
	rejectionReason string
	createdAt       time.Time
	updatedAt       time.Time
	publishedAt     time.Time
	version         int

	events kernel.EventBuffer
}

type validatedContent struct {
	title       string
	description string
	brand       string
	attributes  map[string]AttributeValue
}

func validateContent(schema Schema, c ProductContent) (validatedContent, error) {
	title := strings.TrimSpace(c.Title)
	if n := utf8.RuneCountInString(title); n < 3 || n > 300 {
		return validatedContent{}, ErrInvalidTitle
	}
	description := strings.TrimSpace(c.Description)
	if utf8.RuneCountInString(description) > 10000 {
		return validatedContent{}, ErrInvalidDescription
	}
	brand := strings.TrimSpace(c.Brand)
	if utf8.RuneCountInString(brand) > 100 {
		return validatedContent{}, ErrInvalidBrand
	}
	attrs, err := schema.ParseValues(c.Attributes)
	if err != nil {
		return validatedContent{}, err
	}
	return validatedContent{title: title, description: description, brand: brand, attributes: attrs}, nil
}

func CreateProduct(id ProductID, author kernel.SellerID, class Classification, content ProductContent, now time.Time) (*Product, error) {
	if id.IsZero() || author.IsZero() || class.categoryID.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	fields, err := validateContent(class.schema, content)
	if err != nil {
		return nil, err
	}
	p := &Product{
		id:          id,
		categoryID:  class.categoryID,
		sellerID:    author,
		title:       fields.title,
		description: fields.description,
		brand:       fields.brand,
		attributes:  fields.attributes,
		status:      ProductStatusDraft,
		createdAt:   now,
		updatedAt:   now,
	}
	p.events.Record(ProductCreated{ProductID: id, CategoryID: class.categoryID, SellerID: author, Title: fields.title, At: now})
	return p, nil
}

func (p *Product) UpdateContent(author kernel.SellerID, class Classification, content ProductContent, now time.Time) error {
	if err := p.requireEditableBy(author); err != nil {
		return err
	}
	if class.categoryID != p.categoryID {
		return ErrCategoryMismatch
	}
	fields, err := validateContent(class.schema, content)
	if err != nil {
		return err
	}
	p.title, p.description, p.brand, p.attributes = fields.title, fields.description, fields.brand, fields.attributes
	if p.status == ProductStatusRejected {
		p.status = ProductStatusDraft
	}
	p.updatedAt = now
	p.events.Record(ProductContentUpdated{ProductID: p.id, Title: p.title, At: now})
	return nil
}

func (p *Product) SubmitForModeration(author kernel.SellerID, class Classification, now time.Time) error {
	if err := p.requireAuthor(author); err != nil {
		return err
	}
	if err := p.canTransition(ProductStatusOnModeration); err != nil {
		return err
	}
	if err := p.ensureComplete(class); err != nil {
		return err
	}
	p.status = ProductStatusOnModeration
	p.rejectionReason = ""
	p.updatedAt = now
	p.events.Record(ProductSubmitted{ProductID: p.id, SellerID: p.sellerID, At: now})
	return nil
}

func (p *Product) Publish(moderator kernel.UserID, class Classification, now time.Time) error {
	if err := p.canTransition(ProductStatusPublished); err != nil {
		return err
	}
	if err := p.ensureComplete(class); err != nil {
		return err
	}
	p.status = ProductStatusPublished
	p.publishedAt = now
	p.updatedAt = now
	p.events.Record(ProductPublished{
		ProductID:    p.id,
		SellerID:     p.sellerID,
		CategoryPath: class.Path(),
		Title:        p.title,
		Description:  p.description,
		Brand:        p.brand,
		Attributes:   class.schema.Entries(p.attributes),
		CoverKey:     p.CoverKey(),
		PublishedBy:  moderator,
		At:           now,
	})
	return nil
}

func (p *Product) Reject(moderator kernel.UserID, reason string, now time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}
	if err := p.canTransition(ProductStatusRejected); err != nil {
		return err
	}
	p.status = ProductStatusRejected
	p.rejectionReason = reason
	p.updatedAt = now
	p.events.Record(ProductRejected{ProductID: p.id, SellerID: p.sellerID, Reason: reason, RejectedBy: moderator, At: now})
	return nil
}

func (p *Product) ensureComplete(class Classification) error {
	if class.categoryID != p.categoryID {
		return ErrCategoryMismatch
	}
	values, err := class.schema.ParseValues(p.rawAttributes())
	if err != nil {
		return err
	}
	if missing := class.schema.MissingRequired(values); len(missing) > 0 {
		return ErrProductIncomplete.WithFields(missing...)
	}
	p.attributes = values
	return nil
}

func (p *Product) rawAttributes() map[string]string {
	out := make(map[string]string, len(p.attributes))
	for code, v := range p.attributes {
		out[code] = v.String()
	}
	return out
}

func (p *Product) canTransition(target ProductStatus) error {
	if slices.Contains(productTransitions[p.status], target) {
		return nil
	}
	return &TransitionError{Entity: "product", From: string(p.status), To: string(target)}
}

func (p *Product) requireAuthor(author kernel.SellerID) error {
	if author != p.sellerID {
		return ErrNotProductAuthor
	}
	return nil
}

func (p *Product) requireEditableBy(author kernel.SellerID) error {
	if err := p.requireAuthor(author); err != nil {
		return err
	}
	if p.status != ProductStatusDraft && p.status != ProductStatusRejected {
		return ErrProductLocked
	}
	return nil
}

func (p *Product) ID() ProductID { return p.id }

func (p *Product) CategoryID() CategoryID { return p.categoryID }

func (p *Product) SellerID() kernel.SellerID { return p.sellerID }

func (p *Product) Title() string { return p.title }

func (p *Product) Description() string { return p.description }

func (p *Product) Brand() string { return p.brand }

func (p *Product) Attributes() map[string]AttributeValue { return maps.Clone(p.attributes) }

func (p *Product) Status() ProductStatus { return p.status }

func (p *Product) IsPublished() bool { return p.status == ProductStatusPublished }

func (p *Product) RejectionReason() string { return p.rejectionReason }

func (p *Product) PublishedAt() time.Time { return p.publishedAt }

func (p *Product) Version() int { return p.version }

func (p *Product) AdvanceVersion() { p.version++ }

func (p *Product) PullEvents() []kernel.DomainEvent { return p.events.Pull() }
