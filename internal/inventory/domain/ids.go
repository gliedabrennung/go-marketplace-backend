package domain

import (
	"regexp"
	"strings"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type (
	reservationTag struct{}
	orderTag       struct{}
)

type (
	ReservationID = kernel.ID[reservationTag]
	OrderID       = kernel.ID[orderTag]
)

func NewReservationID() ReservationID { return kernel.NewID[reservationTag]() }

func ParseReservationID(s string) (ReservationID, error) { return kernel.ParseID[reservationTag](s) }

func ParseOrderID(s string) (OrderID, error) { return kernel.ParseID[orderTag](s) }

var skuPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

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
