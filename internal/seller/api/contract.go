package api

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
)

const (
	RoleSellerAdmin    = "seller_admin"
	RoleSellerOperator = "seller_operator"
)

var ErrSellerNotFound = domain.ErrSellerNotFound

type SellerInfo struct {
	ID             string
	Status         string
	CanSell        bool
	PayoutsAllowed bool
}

type Directory interface {
	Seller(ctx context.Context, sellerID string) (SellerInfo, error)
	MemberRole(ctx context.Context, sellerID, userID string) (string, bool, error)
	CommissionRate(ctx context.Context, sellerID, categoryID string) (int, error)
}
