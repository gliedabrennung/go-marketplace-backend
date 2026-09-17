package domain

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type CategoryCreated struct {
	CategoryID CategoryID
	ParentID   CategoryID
	Name       string
	Slug       string
	Path       []CategoryID
	At         time.Time
}

func (e CategoryCreated) EventName() string     { return "catalog.category_created.v1" }
func (e CategoryCreated) AggregateID() string   { return e.CategoryID.String() }
func (e CategoryCreated) OccurredAt() time.Time { return e.At }

type CategoryRenamed struct {
	CategoryID CategoryID
	Name       string
	Slug       string
	At         time.Time
}

func (e CategoryRenamed) EventName() string     { return "catalog.category_renamed.v1" }
func (e CategoryRenamed) AggregateID() string   { return e.CategoryID.String() }
func (e CategoryRenamed) OccurredAt() time.Time { return e.At }

type CategoryAttributeDefined struct {
	CategoryID CategoryID
	Attribute  AttributeDefinition
	At         time.Time
}

func (e CategoryAttributeDefined) EventName() string     { return "catalog.category_attribute_defined.v1" }
func (e CategoryAttributeDefined) AggregateID() string   { return e.CategoryID.String() }
func (e CategoryAttributeDefined) OccurredAt() time.Time { return e.At }

type CategoryAttributeRemoved struct {
	CategoryID CategoryID
	Code       string
	At         time.Time
}

func (e CategoryAttributeRemoved) EventName() string     { return "catalog.category_attribute_removed.v1" }
func (e CategoryAttributeRemoved) AggregateID() string   { return e.CategoryID.String() }
func (e CategoryAttributeRemoved) OccurredAt() time.Time { return e.At }

type ProductCreated struct {
	ProductID  ProductID
	CategoryID CategoryID
	SellerID   kernel.SellerID
	Title      string
	At         time.Time
}

func (e ProductCreated) EventName() string     { return "catalog.product_created.v1" }
func (e ProductCreated) AggregateID() string   { return e.ProductID.String() }
func (e ProductCreated) OccurredAt() time.Time { return e.At }

type ProductContentUpdated struct {
	ProductID ProductID
	Title     string
	At        time.Time
}

func (e ProductContentUpdated) EventName() string     { return "catalog.product_content_updated.v1" }
func (e ProductContentUpdated) AggregateID() string   { return e.ProductID.String() }
func (e ProductContentUpdated) OccurredAt() time.Time { return e.At }

type ProductSubmitted struct {
	ProductID ProductID
	SellerID  kernel.SellerID
	At        time.Time
}

func (e ProductSubmitted) EventName() string     { return "catalog.product_submitted.v1" }
func (e ProductSubmitted) AggregateID() string   { return e.ProductID.String() }
func (e ProductSubmitted) OccurredAt() time.Time { return e.At }

type ProductPublished struct {
	ProductID    ProductID
	SellerID     kernel.SellerID
	CategoryPath []CategoryID
	Title        string
	Description  string
	Brand        string
	Attributes   []AttributeEntry
	CoverKey     string
	PublishedBy  kernel.UserID
	At           time.Time
}

func (e ProductPublished) EventName() string     { return "catalog.product_published.v1" }
func (e ProductPublished) AggregateID() string   { return e.ProductID.String() }
func (e ProductPublished) OccurredAt() time.Time { return e.At }

type ProductRejected struct {
	ProductID  ProductID
	SellerID   kernel.SellerID
	Reason     string
	RejectedBy kernel.UserID
	At         time.Time
}

func (e ProductRejected) EventName() string     { return "catalog.product_rejected.v1" }
func (e ProductRejected) AggregateID() string   { return e.ProductID.String() }
func (e ProductRejected) OccurredAt() time.Time { return e.At }

type ProductImageUploaded struct {
	ProductID   ProductID
	ImageID     ImageID
	ObjectKey   string
	ContentType string
	At          time.Time
}

func (e ProductImageUploaded) EventName() string     { return "catalog.product_image_uploaded.v1" }
func (e ProductImageUploaded) AggregateID() string   { return e.ProductID.String() }
func (e ProductImageUploaded) OccurredAt() time.Time { return e.At }

type ProductImageProcessed struct {
	ProductID ProductID
	ImageID   ImageID
	CoverKey  string
	Published bool
	At        time.Time
}

func (e ProductImageProcessed) EventName() string     { return "catalog.product_image_processed.v1" }
func (e ProductImageProcessed) AggregateID() string   { return e.ProductID.String() }
func (e ProductImageProcessed) OccurredAt() time.Time { return e.At }

type VariantGroupCreated struct {
	GroupID    VariantGroupID
	CategoryID CategoryID
	SellerID   kernel.SellerID
	Axes       []string
	At         time.Time
}

func (e VariantGroupCreated) EventName() string     { return "catalog.variant_group_created.v1" }
func (e VariantGroupCreated) AggregateID() string   { return e.GroupID.String() }
func (e VariantGroupCreated) OccurredAt() time.Time { return e.At }

type VariantGroupChanged struct {
	GroupID    VariantGroupID
	ProductIDs []ProductID
	At         time.Time
}

func (e VariantGroupChanged) EventName() string     { return "catalog.variant_group_changed.v1" }
func (e VariantGroupChanged) AggregateID() string   { return e.GroupID.String() }
func (e VariantGroupChanged) OccurredAt() time.Time { return e.At }

type OfferCreated struct {
	Offer OfferState
	At    time.Time
}

func (e OfferCreated) EventName() string     { return "catalog.offer_created.v1" }
func (e OfferCreated) AggregateID() string   { return e.Offer.OfferID.String() }
func (e OfferCreated) OccurredAt() time.Time { return e.At }

type OfferUpdated struct {
	Offer OfferState
	At    time.Time
}

func (e OfferUpdated) EventName() string     { return "catalog.offer_updated.v1" }
func (e OfferUpdated) AggregateID() string   { return e.Offer.OfferID.String() }
func (e OfferUpdated) OccurredAt() time.Time { return e.At }

type OfferStatusChanged struct {
	Offer OfferState
	At    time.Time
}

func (e OfferStatusChanged) EventName() string     { return "catalog.offer_status_changed.v1" }
func (e OfferStatusChanged) AggregateID() string   { return e.Offer.OfferID.String() }
func (e OfferStatusChanged) OccurredAt() time.Time { return e.At }

type ImportScheduled struct {
	JobID    ImportJobID
	SellerID kernel.SellerID
	Format   ImportFormat
	At       time.Time
}

func (e ImportScheduled) EventName() string     { return "catalog.offer_import_scheduled.v1" }
func (e ImportScheduled) AggregateID() string   { return e.JobID.String() }
func (e ImportScheduled) OccurredAt() time.Time { return e.At }

type ImportFinished struct {
	JobID         ImportJobID
	SellerID      kernel.SellerID
	Status        ImportStatus
	Reason        string
	TotalRows     int
	SucceededRows int
	FailedRows    int
	At            time.Time
}

func (e ImportFinished) EventName() string     { return "catalog.offer_import_finished.v1" }
func (e ImportFinished) AggregateID() string   { return e.JobID.String() }
func (e ImportFinished) OccurredAt() time.Time { return e.At }
