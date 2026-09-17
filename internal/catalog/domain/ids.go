package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

type (
	categoryTag     struct{}
	productTag      struct{}
	imageTag        struct{}
	variantGroupTag struct{}
	offerTag        struct{}
	importJobTag    struct{}
)

type (
	CategoryID     = kernel.ID[categoryTag]
	ProductID      = kernel.ID[productTag]
	ImageID        = kernel.ID[imageTag]
	VariantGroupID = kernel.ID[variantGroupTag]
	OfferID        = kernel.ID[offerTag]
	ImportJobID    = kernel.ID[importJobTag]
)

func NewCategoryID() CategoryID { return kernel.NewID[categoryTag]() }

func NewProductID() ProductID { return kernel.NewID[productTag]() }

func NewImageID() ImageID { return kernel.NewID[imageTag]() }

func NewVariantGroupID() VariantGroupID { return kernel.NewID[variantGroupTag]() }

func NewOfferID() OfferID { return kernel.NewID[offerTag]() }

func NewImportJobID() ImportJobID { return kernel.NewID[importJobTag]() }

func ParseCategoryID(s string) (CategoryID, error) { return kernel.ParseID[categoryTag](s) }

func ParseProductID(s string) (ProductID, error) { return kernel.ParseID[productTag](s) }

func ParseImageID(s string) (ImageID, error) { return kernel.ParseID[imageTag](s) }

func ParseVariantGroupID(s string) (VariantGroupID, error) { return kernel.ParseID[variantGroupTag](s) }

func ParseOfferID(s string) (OfferID, error) { return kernel.ParseID[offerTag](s) }

func ParseImportJobID(s string) (ImportJobID, error) { return kernel.ParseID[importJobTag](s) }
