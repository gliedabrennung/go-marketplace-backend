package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

type orderTag struct{}

type OrderID = kernel.ID[orderTag]

func NewOrderID() OrderID { return kernel.NewID[orderTag]() }

func ParseOrderID(s string) (OrderID, error) { return kernel.ParseID[orderTag](s) }

func NewReference() string { return kernel.NewID[struct{}]().String() }
