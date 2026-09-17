package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const categoryColumns = `c.id::text, COALESCE(c.parent_id::text, ''), c.name, c.slug, c.ancestors::text[], c.created_at, c.updated_at, c.version`

type categoryRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r categoryRepository) FindByID(ctx context.Context, id domain.CategoryID) (*domain.Category, error) {
	snap, err := loadCategory(ctx, r.q, id.String())
	if err != nil {
		return nil, err
	}
	return domain.RehydrateCategory(snap)
}

func (r categoryRepository) FindChain(ctx context.Context, id domain.CategoryID) ([]*domain.Category, error) {
	chain, err := loadCategoryChain(ctx, r.q, id.String())
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Category, 0, len(chain))
	for _, snap := range chain {
		category, err := domain.RehydrateCategory(snap)
		if err != nil {
			return nil, err
		}
		out = append(out, category)
	}
	return out, nil
}

func (r categoryRepository) DescendantAttributeCodes(ctx context.Context, id domain.CategoryID) ([]string, error) {
	rows, err := r.q.Query(ctx, `
		SELECT DISTINCT a.code
		FROM catalog.category_attributes a
		JOIN catalog.categories c ON c.id = a.category_id
		WHERE $1 = ANY (c.ancestors)`, id.String())
	if err != nil {
		return nil, fmt.Errorf("select descendant attributes: %w", err)
	}
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan descendant attributes: %w", err)
	}
	return codes, nil
}

func (r categoryRepository) Save(ctx context.Context, category *domain.Category) error {
	snap := category.Snapshot()
	var err error
	if snap.Version == 0 {
		err = exec(ctx, r.q, "insert category", `
			INSERT INTO catalog.categories (id, parent_id, name, slug, ancestors, created_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5::uuid[], $6, $7, 1)`,
			snap.ID, nullable(snap.ParentID), snap.Name, snap.Slug, list(snap.Ancestors), utc(snap.CreatedAt), utc(snap.UpdatedAt))
	} else {
		err = update(ctx, r.q, "update category", `
			UPDATE catalog.categories SET name = $2, slug = $3, updated_at = $4, version = version + 1
			WHERE id = $1 AND version = $5`,
			snap.ID, snap.Name, snap.Slug, utc(snap.UpdatedAt), snap.Version)
	}
	if err != nil {
		return err
	}
	if err := replaceCategoryAttributes(ctx, r.q, snap); err != nil {
		return err
	}
	category.AdvanceVersion()
	return r.events.Write(ctx, r.q, category.PullEvents())
}

func replaceCategoryAttributes(ctx context.Context, q platform.Querier, snap domain.CategorySnapshot) error {
	if err := exec(ctx, q, "delete category attributes",
		`DELETE FROM catalog.category_attributes WHERE category_id = $1`, snap.ID); err != nil {
		return err
	}
	for i, a := range snap.Attributes {
		err := exec(ctx, q, "insert category attribute", `
			INSERT INTO catalog.category_attributes (category_id, code, name, type, required, filterable, options, unit, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			snap.ID, a.Code, a.Name, a.Type, a.Required, a.Filterable, list(a.Options), a.Unit, i)
		if err != nil {
			return err
		}
	}
	return nil
}

func loadCategory(ctx context.Context, q platform.Querier, id string) (domain.CategorySnapshot, error) {
	row := q.QueryRow(ctx, `SELECT `+categoryColumns+` FROM catalog.categories c WHERE c.id = $1`, id)
	snap, err := scanCategory(row)
	if err != nil {
		return domain.CategorySnapshot{}, notFound(err, domain.ErrCategoryNotFound)
	}
	attributes, err := loadCategoryAttributes(ctx, q, []string{id})
	if err != nil {
		return domain.CategorySnapshot{}, err
	}
	snap.Attributes = attributes[id]
	return snap, nil
}

func loadCategoryChain(ctx context.Context, q platform.Querier, id string) ([]domain.CategorySnapshot, error) {
	rows, err := q.Query(ctx, `
		WITH leaf AS (SELECT id, ancestors FROM catalog.categories WHERE id = $1)
		SELECT `+categoryColumns+`
		FROM catalog.categories c, leaf
		WHERE c.id = leaf.id OR c.id = ANY (leaf.ancestors)
		ORDER BY cardinality(c.ancestors)`, id)
	if err != nil {
		return nil, fmt.Errorf("select category chain: %w", err)
	}
	chain, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CategorySnapshot, error) {
		return scanCategory(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan category chain: %w", err)
	}
	if len(chain) == 0 {
		return nil, domain.ErrCategoryNotFound
	}
	ids := make([]string, len(chain))
	for i, snap := range chain {
		ids[i] = snap.ID
	}
	attributes, err := loadCategoryAttributes(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	for i := range chain {
		chain[i].Attributes = attributes[chain[i].ID]
	}
	return chain, nil
}

func loadCategoryAttributes(ctx context.Context, q platform.Querier, ids []string) (map[string][]domain.AttributeSpec, error) {
	rows, err := q.Query(ctx, `
		SELECT category_id::text, code, name, type, required, filterable, options, unit
		FROM catalog.category_attributes
		WHERE category_id::text = ANY ($1)
		ORDER BY category_id, position`, ids)
	if err != nil {
		return nil, fmt.Errorf("select category attributes: %w", err)
	}
	defer rows.Close()
	out := map[string][]domain.AttributeSpec{}
	for rows.Next() {
		var (
			categoryID string
			spec       domain.AttributeSpec
		)
		if err := rows.Scan(&categoryID, &spec.Code, &spec.Name, &spec.Type, &spec.Required, &spec.Filterable, &spec.Options, &spec.Unit); err != nil {
			return nil, fmt.Errorf("scan category attribute: %w", err)
		}
		out[categoryID] = append(out[categoryID], spec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read category attributes: %w", err)
	}
	return out, nil
}

func scanCategory(row pgx.Row) (domain.CategorySnapshot, error) {
	var snap domain.CategorySnapshot
	err := row.Scan(&snap.ID, &snap.ParentID, &snap.Name, &snap.Slug, &snap.Ancestors, &snap.CreatedAt, &snap.UpdatedAt, &snap.Version)
	snap.CreatedAt, snap.UpdatedAt = utc(snap.CreatedAt), utc(snap.UpdatedAt)
	return snap, err
}
