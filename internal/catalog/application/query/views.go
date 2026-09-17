package query

import (
	"maps"
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
)

type CategoryNode struct {
	ID       string
	ParentID string
	Name     string
	Slug     string
	Depth    int
}

type AttributeView struct {
	Code       string
	Name       string
	Type       string
	Required   bool
	Filterable bool
	Options    []string
	Unit       string
	CategoryID string
}

type CategoryView struct {
	ID         string
	ParentID   string
	Name       string
	Slug       string
	Path       []CategoryNode
	Attributes []AttributeView
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ProductAttributeView struct {
	Code  string
	Name  string
	Type  string
	Value string
	Unit  string
}

type ImageView struct {
	ID          string
	Status      string
	ContentType string
	Width       int
	Height      int
	OriginalURL string
	SmallURL    string
	LargeURL    string
}

type VariantMemberView struct {
	ProductID  string
	AxisValues map[string]string
}

type VariantView struct {
	GroupID string
	Axes    []string
	Members []VariantMemberView
}

type ProductView struct {
	ID              string
	CategoryID      string
	SellerID        string
	Title           string
	Description     string
	Brand           string
	Attributes      []ProductAttributeView
	Images          []ImageView
	Variants        *VariantView
	Status          string
	RejectionReason string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	PublishedAt     time.Time
}

type ProductSummary struct {
	ID         string
	CategoryID string
	SellerID   string
	Title      string
	Brand      string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type OfferView struct {
	ID             string
	ProductID      string
	SellerID       string
	SellerSKU      string
	Price          int64
	Currency       string
	Condition      string
	ProcessingDays int
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ImportRowErrorView struct {
	Row     int
	Field   string
	Code    string
	Message string
}

type ImportJobView struct {
	ID            string
	SellerID      string
	RequestedBy   string
	Format        string
	Status        string
	TotalRows     int
	SucceededRows int
	FailedRows    int
	Errors        []ImportRowErrorView
	FailureReason string
	ReportKey     string
	ReportURL     string
	CreatedAt     time.Time
	StartedAt     time.Time
	FinishedAt    time.Time
	UpdatedAt     time.Time
}

func NewCategoryNode(s domain.CategorySnapshot) CategoryNode {
	return CategoryNode{ID: s.ID, ParentID: s.ParentID, Name: s.Name, Slug: s.Slug, Depth: len(s.Ancestors) + 1}
}

func NewCategoryView(chain []domain.CategorySnapshot) CategoryView {
	leaf := chain[len(chain)-1]
	view := CategoryView{
		ID: leaf.ID, ParentID: leaf.ParentID, Name: leaf.Name, Slug: leaf.Slug,
		CreatedAt: leaf.CreatedAt, UpdatedAt: leaf.UpdatedAt,
		Path: make([]CategoryNode, 0, len(chain)), Attributes: []AttributeView{},
	}
	for _, c := range chain {
		view.Path = append(view.Path, NewCategoryNode(c))
		for _, a := range c.Attributes {
			view.Attributes = append(view.Attributes, AttributeView{
				Code: a.Code, Name: a.Name, Type: a.Type, Required: a.Required, Filterable: a.Filterable,
				Options: slices.Clone(a.Options), Unit: a.Unit, CategoryID: c.ID,
			})
		}
	}
	return view
}

func NewProductView(snap domain.ProductSnapshot, category CategoryView) ProductView {
	view := ProductView{
		ID: snap.ID, CategoryID: snap.CategoryID, SellerID: snap.SellerID, Title: snap.Title,
		Description: snap.Description, Brand: snap.Brand, Status: snap.Status, RejectionReason: snap.RejectionReason,
		CreatedAt: snap.CreatedAt, UpdatedAt: snap.UpdatedAt, PublishedAt: snap.PublishedAt,
		Attributes: []ProductAttributeView{}, Images: make([]ImageView, 0, len(snap.Images)),
	}
	for _, def := range category.Attributes {
		value, ok := snap.Attributes[def.Code]
		if !ok {
			continue
		}
		view.Attributes = append(view.Attributes, ProductAttributeView{
			Code: def.Code, Name: def.Name, Type: value.Type, Value: value.Value, Unit: def.Unit,
		})
	}
	for _, img := range snap.Images {
		view.Images = append(view.Images, ImageView{
			ID: img.ID, Status: img.Status, ContentType: img.ContentType, Width: img.Width, Height: img.Height,
		})
	}
	return view
}

func NewProductSummary(snap domain.ProductSnapshot) ProductSummary {
	return ProductSummary{
		ID: snap.ID, CategoryID: snap.CategoryID, SellerID: snap.SellerID, Title: snap.Title, Brand: snap.Brand,
		Status: snap.Status, CreatedAt: snap.CreatedAt, UpdatedAt: snap.UpdatedAt,
	}
}

func NewVariantView(snap domain.VariantGroupSnapshot, include func(productID string) bool) *VariantView {
	view := &VariantView{GroupID: snap.ID, Axes: slices.Clone(snap.Axes), Members: []VariantMemberView{}}
	for _, m := range snap.Members {
		if include(m.ProductID) {
			view.Members = append(view.Members, VariantMemberView{ProductID: m.ProductID, AxisValues: maps.Clone(m.AxisValues)})
		}
	}
	return view
}

func NewOfferView(snap domain.OfferSnapshot) OfferView {
	return OfferView{
		ID: snap.ID, ProductID: snap.ProductID, SellerID: snap.SellerID, SellerSKU: snap.SellerSKU,
		Price: snap.PriceAmount, Currency: snap.Currency, Condition: snap.Condition, ProcessingDays: snap.ProcessingDays,
		Status: snap.Status, CreatedAt: snap.CreatedAt, UpdatedAt: snap.UpdatedAt,
	}
}

func NewImportJobView(snap domain.ImportJobSnapshot, withErrors bool) ImportJobView {
	view := ImportJobView{
		ID: snap.ID, SellerID: snap.SellerID, RequestedBy: snap.RequestedBy, Format: snap.Format, Status: snap.Status,
		TotalRows: snap.TotalRows, SucceededRows: snap.SucceededRows, FailedRows: snap.FailedRows,
		FailureReason: snap.FailureReason, ReportKey: snap.ReportKey,
		CreatedAt: snap.CreatedAt, StartedAt: snap.StartedAt, FinishedAt: snap.FinishedAt, UpdatedAt: snap.UpdatedAt,
	}
	if withErrors {
		view.Errors = make([]ImportRowErrorView, 0, len(snap.Errors))
		for _, e := range snap.Errors {
			view.Errors = append(view.Errors, ImportRowErrorView(e))
		}
	}
	return view
}
