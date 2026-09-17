package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

type (
	paymentTag struct{}
	refundTag  struct{}
	methodTag  struct{}
	orderTag   struct{}
)

type (
	PaymentID = kernel.ID[paymentTag]
	RefundID  = kernel.ID[refundTag]
	MethodID  = kernel.ID[methodTag]
	OrderID   = kernel.ID[orderTag]
)

func NewPaymentID() PaymentID { return kernel.NewID[paymentTag]() }

func ParsePaymentID(s string) (PaymentID, error) { return kernel.ParseID[paymentTag](s) }

func NewRefundID() RefundID { return kernel.NewID[refundTag]() }

func ParseRefundID(s string) (RefundID, error) { return kernel.ParseID[refundTag](s) }

func NewMethodID() MethodID { return kernel.NewID[methodTag]() }

func ParseMethodID(s string) (MethodID, error) { return kernel.ParseID[methodTag](s) }

func NewOrderID() OrderID { return kernel.NewID[orderTag]() }

func ParseOrderID(s string) (OrderID, error) { return kernel.ParseID[orderTag](s) }
