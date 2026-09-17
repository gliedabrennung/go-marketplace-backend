package domain

import (
	"fmt"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	ErrInvalidCategoryName     = kernel.Validation("CATALOG_INVALID_CATEGORY_NAME", "category name must be 1-100 characters long")
	ErrInvalidSlug             = kernel.Validation("CATALOG_INVALID_SLUG", "slug must contain 1-100 lowercase latin letters, digits or hyphens")
	ErrInvalidAttributeCode    = kernel.Validation("CATALOG_INVALID_ATTRIBUTE_CODE", "attribute code must start with a letter and contain up to 64 lowercase letters, digits or underscores")
	ErrInvalidAttributeName    = kernel.Validation("CATALOG_INVALID_ATTRIBUTE_NAME", "attribute name must be 1-100 characters long")
	ErrInvalidAttributeType    = kernel.Validation("CATALOG_INVALID_ATTRIBUTE_TYPE", "attribute type must be string, number, boolean, enum or unit")
	ErrInvalidAttributeOptions = kernel.Validation("CATALOG_INVALID_ATTRIBUTE_OPTIONS", "enum attribute requires 1-200 distinct non-empty options; other types must not define options")
	ErrInvalidAttributeUnit    = kernel.Validation("CATALOG_INVALID_ATTRIBUTE_UNIT", "unit attribute requires a unit of up to 16 characters; other types must not define a unit")
	ErrInvalidAttributes       = kernel.Validation("CATALOG_INVALID_ATTRIBUTES", "product attributes do not match the category schema")
	ErrInvalidTitle            = kernel.Validation("CATALOG_INVALID_TITLE", "title must be 3-300 characters long")
	ErrInvalidDescription      = kernel.Validation("CATALOG_INVALID_DESCRIPTION", "description must be at most 10000 characters long")
	ErrInvalidBrand            = kernel.Validation("CATALOG_INVALID_BRAND", "brand must be at most 100 characters long")
	ErrUnsupportedImageType    = kernel.Validation("CATALOG_UNSUPPORTED_IMAGE_TYPE", "only JPEG, PNG and WebP images are allowed")
	ErrInvalidImageSize        = kernel.Validation("CATALOG_INVALID_IMAGE_SIZE", "image size must be between 1 byte and 10 MB")
	ErrInvalidImageOrder       = kernel.Validation("CATALOG_INVALID_IMAGE_ORDER", "image order must list every product image exactly once")
	ErrReasonRequired          = kernel.Validation("CATALOG_REASON_REQUIRED", "reason is required")
	ErrInvalidVariantAxes      = kernel.Validation("CATALOG_INVALID_VARIANT_AXES", "variant axes must be 1-3 distinct enum or string attributes of the category")
	ErrInvalidSellerSKU        = kernel.Validation("CATALOG_INVALID_SELLER_SKU", "seller SKU must be 1-64 characters: letters, digits, dot, dash or underscore")
	ErrInvalidPrice            = kernel.Validation("CATALOG_INVALID_PRICE", "price must be a positive amount in minor units")
	ErrInvalidCondition        = kernel.Validation("CATALOG_INVALID_CONDITION", "condition must be new, used or refurbished")
	ErrInvalidProcessingTime   = kernel.Validation("CATALOG_INVALID_PROCESSING_TIME", "processing time must be between 0 and 30 days")
	ErrInvalidImportFormat     = kernel.Validation("CATALOG_INVALID_IMPORT_FORMAT", "import format must be csv, xlsx or json")
	ErrInvalidObjectKey        = kernel.Validation("CATALOG_INVALID_OBJECT_KEY", "object key is invalid")

	ErrCategoryNotFound     = kernel.NotFound("CATALOG_CATEGORY_NOT_FOUND", "category not found")
	ErrAttributeNotFound    = kernel.NotFound("CATALOG_ATTRIBUTE_NOT_FOUND", "attribute not found")
	ErrProductNotFound      = kernel.NotFound("CATALOG_PRODUCT_NOT_FOUND", "product not found")
	ErrImageNotFound        = kernel.NotFound("CATALOG_IMAGE_NOT_FOUND", "image not found")
	ErrVariantGroupNotFound = kernel.NotFound("CATALOG_VARIANT_GROUP_NOT_FOUND", "variant group not found")
	ErrOfferNotFound        = kernel.NotFound("CATALOG_OFFER_NOT_FOUND", "offer not found")
	ErrImportJobNotFound    = kernel.NotFound("CATALOG_IMPORT_JOB_NOT_FOUND", "import job not found")

	ErrDuplicateAttribute    = kernel.Conflict("CATALOG_DUPLICATE_ATTRIBUTE", "attribute code is already defined in the category tree")
	ErrCategorySlugTaken     = kernel.Conflict("CATALOG_CATEGORY_SLUG_TAKEN", "category slug is already used by a sibling")
	ErrVariantConflict       = kernel.Conflict("CATALOG_VARIANT_CONFLICT", "another product in the group has the same axis values")
	ErrProductAlreadyGrouped = kernel.Conflict("CATALOG_PRODUCT_ALREADY_GROUPED", "product already belongs to a variant group")
	ErrOfferExists           = kernel.Conflict("CATALOG_OFFER_EXISTS", "seller already has an offer for this product")
	ErrSellerSKUTaken        = kernel.Conflict("CATALOG_SELLER_SKU_TAKEN", "seller SKU is already used by another offer")

	ErrNotProductAuthor = kernel.Forbidden("CATALOG_NOT_PRODUCT_AUTHOR", "product belongs to another seller")
	ErrNotVariantOwner  = kernel.Forbidden("CATALOG_NOT_VARIANT_OWNER", "variant group belongs to another seller")
	ErrNotOfferOwner    = kernel.Forbidden("CATALOG_NOT_OFFER_OWNER", "offer belongs to another seller")

	ErrCategoryTooDeep         = kernel.BusinessRule("CATALOG_CATEGORY_TOO_DEEP", "category tree depth limit exceeded")
	ErrBrokenCategoryChain     = kernel.BusinessRule("CATALOG_BROKEN_CATEGORY_CHAIN", "category chain is inconsistent")
	ErrCategoryMismatch        = kernel.BusinessRule("CATALOG_CATEGORY_MISMATCH", "classification belongs to another category")
	ErrProductLocked           = kernel.BusinessRule("CATALOG_PRODUCT_LOCKED", "product can be edited only in draft or rejected status")
	ErrProductIncomplete       = kernel.BusinessRule("CATALOG_PRODUCT_INCOMPLETE", "required category attributes are missing")
	ErrProductNotPublished     = kernel.BusinessRule("CATALOG_PRODUCT_NOT_PUBLISHED", "offers can be created only for published products")
	ErrTooManyImages           = kernel.BusinessRule("CATALOG_TOO_MANY_IMAGES", "a product can have at most 15 images")
	ErrImageMismatch           = kernel.BusinessRule("CATALOG_IMAGE_MISMATCH", "uploaded file does not match the declared image")
	ErrImageNotUploaded        = kernel.BusinessRule("CATALOG_IMAGE_NOT_UPLOADED", "image upload is not confirmed")
	ErrImageAlreadyUploaded    = kernel.BusinessRule("CATALOG_IMAGE_ALREADY_UPLOADED", "image upload is already confirmed")
	ErrVariantCategoryMismatch = kernel.BusinessRule("CATALOG_VARIANT_CATEGORY_MISMATCH", "product category differs from the variant group category")
	ErrVariantAxisMissing      = kernel.BusinessRule("CATALOG_VARIANT_AXIS_MISSING", "product has no value for a variant axis")
	ErrVariantMemberNotFound   = kernel.BusinessRule("CATALOG_VARIANT_MEMBER_NOT_FOUND", "product is not a member of the variant group")
	ErrOfferArchived           = kernel.BusinessRule("CATALOG_OFFER_ARCHIVED", "archived offer cannot be changed")
	ErrImportTooLarge          = kernel.BusinessRule("CATALOG_IMPORT_TOO_LARGE", "import exceeds 50000 rows")
	ErrImportNotRunning        = kernel.BusinessRule("CATALOG_IMPORT_NOT_RUNNING", "import job is not running")
	ErrImportNotPending        = kernel.BusinessRule("CATALOG_IMPORT_NOT_PENDING", "import job is not pending")
)

type TransitionError struct {
	Entity string
	From   string
	To     string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s cannot transition from %s to %s", e.Entity, e.From, e.To)
}

func (e *TransitionError) Is(target error) bool {
	_, ok := target.(*TransitionError)
	return ok
}

func (e *TransitionError) Kind() kernel.ErrorKind { return kernel.KindConflict }

func (e *TransitionError) Code() string { return "CATALOG_INVALID_TRANSITION" }

func (e *TransitionError) Message() string { return e.Error() }
