package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const sellerColumns = `id, owner_id, status, legal_form, legal_name, tax_id, legal_address,
	bank_iban, bank_bic, bank_name, bank_beneficiary, bank_verified,
	rejection_reason, suspension_reason, suspension_note,
	rating_score, rating_cancellation_bp, rating_late_shipment_bp, rating_average_review, rating_orders, rating_provisional, rating_calculated_at,
	created_at, updated_at, version`

type sellerRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r sellerRepository) FindByID(ctx context.Context, id kernel.SellerID) (*domain.Seller, error) {
	snap, err := loadSnapshot(ctx, r.q, id.String())
	if err != nil {
		return nil, err
	}
	return domain.RehydrateSeller(snap)
}

func loadSnapshot(ctx context.Context, q platform.Querier, id string) (domain.SellerSnapshot, error) {
	snap, err := scanSeller(q.QueryRow(ctx, "SELECT "+sellerColumns+" FROM seller.sellers WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SellerSnapshot{}, domain.ErrSellerNotFound
	}
	if err != nil {
		return domain.SellerSnapshot{}, fmt.Errorf("select seller: %w", err)
	}
	if err := loadChildren(ctx, q, &snap); err != nil {
		return domain.SellerSnapshot{}, err
	}
	return snap, nil
}

func scanSeller(row pgx.Row) (domain.SellerSnapshot, error) {
	var (
		s                                                 domain.SellerSnapshot
		iban, bic, bankName, beneficiary                  *string
		rejection, suspensionReason, suspensionNote       *string
		score, cancellation, lateShipment, average, count *int
		provisional                                       *bool
		calculatedAt                                      *time.Time
	)
	err := row.Scan(
		&s.ID, &s.OwnerID, &s.Status, &s.LegalForm, &s.LegalName, &s.TaxID, &s.LegalAddress,
		&iban, &bic, &bankName, &beneficiary, &s.BankVerified,
		&rejection, &suspensionReason, &suspensionNote,
		&score, &cancellation, &lateShipment, &average, &count, &provisional, &calculatedAt,
		&s.CreatedAt, &s.UpdatedAt, &s.Version,
	)
	if err != nil {
		return domain.SellerSnapshot{}, err
	}
	s.BankIBAN, s.BankBIC, s.BankName, s.BankBeneficiary = deref(iban), deref(bic), deref(bankName), deref(beneficiary)
	s.RejectionReason, s.SuspensionReason, s.SuspensionNote = deref(rejection), deref(suspensionReason), deref(suspensionNote)
	s.CreatedAt, s.UpdatedAt = utc(s.CreatedAt), utc(s.UpdatedAt)
	if score != nil && calculatedAt != nil {
		s.Rating = &domain.RatingSnapshot{
			Score: *score, CancellationRate: *cancellation, LateShipmentRate: *lateShipment,
			AverageReview: *average, Orders: *count, Provisional: *provisional, CalculatedAt: utc(*calculatedAt),
		}
	}
	return s, nil
}

func loadChildren(ctx context.Context, q platform.Querier, s *domain.SellerSnapshot) error {
	rows, err := q.Query(ctx, "SELECT user_id::text, role, added_at FROM seller.members WHERE seller_id = $1 ORDER BY position", s.ID)
	if err != nil {
		return fmt.Errorf("select members: %w", err)
	}
	s.Members, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.MemberSnapshot, error) {
		var m domain.MemberSnapshot
		err := row.Scan(&m.UserID, &m.Role, &m.AddedAt)
		m.AddedAt = utc(m.AddedAt)
		return m, err
	})
	if err != nil {
		return fmt.Errorf("scan members: %w", err)
	}

	rows, err = q.Query(ctx, "SELECT kind, object_key, uploaded_at FROM seller.documents WHERE seller_id = $1 ORDER BY position", s.ID)
	if err != nil {
		return fmt.Errorf("select documents: %w", err)
	}
	s.Documents, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.DocumentSnapshot, error) {
		var d domain.DocumentSnapshot
		err := row.Scan(&d.Kind, &d.ObjectKey, &d.UploadedAt)
		d.UploadedAt = utc(d.UploadedAt)
		return d, err
	})
	if err != nil {
		return fmt.Errorf("scan documents: %w", err)
	}

	rows, err = q.Query(ctx, "SELECT category_id::text, rate_bp FROM seller.commission_overrides WHERE seller_id = $1", s.ID)
	if err != nil {
		return fmt.Errorf("select commission overrides: %w", err)
	}
	s.CommissionOverrides = map[string]int{}
	var (
		category string
		rate     int
	)
	_, err = pgx.ForEachRow(rows, []any{&category, &rate}, func() error {
		s.CommissionOverrides[category] = rate
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan commission overrides: %w", err)
	}
	return nil
}

func (r sellerRepository) Save(ctx context.Context, seller *domain.Seller) error {
	s := seller.Snapshot()
	if err := r.writeRoot(ctx, s); err != nil {
		return err
	}
	if err := r.replaceChildren(ctx, s); err != nil {
		return err
	}
	if err := r.events.Write(ctx, r.q, seller.PullEvents()); err != nil {
		return err
	}
	seller.AdvanceVersion()
	return nil
}

