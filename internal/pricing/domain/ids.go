package domain

import (
	"regexp"
	"strings"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type (
	promotionTag struct{}
	productTag   struct{}
	categoryTag  struct{}
	orderTag     struct{}
)

type (
	PromotionID = kernel.ID[promotionTag]
	ProductID   = kernel.ID[productTag]
	CategoryID  = kernel.ID[categoryTag]
	OrderID     = kernel.ID[orderTag]
)

func NewPromotionID() PromotionID { return kernel.NewID[promotionTag]() }

func ParsePromotionID(s string) (PromotionID, error) { return kernel.ParseID[promotionTag](s) }

func NewProductID() ProductID { return kernel.NewID[productTag]() }

func ParseProductID(s string) (ProductID, error) { return kernel.ParseID[productTag](s) }

func NewCategoryID() CategoryID { return kernel.NewID[categoryTag]() }

func ParseCategoryID(s string) (CategoryID, error) { return kernel.ParseID[categoryTag](s) }

func NewOrderID() OrderID { return kernel.NewID[orderTag]() }

func ParseOrderID(s string) (OrderID, error) { return kernel.ParseID[orderTag](s) }

var (
	skuPattern  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	codePattern = regexp.MustCompile(`^[A-Z0-9-]{4,32}$`)
)

type SKU struct {
	value string
}

func NewSKU(raw string) (SKU, error) {
	value := strings.TrimSpace(raw)
	if !skuPattern.MatchString(value) {
		return SKU{}, ErrInvalidSKU.WithDetail("%q", raw)
	}
	return SKU{value: value}, nil
}

func (s SKU) String() string { return s.value }

func (s SKU) IsZero() bool { return s.value == "" }

type Code struct {
	value string
}

func NewCode(raw string) (Code, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if !codePattern.MatchString(value) {
		return Code{}, ErrInvalidPromoCode.WithDetail("%q", raw)
	}
	return Code{value: value}, nil
}

func (c Code) String() string { return c.value }

func (c Code) IsZero() bool { return c.value == "" }
