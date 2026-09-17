package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReadModel struct {
	db platform.Querier
}

func NewReadModel(db platform.Querier) *ReadModel {
	return &ReadModel{db: db}
}

func (m *ReadModel) Payment(ctx context.Context, paymentID string) (query.PaymentView, error) {
	views, err := m.payments(ctx, `WHERE id = $1`, paymentID)
	if err != nil {
		return query.PaymentView{}, err
	}
	if len(views) == 0 {
		return query.PaymentView{}, domain.ErrPaymentNotFound
	}
	return views[0], nil
}

func (m *ReadModel) OrderPayments(ctx context.Context, orderID string) ([]query.PaymentView, error) {
	return m.payments(ctx, `WHERE order_id = $1 ORDER BY created_at, id`, orderID)
}

func (m *ReadModel) payments(ctx context.Context, where string, args ...any) ([]query.PaymentView, error) {
	rows, err := m.db.Query(ctx, `SELECT `+paymentColumns+` FROM payment.payments `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("select payments: %w", err)
	}
	snaps, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PaymentSnapshot, error) {
		return scanPayment(row)
	})
	if err != nil {
		return nil, fmt.Errorf("scan payments: %w", err)
	}
	if len(snaps) == 0 {
		return []query.PaymentView{}, nil
	}
	ids := make([]string, 0, len(snaps))
	for _, snap := range snaps {
		ids = append(ids, snap.ID)
	}
	all, err := refundsByPayment(ctx, m.db, ids)
	if err != nil {
		return nil, err
	}
	views := make([]query.PaymentView, 0, len(snaps))
	for _, snap := range snaps {
		snap.Refunds = all[snap.ID]
		views = append(views, query.NewPaymentView(snap))
	}
	return views, nil
}

func refundsByPayment(ctx context.Context, q platform.Querier, paymentIDs []string) (map[string][]domain.RefundSnapshot, error) {
	rows, err := q.Query(ctx, `
		SELECT payment_id::text, id::text, amount, reason, status, provider_refund_id, created_at, completed_at
		FROM payment.refunds WHERE payment_id = ANY ($1::uuid[]) ORDER BY created_at, id`, paymentIDs)
	if err != nil {
		return nil, fmt.Errorf("select refunds: %w", err)
	}
	defer rows.Close()
	out := map[string][]domain.RefundSnapshot{}
	for rows.Next() {
		var (
			s         domain.RefundSnapshot
			paymentID string
			completed *time.Time
		)
		if err := rows.Scan(&paymentID, &s.ID, &s.Amount, &s.Reason, &s.Status, &s.ProviderRefundID, &s.CreatedAt, &completed); err != nil {
			return nil, fmt.Errorf("scan refund: %w", err)
		}
		s.CreatedAt, s.CompletedAt = s.CreatedAt.UTC(), moment(completed)
		out[paymentID] = append(out[paymentID], s)
	}
	return out, rows.Err()
}

func (m *ReadModel) Methods(ctx context.Context, buyerID string) ([]query.MethodView, error) {
	rows, err := m.db.Query(ctx, `SELECT id::text, provider, label, created_at FROM payment.saved_methods
		WHERE buyer_id = $1 AND removed_at IS NULL ORDER BY created_at DESC, id`, buyerID)
	if err != nil {
		return nil, fmt.Errorf("select saved methods: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.MethodView, error) {
		var view query.MethodView
		err := row.Scan(&view.ID, &view.Provider, &view.Label, &view.CreatedAt)
		view.CreatedAt = view.CreatedAt.UTC()
		return view, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan saved methods: %w", err)
	}
	return views, nil
}

func (m *ReadModel) Reconciliation(ctx context.Context, provider string, day time.Time) (query.ReconciliationView, error) {
	var (
		report  application.ReconciliationReport
		payload []byte
	)
	err := m.db.QueryRow(ctx, `SELECT provider, day, checked, mismatches, created_at FROM payment.reconciliation_reports
		WHERE provider = $1 AND day = $2`, provider, day.UTC().Format(time.DateOnly)).
		Scan(&report.Provider, &report.Day, &report.Checked, &payload, &report.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return query.ReconciliationView{}, query.ErrReportNotFound
	}
	if err != nil {
		return query.ReconciliationView{}, fmt.Errorf("select reconciliation report: %w", err)
	}
	if err := json.Unmarshal(payload, &report.Mismatches); err != nil {
		return query.ReconciliationView{}, fmt.Errorf("decode reconciliation mismatches: %w", err)
	}
	report.Day, report.CreatedAt = report.Day.UTC(), report.CreatedAt.UTC()
	return query.NewReconciliationView(report), nil
}

type Reconciliations struct {
	db platform.Querier
}

func NewReconciliations(db platform.Querier) *Reconciliations {
	return &Reconciliations{db: db}
}

func (r *Reconciliations) Ledger(ctx context.Context, provider string, day time.Time) ([]application.LedgerEntry, error) {
	from := day.UTC()
	rows, err := r.db.Query(ctx, `
		SELECT id::text, provider_payment_id, status, captured, refunded, currency FROM payment.payments
		WHERE provider = $1 AND provider_payment_id IS NOT NULL AND created_at >= $2 AND created_at < $3
		ORDER BY id`, provider, from, from.AddDate(0, 0, 1))
	if err != nil {
		return nil, fmt.Errorf("select ledger: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (application.LedgerEntry, error) {
		var entry application.LedgerEntry
		err := row.Scan(&entry.PaymentID, &entry.ProviderPaymentID, &entry.Status, &entry.Captured, &entry.Refunded, &entry.Currency)
		return entry, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan ledger: %w", err)
	}
	return entries, nil
}

func (r *Reconciliations) Save(ctx context.Context, report application.ReconciliationReport) error {
	payload, err := json.Marshal(report.Mismatches)
	if err != nil {
		return fmt.Errorf("encode mismatches: %w", err)
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO payment.reconciliation_reports (provider, day, checked, mismatches, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider, day) DO UPDATE SET checked = EXCLUDED.checked, mismatches = EXCLUDED.mismatches,
			created_at = EXCLUDED.created_at`,
		report.Provider, report.Day.UTC().Format(time.DateOnly), report.Checked, payload, report.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("save reconciliation report: %w", err)
	}
	return nil
}

func DeleteWebhooksBefore(ctx context.Context, db platform.Querier, before time.Time) (int64, error) {
	tag, err := db.Exec(ctx, `DELETE FROM payment.webhook_events WHERE received_at < $1`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete webhook events: %w", err)
	}
	return tag.RowsAffected(), nil
}
