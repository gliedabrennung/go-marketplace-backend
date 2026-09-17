package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type repositories struct {
	q      platform.Querier
	events *outbox.Writer
}

func NewOutboxWriter() *outbox.Writer {
	return outbox.NewWriter("platform", "outbox", NewEventCodec())
}

func NewRepositories(q platform.Querier, events *outbox.Writer) application.Repositories {
	return repositories{q: q, events: events}
}

func NewUnitOfWork(pool *pgxpool.Pool, events *outbox.Writer) *platform.UnitOfWork[application.Repositories] {
	return platform.NewUnitOfWork(pool, func(tx pgx.Tx) application.Repositories {
		return repositories{q: tx, events: events}
	})
}

func (r repositories) Promotions() domain.PromotionRepository { return promotionRepository(r) }

func (r repositories) PromoCodes() domain.PromoCodeRepository { return promoCodeRepository(r) }

func (r repositories) OfferPrices() domain.OfferPriceRepository { return offerPriceRepository(r) }

func (r repositories) Categories() application.CategoryIndex { return categoryIndex{q: r.q} }

func versioned(ctx context.Context, q platform.Querier, version int, insert, update string, insertArgs, updateArgs []any) error {
	if version == 0 {
		_, err := q.Exec(ctx, insert, insertArgs...)
		return err
	}
	tag, err := q.Exec(ctx, update, updateArgs...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}

const promotionColumns = `id::text, name, kind, basis_points, amount, currency, buy_quantity, free_units,
	target_skus, target_sellers::text[], target_categories::text[], priority, exclusive, status,
	starts_at, ends_at, created_at, updated_at, version`

type promotionRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r promotionRepository) FindByID(ctx context.Context, id domain.PromotionID) (*domain.Promotion, error) {
	snap, err := scanPromotion(r.q.QueryRow(ctx, `SELECT `+promotionColumns+` FROM pricing.promotions WHERE id = $1`, id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPromotionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select promotion: %w", err)
	}
	return domain.RehydratePromotion(snap)
}

func (r promotionRepository) Save(ctx context.Context, promotion *domain.Promotion) error {
	s := promotion.Snapshot()
	err := versioned(ctx, r.q, s.Version, `
		INSERT INTO pricing.promotions (id, name, kind, basis_points, amount, currency, buy_quantity, free_units,
			target_skus, target_sellers, target_categories, priority, exclusive, status, starts_at, ends_at,
			created_at, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::uuid[], $11::uuid[], $12, $13, $14, $15, $16, $17, $18, 1)`, `
		UPDATE pricing.promotions SET name = $2, kind = $3, basis_points = $4, amount = $5, currency = $6,
			buy_quantity = $7, free_units = $8, target_skus = $9, target_sellers = $10::uuid[],
			target_categories = $11::uuid[], priority = $12, exclusive = $13, status = $14, starts_at = $15,
			ends_at = $16, updated_at = $17, version = version + 1
		WHERE id = $1 AND version = $18`,
		[]any{s.ID, s.Name, s.Kind, s.BasisPoints, s.Amount, s.Currency, s.BuyQuantity, s.FreeUnits,
			s.SKUs, s.Sellers, s.Categories, s.Priority, s.Exclusive, s.Status, optionalTime(s.StartsAt),
			optionalTime(s.EndsAt), s.CreatedAt.UTC(), s.UpdatedAt.UTC()},
		[]any{s.ID, s.Name, s.Kind, s.BasisPoints, s.Amount, s.Currency, s.BuyQuantity, s.FreeUnits,
			s.SKUs, s.Sellers, s.Categories, s.Priority, s.Exclusive, s.Status, optionalTime(s.StartsAt),
			optionalTime(s.EndsAt), s.UpdatedAt.UTC(), s.Version})
	if err != nil {
		return wrap("save promotion", err)
	}
	promotion.AdvanceVersion()
	return r.events.Write(ctx, r.q, promotion.PullEvents())
}

func scanPromotion(row pgx.Row) (domain.PromotionSnapshot, error) {
	var (
		s                domain.PromotionSnapshot
		startsAt, endsAt *time.Time
	)
	err := row.Scan(&s.ID, &s.Name, &s.Kind, &s.BasisPoints, &s.Amount, &s.Currency, &s.BuyQuantity, &s.FreeUnits,
		&s.SKUs, &s.Sellers, &s.Categories, &s.Priority, &s.Exclusive, &s.Status,
		&startsAt, &endsAt, &s.CreatedAt, &s.UpdatedAt, &s.Version)
	if err != nil {
		return domain.PromotionSnapshot{}, err
	}
	s.StartsAt, s.EndsAt = moment(startsAt), moment(endsAt)
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return s, nil
}

const promoCodeColumns = `code, kind, basis_points, amount, currency, min_cart_amount, total_limit, per_customer_limit,
	used, status, starts_at, ends_at, created_at, updated_at, version`

type promoCodeRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r promoCodeRepository) FindByCode(ctx context.Context, code domain.Code) (*domain.PromoCode, error) {
	snap, err := scanPromoCode(r.q.QueryRow(ctx, `SELECT `+promoCodeColumns+` FROM pricing.promo_codes WHERE code = $1`, code.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPromoCodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select promo code: %w", err)
	}
	return domain.RehydratePromoCode(snap)
}

func (r promoCodeRepository) CustomerUsage(ctx context.Context, code domain.Code, customer kernel.UserID) (int, error) {
	return customerUsage(ctx, r.q, code, customer)
}

func customerUsage(ctx context.Context, q platform.Querier, code domain.Code, customer kernel.UserID) (int, error) {
	var count int
	err := q.QueryRow(ctx, `SELECT count(*) FROM pricing.promo_redemptions WHERE code = $1 AND customer_id = $2`,
		code.String(), customer.String()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count promo code usage: %w", err)
	}
	return count, nil
}

func (r promoCodeRepository) Redeemed(ctx context.Context, code domain.Code, order domain.OrderID) (bool, error) {
	var exists bool
	err := r.q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pricing.promo_redemptions WHERE code = $1 AND order_id = $2)`,
		code.String(), order.String()).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check promo redemption: %w", err)
	}
	return exists, nil
}

func (r promoCodeRepository) Save(ctx context.Context, promo *domain.PromoCode) error {
	s := promo.Snapshot()
	if s.Version == 0 {
		if err := r.insert(ctx, s); err != nil {
			return err
		}
	}
	if err := r.recordRedemptions(ctx, s.Code, promo.PullRedemptions(), promo.PullReleases()); err != nil {
		return err
	}
	if s.Version > 0 {
		tag, err := r.q.Exec(ctx, `
			UPDATE pricing.promo_codes SET used = $2, status = $3, updated_at = $4, version = version + 1
			WHERE code = $1 AND version = $5`, s.Code, s.Used, s.Status, s.UpdatedAt.UTC(), s.Version)
		if err != nil {
			return wrap("update promo code", err)
		}
		if tag.RowsAffected() == 0 {
			return kernel.ErrConcurrentModification
		}
	}
	promo.AdvanceVersion()
	return r.events.Write(ctx, r.q, promo.PullEvents())
}

func (r promoCodeRepository) insert(ctx context.Context, s domain.PromoCodeSnapshot) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO pricing.promo_codes (code, kind, basis_points, amount, currency, min_cart_amount, total_limit,
			per_customer_limit, used, status, starts_at, ends_at, created_at, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 1)`,
		s.Code, s.Kind, s.BasisPoints, s.Amount, s.Currency, s.MinCartAmount, s.TotalLimit, s.PerCustomerLimit,
		s.Used, s.Status, optionalTime(s.StartsAt), optionalTime(s.EndsAt), s.CreatedAt.UTC(), s.UpdatedAt.UTC())
	if err != nil {
		return wrap("insert promo code", err)
	}
	return nil
}

