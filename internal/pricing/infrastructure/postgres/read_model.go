package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReadModel struct {
	db platform.Querier
}

func NewReadModel(db platform.Querier) *ReadModel {
	return &ReadModel{db: db}
}

func (m *ReadModel) Prices(ctx context.Context, skus []string) ([]*domain.OfferPrice, error) {
	rows, err := m.db.Query(ctx, `SELECT `+offerPriceColumns+` FROM pricing.offer_prices WHERE sku = ANY ($1)`, skus)
	if err != nil {
		return nil, fmt.Errorf("select offer prices: %w", err)
	}
	snaps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.OfferPriceSnapshot, error) {
		return scanOfferPrice(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan offer prices: %w", err)
	}
	out := make([]*domain.OfferPrice, 0, len(snaps))
	for _, snap := range snaps {
		price, err := domain.RehydrateOfferPrice(snap)
		if err != nil {
			return nil, err
		}
		out = append(out, price)
	}
	return out, nil
}

func (m *ReadModel) CategoryPaths(ctx context.Context, productIDs []string) (map[string][]string, error) {
	return categoryPaths(ctx, m.db, productIDs)
}

func (m *ReadModel) RunningPromotions(ctx context.Context, at time.Time) ([]*domain.Promotion, error) {
	rows, err := m.db.Query(ctx, `SELECT `+promotionColumns+` FROM pricing.promotions
		WHERE status = 'active' AND (starts_at IS NULL OR starts_at <= $1) AND (ends_at IS NULL OR ends_at > $1)
		ORDER BY priority DESC, id`, at.UTC())
	if err != nil {
		return nil, fmt.Errorf("select running promotions: %w", err)
	}
	snaps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PromotionSnapshot, error) {
		return scanPromotion(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan running promotions: %w", err)
	}
	out := make([]*domain.Promotion, 0, len(snaps))
	for _, snap := range snaps {
		promotion, err := domain.RehydratePromotion(snap)
		if err != nil {
			return nil, err
		}
		out = append(out, promotion)
	}
	return out, nil
}

func (m *ReadModel) FindPromoCode(ctx context.Context, code domain.Code) (*domain.PromoCode, error) {
	return promoCodeRepository{q: m.db}.FindByCode(ctx, code)
}

func (m *ReadModel) PromoCodeUsage(ctx context.Context, code domain.Code, customer kernel.UserID) (int, error) {
	return customerUsage(ctx, m.db, code, customer)
}

func (m *ReadModel) Promotion(ctx context.Context, promotionID string) (query.PromotionView, error) {
	snap, err := scanPromotion(m.db.QueryRow(ctx, `SELECT `+promotionColumns+` FROM pricing.promotions WHERE id = $1`, promotionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return query.PromotionView{}, domain.ErrPromotionNotFound
	}
	if err != nil {
		return query.PromotionView{}, fmt.Errorf("select promotion: %w", err)
	}
	return query.NewPromotionView(snap), nil
}

func (m *ReadModel) Promotions(ctx context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[query.PromotionView], error) {
	sql := `SELECT ` + promotionColumns + ` FROM pricing.promotions WHERE ($1 = '' OR status = $1)`
	args := []any{status, limit + 1}
	if after != nil {
		sql += ` AND (created_at, id) < ($3, $4::uuid)`
		args = append(args, after.At, after.ID)
	}
	sql += ` ORDER BY created_at DESC, id DESC LIMIT $2`

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return pagination.Page[query.PromotionView]{}, fmt.Errorf("select promotions: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.PromotionView, error) {
		snap, err := scanPromotion(row)
		return query.NewPromotionView(snap), err
	})
	if err != nil {
		return pagination.Page[query.PromotionView]{}, fmt.Errorf("scan promotions: %w", err)
	}
	return pagination.Build(views, limit, func(v query.PromotionView) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (m *ReadModel) PromoCode(ctx context.Context, code string) (query.PromoCodeView, error) {
	snap, err := scanPromoCode(m.db.QueryRow(ctx, `SELECT `+promoCodeColumns+` FROM pricing.promo_codes WHERE code = $1`, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return query.PromoCodeView{}, domain.ErrPromoCodeNotFound
	}
	if err != nil {
		return query.PromoCodeView{}, fmt.Errorf("select promo code: %w", err)
	}
	return query.NewPromoCodeView(snap), nil
}
