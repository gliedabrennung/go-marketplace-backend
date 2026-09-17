package domain

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type AttributeValueSnapshot struct {
	Type  string
	Value string
}

type ImageSnapshot struct {
	ID          string
	ContentType string
	Size        int64
	Status      string
	Width       int
	Height      int
	CreatedAt   time.Time
}

type ProductSnapshot struct {
	ID              string
	CategoryID      string
	SellerID        string
	Title           string
	Description     string
	Brand           string
	Attributes      map[string]AttributeValueSnapshot
	Images          []ImageSnapshot
	Status          string
	RejectionReason string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	PublishedAt     time.Time
	Version         int
}

func (p *Product) Snapshot() ProductSnapshot {
	snap := ProductSnapshot{
		ID:              p.id.String(),
		CategoryID:      p.categoryID.String(),
		SellerID:        p.sellerID.String(),
		Title:           p.title,
		Description:     p.description,
		Brand:           p.brand,
		Attributes:      make(map[string]AttributeValueSnapshot, len(p.attributes)),
		Images:          make([]ImageSnapshot, len(p.images)),
		Status:          string(p.status),
		RejectionReason: p.rejectionReason,
		CreatedAt:       p.createdAt,
		UpdatedAt:       p.updatedAt,
		PublishedAt:     p.publishedAt,
		Version:         p.version,
	}
	for code, v := range p.attributes {
		snap.Attributes[code] = AttributeValueSnapshot{Type: string(v.typ), Value: v.String()}
	}
	for i, img := range p.images {
		snap.Images[i] = ImageSnapshot{
			ID: img.id.String(), ContentType: img.contentType, Size: img.size, Status: string(img.status),
			Width: img.width, Height: img.height, CreatedAt: img.createdAt,
		}
	}
	return snap
}

func RehydrateProduct(s ProductSnapshot) (*Product, error) {
	id, err := ParseProductID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate product: %w", err)
	}
	categoryID, err := ParseCategoryID(s.CategoryID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate product %s category: %w", s.ID, err)
	}
	sellerID, err := kernel.ParseSellerID(s.SellerID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate product %s seller: %w", s.ID, err)
	}
	p := &Product{
		id: id, categoryID: categoryID, sellerID: sellerID,
		title: s.Title, description: s.Description, brand: s.Brand,
		attributes: make(map[string]AttributeValue, len(s.Attributes)),
		status:     ProductStatus(s.Status), rejectionReason: s.RejectionReason,
		createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, publishedAt: s.PublishedAt, version: s.Version,
	}
	for code, raw := range s.Attributes {
		v, err := restoreValue(AttributeType(raw.Type), raw.Value)
		if err != nil {
			return nil, fmt.Errorf("rehydrate product %s attribute %s: %w", s.ID, code, err)
		}
		p.attributes[code] = v
	}
	for _, raw := range s.Images {
		imageID, err := ParseImageID(raw.ID)
		if err != nil {
			return nil, fmt.Errorf("rehydrate product %s image: %w", s.ID, err)
		}
		p.images = append(p.images, Image{
			id: imageID, contentType: raw.ContentType, size: raw.Size, status: ImageStatus(raw.Status),
			width: raw.Width, height: raw.Height, createdAt: raw.CreatedAt,
		})
	}
	return p, nil
}

func restoreValue(typ AttributeType, raw string) (AttributeValue, error) {
	switch typ {
	case AttributeNumber, AttributeUnit:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return AttributeValue{}, err
		}
		return AttributeValue{typ: typ, number: n}, nil
	case AttributeBoolean:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return AttributeValue{}, err
		}
		return AttributeValue{typ: typ, boolean: b}, nil
	case AttributeString, AttributeEnum:
		return AttributeValue{typ: typ, text: raw}, nil
	default:
		return AttributeValue{}, ErrInvalidAttributeType.WithDetail("%q", typ)
	}
}
