package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
	platform "github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

const userColumns = "id, email, email_verified, password_hash, roles, status, block_reason, created_at, updated_at, version"

type userRepository struct {
	q      platform.Querier
	events *outbox.Writer
}

func (r userRepository) FindByID(ctx context.Context, id kernel.UserID) (*domain.User, error) {
	return r.findOne(ctx, "id = $1", id.String())
}

func (r userRepository) FindByVerifiedEmail(ctx context.Context, email domain.Email) (*domain.User, error) {
	return r.findOne(ctx, "email = $1 AND email_verified", email.String())
}

func (r userRepository) findOne(ctx context.Context, where string, arg any) (*domain.User, error) {
	var (
		s      domain.UserSnapshot
		reason *string
	)
	err := r.q.QueryRow(ctx, "SELECT "+userColumns+" FROM identity.users WHERE "+where, arg).Scan(
		&s.ID, &s.Email, &s.EmailVerified, &s.PasswordHash,
		&s.Roles, &s.Status, &reason, &s.CreatedAt, &s.UpdatedAt, &s.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select user: %w", err)
	}
	s.BlockReason = deref(reason)
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return domain.RehydrateUser(s)
}

func (r userRepository) Save(ctx context.Context, u *domain.User) error {
	s := u.Snapshot()
	if err := r.write(ctx, s); err != nil {
		return err
	}
	if err := r.events.Write(ctx, r.q, u.PullEvents()); err != nil {
		return err
	}
	u.AdvanceVersion()
	return nil
}

func (r userRepository) write(ctx context.Context, s domain.UserSnapshot) error {
	if s.Version == 0 {
		_, err := r.q.Exec(ctx, `
			INSERT INTO identity.users (id, email, email_verified, password_hash, roles, status, block_reason, created_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1)`,
			s.ID, s.Email, s.EmailVerified, s.PasswordHash, s.Roles, s.Status, nullable(s.BlockReason), s.CreatedAt, s.UpdatedAt,
		)
		return userWriteError(err)
	}
	tag, err := r.q.Exec(ctx, `
		UPDATE identity.users
		SET email = $2, email_verified = $3, password_hash = $4, roles = $5, status = $6,
		    block_reason = $7, updated_at = $8, version = version + 1
		WHERE id = $1 AND version = $9`,
		s.ID, s.Email, s.EmailVerified, s.PasswordHash, s.Roles, s.Status, nullable(s.BlockReason), s.UpdatedAt, s.Version,
	)
	if err != nil {
		return userWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return kernel.ErrConcurrentModification
	}
	return nil
}

func userWriteError(err error) error {
	if err == nil {
		return nil
	}
	if constraint, ok := platform.UniqueViolation(err); ok {
		switch constraint {
		case "uq_users_verified_email":
			return domain.ErrEmailTaken
		case "users_pkey":
			return kernel.ErrConcurrentModification
		}
	}
	return fmt.Errorf("write user: %w", err)
}