func (r sellerRepository) writeRoot(ctx context.Context, s domain.SellerSnapshot) error {
	rating := ratingColumns(s.Rating)
	if s.Version == 0 {
		args := append([]any{
			s.ID, s.OwnerID, s.Status, s.LegalForm, s.LegalName, s.TaxID, s.LegalAddress,
			nullable(s.BankIBAN), nullable(s.BankBIC), nullable(s.BankName), nullable(s.BankBeneficiary), s.BankVerified,
			nullable(s.RejectionReason), nullable(s.SuspensionReason), nullable(s.SuspensionNote),
		}, rating...)
		args = append(args, s.CreatedAt, s.UpdatedAt)
		_, err := r.q.Exec(ctx, "INSERT INTO seller.sellers ("+sellerColumns+`)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, 1)`,
			args...)
		return sellerWriteError(err)
	}
	args := append([]any{
		s.ID, s.Status, s.LegalForm, s.LegalName, s.TaxID, s.LegalAddress,
		nullable(s.BankIBAN), nullable(s.BankBIC), nullable(s.BankName), nullable(s.BankBeneficiary), s.BankVerified,
		nullable(s.RejectionReason), nullable(s.SuspensionReason), nullable(s.SuspensionNote),
	}, rating...)
	args = append(args, s.UpdatedAt, s.Version)
	tag, err := r.q.Exec(ctx, `
		UPDATE seller.sellers SET
			status = $2, legal_form = $3, legal_name = $4, tax_id = $5, legal_address = $6,
			bank_iban = $7, bank_bic = $8, bank_name = $9, bank_beneficiary = $10, bank_verified = $11,
			rejection_reason = $12, suspension_reason = $13, suspension_note = $14,
			rating_score = $15, rating_cancellation_bp = $16, rating_late_shipment_bp = $17, rating_average_review = $18,
			rating_orders = $19, rating_provisional = $20, rating_calculated_at = $21,
			updated_at = $22, version = version + 1
		WHERE id = $1 AND version = $23`,
		args...)
	if err != nil {
		return sellerWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}

func ratingColumns(r *domain.RatingSnapshot) []any {
	if r == nil {
		return []any{nil, nil, nil, nil, nil, nil, nil}
	}
	return []any{r.Score, r.CancellationRate, r.LateShipmentRate, r.AverageReview, r.Orders, r.Provisional, r.CalculatedAt}
}

func (r sellerRepository) replaceChildren(ctx context.Context, s domain.SellerSnapshot) error {
	for _, table := range []string{"seller.members", "seller.documents", "seller.commission_overrides"} {
		if _, err := r.q.Exec(ctx, "DELETE FROM "+table+" WHERE seller_id = $1", s.ID); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	for i, m := range s.Members {
		if _, err := r.q.Exec(ctx, "INSERT INTO seller.members (seller_id, user_id, role, added_at, position) VALUES ($1, $2, $3, $4, $5)",
			s.ID, m.UserID, m.Role, m.AddedAt, i); err != nil {
			return fmt.Errorf("insert member: %w", err)
		}
	}
	for i, d := range s.Documents {
		if _, err := r.q.Exec(ctx, "INSERT INTO seller.documents (seller_id, object_key, kind, uploaded_at, position) VALUES ($1, $2, $3, $4, $5)",
			s.ID, d.ObjectKey, d.Kind, d.UploadedAt, i); err != nil {
			return fmt.Errorf("insert document: %w", err)
		}
	}
	for category, rate := range s.CommissionOverrides {
		if _, err := r.q.Exec(ctx, "INSERT INTO seller.commission_overrides (seller_id, category_id, rate_bp) VALUES ($1, $2, $3)",
			s.ID, category, rate); err != nil {
			return fmt.Errorf("insert commission override: %w", err)
		}
	}
	return nil
}

func sellerWriteError(err error) error {
	if err == nil {
		return nil
	}
	if constraint, ok := platform.UniqueViolation(err); ok {
		switch constraint {
		case "uq_sellers_owner_active":
			return domain.ErrSellerAlreadyExists
		case "uq_sellers_tax_id_active":
			return domain.ErrTaxIDTaken
		case "sellers_pkey":
			return kernel.ErrConcurrentModification
		}
	}
	return fmt.Errorf("write seller: %w", err)
}

type commissionRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r commissionRepository) FindByCategory(ctx context.Context, id domain.CategoryID) (*domain.CategoryCommission, error) {
	var (
		rate      int
		updatedAt time.Time
		version   int
	)
	err := r.q.QueryRow(ctx, "SELECT rate_bp, updated_at, version FROM seller.category_commissions WHERE category_id = $1", id.String()).
		Scan(&rate, &updatedAt, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCategoryCommissionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select category commission: %w", err)
	}
	return domain.RehydrateCategoryCommission(id.String(), rate, utc(updatedAt), version)
}

func (r commissionRepository) Save(ctx context.Context, c *domain.CategoryCommission) error {
	if c.Version() == 0 {
		_, err := r.q.Exec(ctx, "INSERT INTO seller.category_commissions (category_id, rate_bp, updated_at, version) VALUES ($1, $2, $3, 1)",
			c.CategoryID().String(), c.Rate().Value(), c.UpdatedAt())
		if _, dup := platform.UniqueViolation(err); dup {
			return kernel.ErrConcurrentModification
		}
		if err != nil {
			return fmt.Errorf("insert category commission: %w", err)
		}
	} else {
		tag, err := r.q.Exec(ctx, "UPDATE seller.category_commissions SET rate_bp = $2, updated_at = $3, version = version + 1 WHERE category_id = $1 AND version = $4",
			c.CategoryID().String(), c.Rate().Value(), c.UpdatedAt(), c.Version())
		if err != nil {
			return fmt.Errorf("update category commission: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return kernel.ErrConcurrentModification
		}
	}
	if err := r.events.Write(ctx, r.q, c.PullEvents()); err != nil {
		return err
	}
	c.AdvanceVersion()
	return nil
}
