package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const offerColumns = `id::text, product_id::text, seller_id::text, seller_sku, price_amount, currency, condition,
	processing_days, status, created_at, updated_at, version`

type offerRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r offerRepository) FindByID(ctx context.Context, id domain.OfferID) (*domain.Offer, error) {
	snap, err := scanOffer(r.q.QueryRow(ctx, `SELECT `+offerColumns+` FROM catalog.offers WHERE id = $1`, id.String()))
	if err != nil {
		return nil, notFound(err, domain.ErrOfferNotFound)
	}
	return domain.RehydrateOffer(snap)
}

func (r offerRepository) FindBySellerSKU(ctx context.Context, seller kernel.SellerID, sku domain.SellerSKU) (*domain.Offer, error) {
	snap, err := scanOffer(r.q.QueryRow(ctx, `SELECT `+offerColumns+`
		FROM catalog.offers WHERE seller_id = $1 AND seller_sku = $2 AND status <> 'archived'`, seller.String(), sku.String()))
	if err != nil {
		return nil, notFound(err, domain.ErrOfferNotFound)
	}
	return domain.RehydrateOffer(snap)
}

func (r offerRepository) Save(ctx context.Context, offer *domain.Offer) error {
	snap := offer.Snapshot()
	var err error
	if snap.Version == 0 {
		err = exec(ctx, r.q, "insert offer", `
			INSERT INTO catalog.offers (id, product_id, seller_id, seller_sku, price_amount, currency, condition,
				processing_days, status, created_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1)`,
			snap.ID, snap.ProductID, snap.SellerID, snap.SellerSKU, snap.PriceAmount, snap.Currency, snap.Condition,
			snap.ProcessingDays, snap.Status, utc(snap.CreatedAt), utc(snap.UpdatedAt))
	} else {
		err = update(ctx, r.q, "update offer", `
			UPDATE catalog.offers
			SET price_amount = $2, currency = $3, condition = $4, processing_days = $5, status = $6,
				updated_at = $7, version = version + 1
			WHERE id = $1 AND version = $8`,
			snap.ID, snap.PriceAmount, snap.Currency, snap.Condition, snap.ProcessingDays, snap.Status,
			utc(snap.UpdatedAt), snap.Version)
	}
	if err != nil {
		return err
	}
	offer.AdvanceVersion()
	return r.events.Write(ctx, r.q, offer.PullEvents())
}

func scanOffer(row pgx.Row) (domain.OfferSnapshot, error) {
	var snap domain.OfferSnapshot
	err := row.Scan(&snap.ID, &snap.ProductID, &snap.SellerID, &snap.SellerSKU, &snap.PriceAmount, &snap.Currency,
		&snap.Condition, &snap.ProcessingDays, &snap.Status, &snap.CreatedAt, &snap.UpdatedAt, &snap.Version)
	snap.CreatedAt, snap.UpdatedAt = utc(snap.CreatedAt), utc(snap.UpdatedAt)
	return snap, err
}
