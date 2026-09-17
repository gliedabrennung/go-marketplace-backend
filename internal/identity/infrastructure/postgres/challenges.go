package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const challengeColumns = "id, purpose, target, user_id, secret_digest, status, attempts, max_attempts, expires_at, created_at, updated_at, version"

type challengeRepository struct {
	q platform.Querier
}

func (r challengeRepository) FindByID(ctx context.Context, id domain.ChallengeID) (*domain.Challenge, error) {
	return r.findOne(ctx, "SELECT "+challengeColumns+" FROM identity.challenges WHERE id = $1", id.String())
}

func (r challengeRepository) findOne(ctx context.Context, sql string, arg any) (*domain.Challenge, error) {
	var (
		s      domain.ChallengeSnapshot
		userID *string
	)
	err := r.q.QueryRow(ctx, sql, arg).Scan(
		&s.ID, &s.Purpose, &s.Target, &userID, &s.SecretDigest, &s.Status,
		&s.Attempts, &s.MaxAttempts, &s.ExpiresAt, &s.CreatedAt, &s.UpdatedAt, &s.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select challenge: %w", err)
	}
	s.UserID = deref(userID)
	s.ExpiresAt, s.CreatedAt, s.UpdatedAt = s.ExpiresAt.UTC(), s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return domain.RehydrateChallenge(s)
}

func (r challengeRepository) Save(ctx context.Context, c *domain.Challenge) error {
	s := c.Snapshot()
	if s.Version == 0 {
		_, err := r.q.Exec(ctx, `
			INSERT INTO identity.challenges (id, purpose, target, user_id, secret_digest, status, attempts, max_attempts,
			                                 expires_at, created_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1)`,
			s.ID, s.Purpose, s.Target, nullable(s.UserID), s.SecretDigest, s.Status, s.Attempts, s.MaxAttempts,
			s.ExpiresAt, s.CreatedAt, s.UpdatedAt,
		)
		if _, dup := platform.UniqueViolation(err); dup {
			return kernel.ErrConcurrentModification
		}
		if err != nil {
			return fmt.Errorf("insert challenge: %w", err)
		}
		c.AdvanceVersion()
		return nil
	}
	tag, err := r.q.Exec(ctx, `
		UPDATE identity.challenges
		SET status = $2, attempts = $3, updated_at = $4, version = version + 1
		WHERE id = $1 AND version = $5`,
		s.ID, s.Status, s.Attempts, s.UpdatedAt, s.Version,
	)
	if err != nil {
		return fmt.Errorf("update challenge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	c.AdvanceVersion()
	return nil
}

func DeleteExpiredChallenges(ctx context.Context, q platform.Querier, before time.Time) (int64, error) {
	tag, err := q.Exec(ctx, "DELETE FROM identity.challenges WHERE expires_at < $1", before)
	if err != nil {
		return 0, fmt.Errorf("delete expired challenges: %w", err)
	}
	return tag.RowsAffected(), nil
}
