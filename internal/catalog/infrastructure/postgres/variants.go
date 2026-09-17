package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type variantGroupRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r variantGroupRepository) FindByID(ctx context.Context, id domain.VariantGroupID) (*domain.VariantGroup, error) {
	snap, err := loadVariantGroup(ctx, r.q, id.String())
	if err != nil {
		return nil, err
	}
	return domain.RehydrateVariantGroup(snap)
}

func (r variantGroupRepository) Save(ctx context.Context, group *domain.VariantGroup) error {
	snap := group.Snapshot()
	var err error
	if snap.Version == 0 {
		err = exec(ctx, r.q, "insert variant group", `
			INSERT INTO catalog.variant_groups (id, category_id, seller_id, axes, created_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, 1)`,
			snap.ID, snap.CategoryID, snap.SellerID, snap.Axes, utc(snap.CreatedAt), utc(snap.UpdatedAt))
	} else {
		err = update(ctx, r.q, "update variant group", `
			UPDATE catalog.variant_groups SET updated_at = $2, version = version + 1 WHERE id = $1 AND version = $3`,
			snap.ID, utc(snap.UpdatedAt), snap.Version)
	}
	if err != nil {
		return err
	}
	if err := replaceVariantMembers(ctx, r.q, snap); err != nil {
		return err
	}
	group.AdvanceVersion()
	return r.events.Write(ctx, r.q, group.PullEvents())
}

func replaceVariantMembers(ctx context.Context, q platform.Querier, snap domain.VariantGroupSnapshot) error {
	if err := exec(ctx, q, "delete variant members",
		`DELETE FROM catalog.variant_members WHERE group_id = $1`, snap.ID); err != nil {
		return err
	}
	for i, m := range snap.Members {
		values, err := json.Marshal(m.AxisValues)
		if err != nil {
			return fmt.Errorf("encode axis values: %w", err)
		}
		err = exec(ctx, q, "insert variant member", `
			INSERT INTO catalog.variant_members (group_id, product_id, axis_values, position) VALUES ($1, $2, $3, $4)`,
			snap.ID, m.ProductID, values, i)
		if err != nil {
			return err
		}
	}
	return nil
}

func loadVariantGroup(ctx context.Context, q platform.Querier, id string) (domain.VariantGroupSnapshot, error) {
	var snap domain.VariantGroupSnapshot
	err := q.QueryRow(ctx, `
		SELECT id::text, category_id::text, seller_id::text, axes, created_at, updated_at, version
		FROM catalog.variant_groups WHERE id = $1`, id).
		Scan(&snap.ID, &snap.CategoryID, &snap.SellerID, &snap.Axes, &snap.CreatedAt, &snap.UpdatedAt, &snap.Version)
	if err != nil {
		return domain.VariantGroupSnapshot{}, notFound(err, domain.ErrVariantGroupNotFound)
	}
	snap.CreatedAt, snap.UpdatedAt = utc(snap.CreatedAt), utc(snap.UpdatedAt)
	if snap.Members, err = loadVariantMembers(ctx, q, id); err != nil {
		return domain.VariantGroupSnapshot{}, err
	}
	return snap, nil
}

func loadVariantMembers(ctx context.Context, q platform.Querier, groupID string) ([]domain.VariantMemberSnapshot, error) {
	rows, err := q.Query(ctx, `
		SELECT product_id::text, axis_values FROM catalog.variant_members WHERE group_id = $1 ORDER BY position`, groupID)
	if err != nil {
		return nil, fmt.Errorf("select variant members: %w", err)
	}
	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.VariantMemberSnapshot, error) {
		var m domain.VariantMemberSnapshot
		return m, row.Scan(&m.ProductID, &m.AxisValues)
	})
	if err != nil {
		return nil, fmt.Errorf("scan variant members: %w", err)
	}
	return members, nil
}
