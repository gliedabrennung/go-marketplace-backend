package query

import (
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var ErrUnknownStatus = kernel.Validation("SELLER_UNKNOWN_STATUS", "seller status filter is unknown")

var knownStatuses = map[domain.SellerStatus]struct{}{
	domain.StatusDraft:         {},
	domain.StatusPendingReview: {},
	domain.StatusActive:        {},
	domain.StatusSuspended:     {},
	domain.StatusTerminated:    {},
}
