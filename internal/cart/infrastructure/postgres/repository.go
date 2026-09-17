package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type repositories struct {
	q platform.Querier
}

func NewRepositories(q platform.Querier) application.Repositories {
	return repositories{q: q}
}

func NewUnitOfWork(pool *pgxpool.Pool) *platform.UnitOfWork[application.Repositories] {
	return platform.NewUnitOfWork(pool, func(tx pgx.Tx) application.Repositories {
		return repositories{q: tx}
	})
}

func (r repositories) Carts() domain.Repository { return Repository(r) }

type Repository struct {
	q platform.Querier
}

func NewRepository(q platform.Querier) Repository {
	return Repository{q: q}
}

func (r Repository) FindByOwner(ctx context.Context, owner domain.Owner) (*domain.Cart, error) {
	var (
		s       domain.CartSnapshot
		expires *time.Time
	)
	err := r.q.QueryRow(ctx, `
		SELECT id::text, owner_kind, owner_id, currency, promo_code, created_at, updated_at, expires_at, version
		FROM cart.carts WHERE owner_kind = $1 AND owner_id = $2`, string(owner.Kind()), owner.ID()).
		Scan(&s.ID, &s.OwnerKind, &s.OwnerID, &s.Currency, &s.PromoCode, &s.CreatedAt, &s.UpdatedAt, &expires, &s.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCartNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select cart: %w", err)
	}
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	if expires != nil {
		s.ExpiresAt = expires.UTC()
	}
	rows, err := r.q.Query(ctx, `SELECT sku, seller_id::text, quantity, price, added_at, updated_at
		FROM cart.items WHERE cart_id = $1 ORDER BY position, added_at`, s.ID)
	if err != nil {
		return nil, fmt.Errorf("select cart items: %w", err)
	}
	s.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ItemSnapshot, error) {
		var item domain.ItemSnapshot
		err := row.Scan(&item.SKU, &item.SellerID, &item.Quantity, &item.Price, &item.AddedAt, &item.UpdatedAt)
		item.AddedAt, item.UpdatedAt = item.AddedAt.UTC(), item.UpdatedAt.UTC()
		return item, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan cart items: %w", err)
	}
	return domain.Rehydrate(s)
}

func (r Repository) Save(ctx context.Context, cart *domain.Cart) error {
	s := cart.Snapshot()
	if err := r.upsert(ctx, s); err != nil {
		return err
	}
	if _, err := r.q.Exec(ctx, `DELETE FROM cart.items WHERE cart_id = $1`, s.ID); err != nil {
		return fmt.Errorf("clear cart items: %w", err)
	}
	if len(s.Items) > 0 {
		var (
			skus, sellers         []string
			quantities, positions []int
			prices                []int64
			added, updated        []time.Time
		)
		for position, item := range s.Items {
			skus, sellers = append(skus, item.SKU), append(sellers, item.SellerID)
			quantities, positions = append(quantities, item.Quantity), append(positions, position)
			prices = append(prices, item.Price)
			added, updated = append(added, item.AddedAt.UTC()), append(updated, item.UpdatedAt.UTC())
		}
		if _, err := r.q.Exec(ctx, `
			INSERT INTO cart.items (cart_id, sku, seller_id, quantity, price, position, added_at, updated_at)
			SELECT $1, * FROM unnest($2::text[], $3::uuid[], $4::int[], $5::bigint[], $6::int[], $7::timestamptz[], $8::timestamptz[])`,
			s.ID, skus, sellers, quantities, prices, positions, added, updated); err != nil {
			return fmt.Errorf("insert cart items: %w", err)
		}
	}
	cart.AdvanceVersion()
	return nil
}

func (r Repository) upsert(ctx context.Context, s domain.CartSnapshot) error {
	var expires *time.Time
	if !s.ExpiresAt.IsZero() {
		value := s.ExpiresAt.UTC()
		expires = &value
	}
	if s.Version == 0 {
		_, err := r.q.Exec(ctx, `
			INSERT INTO cart.carts (id, owner_kind, owner_id, currency, promo_code, created_at, updated_at, expires_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1)`,
			s.ID, s.OwnerKind, s.OwnerID, s.Currency, s.PromoCode, s.CreatedAt.UTC(), s.UpdatedAt.UTC(), expires)
		if _, ok := platform.UniqueViolation(err); ok {
			return kernel.ErrConcurrentModification
		}
		if err != nil {
			return fmt.Errorf("insert cart: %w", err)
		}
		return nil
	}
	tag, err := r.q.Exec(ctx, `
		UPDATE cart.carts SET currency = $2, promo_code = $3, updated_at = $4, expires_at = $5, version = version + 1
		WHERE id = $1 AND version = $6`, s.ID, s.Currency, s.PromoCode, s.UpdatedAt.UTC(), expires, s.Version)
	if err != nil {
		return fmt.Errorf("update cart: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}

func (r Repository) Delete(ctx context.Context, cart *domain.Cart) error {
	tag, err := r.q.Exec(ctx, `DELETE FROM cart.carts WHERE id = $1 AND version = $2`, cart.ID().String(), cart.Version())
	if err != nil {
		return fmt.Errorf("delete cart: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}

func (r Repository) DeleteExpired(ctx context.Context, before time.Time, limit int) (int, error) {
	tag, err := r.q.Exec(ctx, `
		DELETE FROM cart.carts WHERE id IN (
			SELECT id FROM cart.carts WHERE expires_at < $1 ORDER BY expires_at LIMIT $2 FOR UPDATE SKIP LOCKED)`,
		before.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired carts: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
