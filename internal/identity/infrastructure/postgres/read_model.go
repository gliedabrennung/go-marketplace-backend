package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/audit"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type ReadModel struct {
	db platform.Querier
}

func NewReadModel(db platform.Querier) *ReadModel {
	return &ReadModel{db: db}
}

func (m *ReadModel) ListActiveByUser(ctx context.Context, userID string, now time.Time, limit int, after *pagination.Keyset) (pagination.Page[query.SessionView], error) {
	sql := `SELECT id, device_name, user_agent, ip, created_at, last_used_at, expires_at
		FROM identity.sessions
		WHERE user_id = $1 AND status = 'active' AND expires_at > $2`
	args := []any{userID, now, limit + 1}
	if after != nil {
		if _, err := kernel.ParseID[struct{}](after.ID); err != nil {
			return pagination.Page[query.SessionView]{}, pagination.ErrInvalidCursor
		}
		sql += " AND (last_used_at, id) < ($4, $5)"
		args = append(args, after.At, after.ID)
	}
	sql += " ORDER BY last_used_at DESC, id DESC LIMIT $3"

	rows, err := m.db.Query(ctx, sql, args...)
	if err != nil {
		return pagination.Page[query.SessionView]{}, fmt.Errorf("list sessions: %w", err)
	}
	views, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (query.SessionView, error) {
		var v query.SessionView
		err := row.Scan(&v.ID, &v.DeviceName, &v.UserAgent, &v.IP, &v.CreatedAt, &v.LastUsedAt, &v.ExpiresAt)
		return v, err
	})
	if err != nil {
		return pagination.Page[query.SessionView]{}, fmt.Errorf("scan sessions: %w", err)
	}
	return pagination.Build(views, limit, func(v query.SessionView) pagination.Keyset {
		return pagination.Keyset{At: v.LastUsedAt, ID: v.ID}
	}), nil
}

func (m *ReadModel) ActiveSessionIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := m.db.Query(ctx,
		"SELECT id::text FROM identity.sessions WHERE user_id = $1 AND status = 'active' ORDER BY id", userID)
	if err != nil {
		return nil, fmt.Errorf("list active session ids: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan active session ids: %w", err)
	}
	return ids, nil
}

func (m *ReadModel) Profile(ctx context.Context, userID string) (query.Profile, error) {
	var p query.Profile
	err := m.db.QueryRow(ctx, `
		SELECT id, email, email_verified, roles, status, created_at
		FROM identity.users WHERE id = $1`, userID,
	).Scan(&p.ID, &p.Email, &p.EmailVerified, &p.Roles, &p.Status, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return query.Profile{}, domain.ErrUserNotFound
	}
	if err != nil {
		return query.Profile{}, fmt.Errorf("select profile: %w", err)
	}
	p.CreatedAt = p.CreatedAt.UTC()
	return p, nil
}

type auditTrail struct {
	q platform.Querier
}

func (a auditTrail) Record(ctx context.Context, e application.AuditEntry) error {
	return audit.Writer{}.Write(ctx, a.q, audit.Entry{
		ActorID:    e.ActorID,
		ActorRoles: e.ActorRoles,
		Action:     e.Action,
		ObjectType: e.ObjectType,
		ObjectID:   e.ObjectID,
		Details:    e.Details,
		OccurredAt: e.OccurredAt,
	})
}
