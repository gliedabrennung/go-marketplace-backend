package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReadModel struct {
	db platform.Querier
}

func NewReadModel(db platform.Querier) *ReadModel {
	return &ReadModel{db: db}
}

func (m *ReadModel) CategoryTree(ctx context.Context) ([]query.CategoryNode, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id::text, COALESCE(parent_id::text, ''), name, slug, cardinality(ancestors) + 1
		FROM catalog.categories
		ORDER BY cardinality(ancestors), name, id`)
	if err != nil {
		return nil, fmt.Errorf("select category tree: %w", err)
	}
	nodes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.CategoryNode, error) {
		var n query.CategoryNode
		return n, row.Scan(&n.ID, &n.ParentID, &n.Name, &n.Slug, &n.Depth)
	})
	if err != nil {
		return nil, fmt.Errorf("scan category tree: %w", err)
	}
	return nodes, nil
}

func (m *ReadModel) Category(ctx context.Context, categoryID string) (query.CategoryView, error) {
	chain, err := loadCategoryChain(ctx, m.db, categoryID)
	if err != nil {
		return query.CategoryView{}, err
	}
	return query.NewCategoryView(chain), nil
}

func (m *ReadModel) Product(ctx context.Context, productID string) (query.ProductView, error) {
	snap, err := loadProduct(ctx, m.db, productID)
	if err != nil {
		return query.ProductView{}, err
	}
	chain, err := loadCategoryChain(ctx, m.db, snap.CategoryID)
	if err != nil {
		return query.ProductView{}, err
	}
	return query.NewProductView(snap, query.NewCategoryView(chain)), nil
}

func (m *ReadModel) ProductVariants(ctx context.Context, productID string, publishedOnly bool) (*query.VariantView, error) {
	rows, err := m.db.Query(ctx, `
		SELECT g.id::text, g.axes, m.product_id::text, m.axis_values, p.status
		FROM catalog.variant_members owner
		JOIN catalog.variant_groups g ON g.id = owner.group_id
		JOIN catalog.variant_members m ON m.group_id = g.id
		JOIN catalog.products p ON p.id = m.product_id
		WHERE owner.product_id = $1
		ORDER BY m.position`, productID)
	if err != nil {
		return nil, fmt.Errorf("select product variants: %w", err)
	}
	defer rows.Close()

	var view *query.VariantView
	for rows.Next() {
		var (
			groupID, memberID, status string
			axes                      []string
			raw                       []byte
		)
		if err := rows.Scan(&groupID, &axes, &memberID, &raw, &status); err != nil {
			return nil, fmt.Errorf("scan product variants: %w", err)
		}
		if view == nil {
			view = &query.VariantView{GroupID: groupID, Axes: axes, Members: []query.VariantMemberView{}}
		}
		if publishedOnly && status != string(domain.ProductStatusPublished) {
			continue
		}
		values := map[string]string{}
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("decode axis values: %w", err)
		}
		view.Members = append(view.Members, query.VariantMemberView{ProductID: memberID, AxisValues: values})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read product variants: %w", err)
	}
	return view, nil
}

func (m *ReadModel) ProductOffers(ctx context.Context, productID, status string) ([]query.OfferView, error) {
	rows, err := m.db.Query(ctx, `SELECT `+offerColumns+`
		FROM catalog.offers WHERE product_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY price_amount, id`, productID, status)
	if err != nil {
		return nil, fmt.Errorf("select product offers: %w", err)
	}
	return collectOffers(rows)
}

func (m *ReadModel) SellerProducts(ctx context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[query.ProductSummary], error) {
	sql := `SELECT id::text, category_id::text, seller_id::text, title, brand, status, created_at, updated_at
		FROM catalog.products WHERE seller_id = $1 AND ($2 = '' OR status = $2)`
	args := []any{sellerID, status, limit + 1}
	if after != nil {
		sql += ` AND (created_at, id) < ($4, $5)`
		args = append(args, after.At, after.ID)
	}
	sql += ` ORDER BY created_at DESC, id DESC LIMIT $3`

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return pagination.Page[query.ProductSummary]{}, fmt.Errorf("select seller products: %w", err)
	}
	summaries, err := collectSummaries(rows)
	if err != nil {
		return pagination.Page[query.ProductSummary]{}, err
	}
	return pagination.Build(summaries, limit, func(v query.ProductSummary) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (m *ReadModel) ModerationQueue(ctx context.Context, limit int, after *pagination.Keyset) (pagination.Page[query.ProductSummary], error) {
	sql := `SELECT id::text, category_id::text, seller_id::text, title, brand, status, created_at, updated_at
		FROM catalog.products WHERE status = 'on_moderation'`
	args := []any{limit + 1}
	if after != nil {
		sql += ` AND (updated_at, id) > ($2, $3)`
		args = append(args, after.At, after.ID)
	}
	sql += ` ORDER BY updated_at, id LIMIT $1`

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return pagination.Page[query.ProductSummary]{}, fmt.Errorf("select moderation queue: %w", err)
	}
	summaries, err := collectSummaries(rows)
	if err != nil {
		return pagination.Page[query.ProductSummary]{}, err
	}
	return pagination.Build(summaries, limit, func(v query.ProductSummary) pagination.Keyset {
		return pagination.Keyset{At: v.UpdatedAt, ID: v.ID}
	}), nil
}

func (m *ReadModel) SellerOffers(ctx context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[query.OfferView], error) {
	sql := `SELECT ` + offerColumns + ` FROM catalog.offers WHERE seller_id = $1 AND ($2 = '' OR status = $2)`
	args := []any{sellerID, status, limit + 1}
	if after != nil {
		sql += ` AND (created_at, id) < ($4, $5)`
		args = append(args, after.At, after.ID)
	}
	sql += ` ORDER BY created_at DESC, id DESC LIMIT $3`

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return pagination.Page[query.OfferView]{}, fmt.Errorf("select seller offers: %w", err)
	}
	offers, err := collectOffers(rows)
	if err != nil {
		return pagination.Page[query.OfferView]{}, err
	}
	return pagination.Build(offers, limit, func(v query.OfferView) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (m *ReadModel) ImportJob(ctx context.Context, jobID string) (query.ImportJobView, error) {
	snap, err := loadImportJob(ctx, m.db, jobID)
	if err != nil {
		return query.ImportJobView{}, err
	}
	return query.NewImportJobView(snap, true), nil
}

func (m *ReadModel) SellerImports(ctx context.Context, sellerID string, limit int, after *pagination.Keyset) (pagination.Page[query.ImportJobView], error) {
	sql := `SELECT id::text, seller_id::text, requested_by::text, format, status, total_rows, succeeded_rows, failed_rows,
			COALESCE(failure_reason, ''), COALESCE(report_key, ''), created_at, started_at, finished_at, updated_at
		FROM catalog.import_jobs WHERE seller_id = $1`
	args := []any{sellerID, limit + 1}
	if after != nil {
		sql += ` AND (created_at, id) < ($3, $4)`
		args = append(args, after.At, after.ID)
	}
	sql += ` ORDER BY created_at DESC, id DESC LIMIT $2`

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return pagination.Page[query.ImportJobView]{}, fmt.Errorf("select seller imports: %w", err)
	}
	jobs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.ImportJobView, error) {
		var (
			v                 query.ImportJobView
			started, finished *time.Time
		)
		err := row.Scan(&v.ID, &v.SellerID, &v.RequestedBy, &v.Format, &v.Status, &v.TotalRows, &v.SucceededRows,
			&v.FailedRows, &v.FailureReason, &v.ReportKey, &v.CreatedAt, &started, &finished, &v.UpdatedAt)
		v.CreatedAt, v.UpdatedAt = utc(v.CreatedAt), utc(v.UpdatedAt)
		v.StartedAt, v.FinishedAt = moment(started), moment(finished)
		return v, err
	})
	if err != nil {
		return pagination.Page[query.ImportJobView]{}, fmt.Errorf("scan seller imports: %w", err)
	}
	return pagination.Build(jobs, limit, func(v query.ImportJobView) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (m *ReadModel) StaleImportIDs(ctx context.Context, before time.Time, limit int) ([]string, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id::text FROM catalog.import_jobs
		WHERE status = 'processing' AND updated_at < $1
		ORDER BY updated_at LIMIT $2`, before.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("select stale imports: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan stale imports: %w", err)
	}
	return ids, nil
}

func collectSummaries(rows pgx.Rows) ([]query.ProductSummary, error) {
	summaries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.ProductSummary, error) {
		var s query.ProductSummary
		err := row.Scan(&s.ID, &s.CategoryID, &s.SellerID, &s.Title, &s.Brand, &s.Status, &s.CreatedAt, &s.UpdatedAt)
		s.CreatedAt, s.UpdatedAt = utc(s.CreatedAt), utc(s.UpdatedAt)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan products: %w", err)
	}
	return summaries, nil
}

func collectOffers(rows pgx.Rows) ([]query.OfferView, error) {
	offers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.OfferView, error) {
		snap, err := scanOffer(row)
		return query.NewOfferView(snap), err
	})
	if err != nil {
		return nil, fmt.Errorf("scan offers: %w", err)
	}
	return offers, nil
}
