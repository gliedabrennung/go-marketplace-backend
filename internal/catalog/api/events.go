package api

import "time"

const (
	EventCategoryCreated          = "catalog.category_created.v1"
	EventCategoryRenamed          = "catalog.category_renamed.v1"
	EventCategoryAttributeDefined = "catalog.category_attribute_defined.v1"
	EventCategoryAttributeRemoved = "catalog.category_attribute_removed.v1"
	EventProductCreated           = "catalog.product_created.v1"
	EventProductContentUpdated    = "catalog.product_content_updated.v1"
	EventProductSubmitted         = "catalog.product_submitted.v1"
	EventProductPublished         = "catalog.product_published.v1"
	EventProductRejected          = "catalog.product_rejected.v1"
	EventProductImageUploaded     = "catalog.product_image_uploaded.v1"
	EventProductImageProcessed    = "catalog.product_image_processed.v1"
	EventVariantGroupCreated      = "catalog.variant_group_created.v1"
	EventVariantGroupChanged      = "catalog.variant_group_changed.v1"
	EventOfferCreated             = "catalog.offer_created.v1"
	EventOfferUpdated             = "catalog.offer_updated.v1"
	EventOfferStatusChanged       = "catalog.offer_status_changed.v1"
	EventImportScheduled          = "catalog.offer_import_scheduled.v1"
	EventImportFinished           = "catalog.offer_import_finished.v1"
)

type CategoryCreatedV1 struct {
	CategoryID string    `json:"category_id"`
	ParentID   string    `json:"parent_id,omitempty"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	Path       []string  `json:"path"`
	OccurredAt time.Time `json:"occurred_at"`
}

type CategoryRenamedV1 struct {
	CategoryID string    `json:"category_id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	OccurredAt time.Time `json:"occurred_at"`
}

type CategoryAttributeV1 struct {
	CategoryID string    `json:"category_id"`
	Code       string    `json:"code"`
	Name       string    `json:"name,omitempty"`
	Type       string    `json:"type,omitempty"`
	Required   bool      `json:"required,omitempty"`
	Filterable bool      `json:"filterable,omitempty"`
	Options    []string  `json:"options,omitempty"`
	Unit       string    `json:"unit,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ProductCreatedV1 struct {
	ProductID  string    `json:"product_id"`
	CategoryID string    `json:"category_id"`
	SellerID   string    `json:"seller_id"`
	Title      string    `json:"title"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ProductContentUpdatedV1 struct {
	ProductID  string    `json:"product_id"`
	Title      string    `json:"title"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ProductSubmittedV1 struct {
	ProductID  string    `json:"product_id"`
	SellerID   string    `json:"seller_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ProductAttributeV1 struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Value      string   `json:"value"`
	Number     *float64 `json:"number,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	Filterable bool     `json:"filterable,omitempty"`
}

type ProductPublishedV1 struct {
	ProductID    string               `json:"product_id"`
	SellerID     string               `json:"seller_id"`
	CategoryPath []string             `json:"category_path"`
	Title        string               `json:"title"`
	Description  string               `json:"description,omitempty"`
	Brand        string               `json:"brand,omitempty"`
	Attributes   []ProductAttributeV1 `json:"attributes"`
	CoverKey     string               `json:"cover_key,omitempty"`
	PublishedBy  string               `json:"published_by"`
	OccurredAt   time.Time            `json:"occurred_at"`
}

type ProductRejectedV1 struct {
	ProductID  string    `json:"product_id"`
	SellerID   string    `json:"seller_id"`
	Reason     string    `json:"reason"`
	RejectedBy string    `json:"rejected_by"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ProductImageUploadedV1 struct {
	ProductID   string    `json:"product_id"`
	ImageID     string    `json:"image_id"`
	ObjectKey   string    `json:"object_key"`
	ContentType string    `json:"content_type"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type ProductImageProcessedV1 struct {
	ProductID  string    `json:"product_id"`
	ImageID    string    `json:"image_id"`
	CoverKey   string    `json:"cover_key,omitempty"`
	Published  bool      `json:"published"`
	OccurredAt time.Time `json:"occurred_at"`
}

type VariantGroupCreatedV1 struct {
	GroupID    string    `json:"group_id"`
	CategoryID string    `json:"category_id"`
	SellerID   string    `json:"seller_id"`
	Axes       []string  `json:"axes"`
	OccurredAt time.Time `json:"occurred_at"`
}

type VariantGroupChangedV1 struct {
	GroupID    string    `json:"group_id"`
	ProductIDs []string  `json:"product_ids"`
	OccurredAt time.Time `json:"occurred_at"`
}

type OfferV1 struct {
	OfferID        string    `json:"offer_id"`
	ProductID      string    `json:"product_id"`
	SellerID       string    `json:"seller_id"`
	SellerSKU      string    `json:"seller_sku"`
	PriceAmount    int64     `json:"price_amount"`
	Currency       string    `json:"currency"`
	Condition      string    `json:"condition"`
	ProcessingDays int       `json:"processing_days"`
	Status         string    `json:"status"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type ImportScheduledV1 struct {
	JobID      string    `json:"job_id"`
	SellerID   string    `json:"seller_id"`
	Format     string    `json:"format"`
	OccurredAt time.Time `json:"occurred_at"`
}

type ImportFinishedV1 struct {
	JobID         string    `json:"job_id"`
	SellerID      string    `json:"seller_id"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason,omitempty"`
	TotalRows     int       `json:"total_rows"`
	SucceededRows int       `json:"succeeded_rows"`
	FailedRows    int       `json:"failed_rows"`
	OccurredAt    time.Time `json:"occurred_at"`
}
