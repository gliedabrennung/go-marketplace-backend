package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const sessionColumns = "s.id, s.user_id, s.refresh_token_hash, s.status, s.device_name, s.user_agent, s.ip, s.created_at, s.last_used_at, s.expires_at, s.revoked_at, s.revoke_reason, s.version"

type sessionRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r sessionRepository) FindByID(ctx context.Context, id domain.SessionID) (*domain.Session, error) {
	return r.findOne(ctx, "SELECT "+sessionColumns+" FROM identity.sessions s WHERE s.id = $1", id.String())
}

func (r sessionRepository) FindByRefreshDigest(ctx context.Context, digest string) (*domain.Session, error) {
	return r.findOne(ctx, "SELECT "+sessionColumns+
		" FROM identity.refresh_tokens t JOIN identity.sessions s ON s.id = t.session_id WHERE t.token_hash = $1", digest)
}

func (r sessionRepository) findOne(ctx context.Context, sql string, arg any) (*domain.Session, error) {
	var (
		s         domain.SessionSnapshot
		revokedAt *time.Time
		reason    *string
	)
	err := r.q.QueryRow(ctx, sql, arg).Scan(
		&s.ID, &s.UserID, &s.RefreshDigest, &s.Status, &s.DeviceName, &s.UserAgent, &s.IP,
		&s.CreatedAt, &s.LastUsedAt, &s.ExpiresAt, &revokedAt, &reason, &s.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select session: %w", err)
	}
	s.RevokedAt, s.RevokeReason = derefTime(revokedAt), deref(reason)
	s.CreatedAt, s.LastUsedAt, s.ExpiresAt = s.CreatedAt.UTC(), s.LastUsedAt.UTC(), s.ExpiresAt.UTC()
	return domain.RehydrateSession(s)
}

func (r sessionRepository) Save(ctx context.Context, session *domain.Session) error {
	s := session.Snapshot()
	if err := r.write(ctx, s); err != nil {
		return err
	}
	_, err := r.q.Exec(ctx,
		"INSERT INTO identity.refresh_tokens (token_hash, session_id, issued_at) VALUES ($1, $2, $3) ON CONFLICT (token_hash) DO NOTHING",
		s.RefreshDigest, s.ID, s.LastUsedAt,
	)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	if err := r.events.Write(ctx, r.q, session.PullEvents()); err != nil {
		return err
	}
	session.AdvanceVersion()
	return nil
}

func (r sessionRepository) write(ctx context.Context, s domain.SessionSnapshot) error {
	if s.Version == 0 {
		_, err := r.q.Exec(ctx, `
			INSERT INTO identity.sessions (id, user_id, refresh_token_hash, status, device_name, user_agent, ip,
			                               created_at, last_used_at, expires_at, revoked_at, revoke_reason, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 1)`,
			s.ID, s.UserID, s.RefreshDigest, s.Status, s.DeviceName, s.UserAgent, s.IP,
			s.CreatedAt, s.LastUsedAt, s.ExpiresAt, nullableTime(s.RevokedAt), nullable(s.RevokeReason),
		)
		if _, dup := platform.UniqueViolation(err); dup {
			return kernel.ErrConcurrentModification
		}
		if err != nil {
			return fmt.Errorf("insert session: %w", err)
		}
		return nil
	}
	tag, err := r.q.Exec(ctx, `
		UPDATE identity.sessions
		SET refresh_token_hash = $2, status = $3, last_used_at = $4, expires_at = $5,
		    revoked_at = $6, revoke_reason = $7, version = version + 1
		WHERE id = $1 AND version = $8`,
		s.ID, s.RefreshDigest, s.Status, s.LastUsedAt, s.ExpiresAt,
		nullableTime(s.RevokedAt), nullable(s.RevokeReason), s.Version,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}
