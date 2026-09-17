package query

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

var ErrInvalidPeriod = kernel.Validation("SETTLEMENT_INVALID_PERIOD", "period must be formatted as YYYY-MM")

type EntryView struct {
	ID         string
	OrderID    string
	Currency   string
	Gross      int64
	Commission int64
	Net        int64
	AccruedAt  time.Time
}

type Report struct {
	SellerID   string
	From       time.Time
	To         time.Time
	Currency   string
	Gross      int64
	Commission int64
	Net        int64
	Entries    []EntryView
}

type ReadModel interface {
	ListBySeller(ctx context.Context, sellerID string, from, to time.Time) ([]EntryView, error)
}

type GetSellerSettlements struct {
	Actor    auth.Principal
	SellerID string
	Period   string
}

type GetSellerSettlementsHandler struct {
	reader  ReadModel
	sellers application.CommissionRates
}

func NewGetSellerSettlementsHandler(reader ReadModel, sellers application.CommissionRates) *GetSellerSettlementsHandler {
	return &GetSellerSettlementsHandler{reader: reader, sellers: sellers}
}

func ParsePeriod(period string) (time.Time, time.Time, error) {
	from, err := time.Parse("2006-01", period)
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvalidPeriod
	}
	from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, 0), nil
}

func (h *GetSellerSettlementsHandler) Handle(ctx context.Context, q GetSellerSettlements) (Report, error) {
	if q.Actor.UserID == "" {
		return Report{}, auth.ErrUnauthenticated
	}
	if _, err := kernel.ParseSellerID(q.SellerID); err != nil {
		return Report{}, ErrNotMember
	}
	if !identity.Can(q.Actor, identity.PermOrdersSupport) {
		_, member, err := h.sellers.MemberRole(ctx, q.SellerID, q.Actor.UserID)
		if err != nil {
			return Report{}, err
		}
		if !member {
			return Report{}, ErrNotMember
		}
	}
	from, to, err := ParsePeriod(q.Period)
	if err != nil {
		return Report{}, err
	}
	entries, err := h.reader.ListBySeller(ctx, q.SellerID, from, to)
	if err != nil {
		return Report{}, err
	}
	report := Report{SellerID: q.SellerID, From: from, To: to, Entries: entries}
	for _, entry := range entries {
		report.Currency = entry.Currency
		report.Gross += entry.Gross
		report.Commission += entry.Commission
		report.Net += entry.Net
	}
	return report, nil
}

var ErrNotMember = kernel.NotFound("SETTLEMENT_SELLER_NOT_FOUND", "seller not found")