func (r promoCodeRepository) recordRedemptions(ctx context.Context, code string, redemptions []domain.Redemption, releases []domain.OrderID) error {
	for _, redemption := range redemptions {
		_, err := r.q.Exec(ctx, `
			INSERT INTO pricing.promo_redemptions (code, order_id, customer_id, amount, currency, redeemed_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			code, redemption.OrderID.String(), redemption.CustomerID.String(), redemption.Amount.Amount(),
			string(redemption.Amount.Currency()), redemption.At.UTC())
		if err != nil {
			return wrap("record promo redemption", err)
		}
	}
	for _, order := range releases {
		if _, err := r.q.Exec(ctx, `DELETE FROM pricing.promo_redemptions WHERE code = $1 AND order_id = $2`,
			code, order.String()); err != nil {
			return fmt.Errorf("release promo redemption: %w", err)
		}
	}
	return nil
}

func scanPromoCode(row pgx.Row) (domain.PromoCodeSnapshot, error) {
	var (
		s                domain.PromoCodeSnapshot
		startsAt, endsAt *time.Time
	)
	err := row.Scan(&s.Code, &s.Kind, &s.BasisPoints, &s.Amount, &s.Currency, &s.MinCartAmount, &s.TotalLimit,
		&s.PerCustomerLimit, &s.Used, &s.Status, &startsAt, &endsAt, &s.CreatedAt, &s.UpdatedAt, &s.Version)
	if err != nil {
		return domain.PromoCodeSnapshot{}, err
	}
	s.StartsAt, s.EndsAt = moment(startsAt), moment(endsAt)
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return s, nil
}

const offerPriceColumns = `sku, product_id::text, seller_id::text, amount, COALESCE(compare_at, 0), currency, active, updated_at, version`

type offerPriceRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r offerPriceRepository) FindBySKU(ctx context.Context, sku domain.SKU) (*domain.OfferPrice, error) {
	snap, err := scanOfferPrice(r.q.QueryRow(ctx, `SELECT `+offerPriceColumns+` FROM pricing.offer_prices WHERE sku = $1`, sku.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrOfferPriceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select offer price: %w", err)
	}
	return domain.RehydrateOfferPrice(snap)
}

func (r offerPriceRepository) Save(ctx context.Context, price *domain.OfferPrice) error {
	s := price.Snapshot()
	compareAt := optionalAmount(s.CompareAt)
	err := versioned(ctx, r.q, s.Version, `
		INSERT INTO pricing.offer_prices (sku, product_id, seller_id, amount, compare_at, currency, active, updated_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1)`, `
		UPDATE pricing.offer_prices SET amount = $2, compare_at = $3, currency = $4, active = $5, updated_at = $6,
			version = version + 1
		WHERE sku = $1 AND version = $7`,
		[]any{s.SKU, s.ProductID, s.SellerID, s.Amount, compareAt, s.Currency, s.Active, s.UpdatedAt.UTC()},
		[]any{s.SKU, s.Amount, compareAt, s.Currency, s.Active, s.UpdatedAt.UTC(), s.Version})
	if err != nil {
		return wrap("save offer price", err)
	}
	price.AdvanceVersion()
	return r.events.Write(ctx, r.q, price.PullEvents())
}

func scanOfferPrice(row pgx.Row) (domain.OfferPriceSnapshot, error) {
	var s domain.OfferPriceSnapshot
	err := row.Scan(&s.SKU, &s.ProductID, &s.SellerID, &s.Amount, &s.CompareAt, &s.Currency, &s.Active, &s.UpdatedAt, &s.Version)
	s.UpdatedAt = s.UpdatedAt.UTC()
	return s, err
}

type categoryIndex struct {
	q platform.Querier
}

func (c categoryIndex) Save(ctx context.Context, productID string, path []string, at time.Time) error {
	if path == nil {
		path = []string{}
	}
	_, err := c.q.Exec(ctx, `
		INSERT INTO pricing.product_categories (product_id, category_path, updated_at) VALUES ($1, $2::uuid[], $3)
		ON CONFLICT (product_id) DO UPDATE SET category_path = EXCLUDED.category_path, updated_at = EXCLUDED.updated_at`,
		productID, path, at.UTC())
	if err != nil {
		return fmt.Errorf("save product categories: %w", err)
	}
	return nil
}

func (c categoryIndex) Paths(ctx context.Context, productIDs []string) (map[string][]string, error) {
	return categoryPaths(ctx, c.q, productIDs)
}

func categoryPaths(ctx context.Context, q platform.Querier, productIDs []string) (map[string][]string, error) {
	rows, err := q.Query(ctx, `
		SELECT product_id::text, category_path::text[] FROM pricing.product_categories
		WHERE product_id::text = ANY ($1)`, productIDs)
	if err != nil {
		return nil, fmt.Errorf("select product categories: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string, len(productIDs))
	for rows.Next() {
		var (
			productID string
			path      []string
		)
		if err := rows.Scan(&productID, &path); err != nil {
			return nil, fmt.Errorf("scan product categories: %w", err)
		}
		out[productID] = path
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read product categories: %w", err)
	}
	return out, nil
}

func wrap(op string, err error) error {
	if errors.Is(err, kernel.ErrConcurrentModification) {
		return err
	}
	if name, ok := platform.UniqueViolation(err); ok {
		switch name {
		case "promo_codes_pkey":
			return domain.ErrPromoCodeExists
		case "promo_redemptions_pkey":
			return domain.ErrPromoAlreadyUsed
		}
	}
	if name, ok := platform.CheckViolation(err); ok && name == "chk_promo_codes_used" {
		return domain.ErrPromoCodeDepleted
	}
	return fmt.Errorf("%s: %w", op, err)
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func moment(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func optionalAmount(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}
