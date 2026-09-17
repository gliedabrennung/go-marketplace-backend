package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
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

func (m *ReadModel) Seller(ctx context.Context, sellerID string) (query.SellerView, error) {
	if _, err := kernel.ParseSellerID(sellerID); err != nil {
		return query.SellerView{}, domain.ErrSellerNotFound
	}
	snap, err := loadSnapshot(ctx, m.db, sellerID)
	if err != nil {
		return query.SellerView{}, err
	}
	return query.NewSellerView(snap), nil
}

func (m *ReadModel) ListByMember(ctx context.Context, userID string) ([]query.SellerSummary, error) {
	rows, err := m.db.Query(ctx, `
		SELECT s.id::text, s.status, s.legal_name, s.tax_id, m.role, s.created_at, s.updated_at
		FROM seller.members m
		JOIN seller.sellers s ON s.id = m.seller_id
		WHERE m.user_id = $1
		ORDER BY s.created_at, s.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list sellers by member: %w", err)
	}
	return collectSummaries(rows)
}

func (m *ReadModel) ListByStatus(ctx context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[query.SellerSummary], error) {
	sql := `SELECT id::text, status, legal_name, tax_id, '', created_at, updated_at FROM seller.sellers WHERE status = $1`
	args := []any{status, limit + 1}
	if after != nil {
		if _, err := kernel.ParseID[struct{}](after.ID); err != nil {
			return pagination.Page[query.SellerSummary]{}, pagination.ErrInvalidCursor
		}
		sql += " AND (updated_at, id) > ($3, $4)"
		args = append(args, after.At, after.ID)
	}
	sql += " ORDER BY updated_at, id LIMIT $2"

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return pagination.Page[query.SellerSummary]{}, fmt.Errorf("list sellers by status: %w", err)
	}
	summaries, err := collectSummaries(rows)
	if err != nil {
		return pagination.Page[query.SellerSummary]{}, err
	}
	return pagination.Build(summaries, limit, func(s query.SellerSummary) pagination.Keyset {
		return pagination.Keyset{At: s.UpdatedAt, ID: s.ID}
	}), nil
}

func collectSummaries(rows pgx.Rows) ([]query.SellerSummary, error) {
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.SellerSummary, error) {
		var s query.SellerSummary
		err := row.Scan(&s.ID, &s.Status, &s.LegalName, &s.TaxID, &s.Role, &s.CreatedAt, &s.UpdatedAt)
		s.CreatedAt, s.UpdatedAt = utc(s.CreatedAt), utc(s.UpdatedAt)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan seller summaries: %w", err)
	}
	return out, nil
}

type Directory struct {
	db     platform.Querier
	policy domain.CommissionPolicy
}

func NewDirectory(db platform.Querier, policy domain.CommissionPolicy) *Directory {
	return &Directory{db: db, policy: policy}
}

func (d *Directory) Seller(ctx context.Context, sellerID string) (api.SellerInfo, error) {
	seller, err := d.load(ctx, sellerID)
	if err != nil {
		return api.SellerInfo{}, err
	}
	return api.SellerInfo{
		ID:             seller.ID().String(),
		Status:         string(seller.Status()),
		CanSell:        seller.CanSell(),
		PayoutsAllowed: seller.PayoutsAllowed(),
	}, nil
}

func (d *Directory) MemberRole(ctx context.Context, sellerID, userID string) (string, bool, error) {
	if _, err := kernel.ParseSellerID(sellerID); err != nil {
		return "", false, nil
	}
	if _, err := kernel.ParseUserID(userID); err != nil {
		return "", false, nil
	}
	var role string
	err := d.db.QueryRow(ctx, "SELECT role FROM seller.members WHERE seller_id = $1 AND user_id = $2", sellerID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("select member role: %w", err)
	}
	return role, true, nil
}

func (d *Directory) CommissionRate(ctx context.Context, sellerID, categoryID string) (int, error) {
	seller, err := d.load(ctx, sellerID)
	if err != nil {
		return 0, err
	}
	category, err := domain.ParseCategoryID(categoryID)
	if err != nil {
		return 0, err
	}
	base, err := commissionRepository{q: d.db}.FindByCategory(ctx, category)
	switch {
	case errors.Is(err, domain.ErrCategoryCommissionNotFound):
		base = nil
	case err != nil:
		return 0, err
	}
	return d.policy.RateFor(seller, category, base).Value(), nil
}

func (d *Directory) load(ctx context.Context, sellerID string) (*domain.Seller, error) {
	id, err := kernel.ParseSellerID(sellerID)
	if err != nil {
		return nil, domain.ErrSellerNotFound
	}
	return sellerRepository{q: d.db}.FindByID(ctx, id)
}
