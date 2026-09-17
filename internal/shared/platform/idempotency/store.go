package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type State string

const (
	StateInProgress State = "in_progress"
	StateCompleted  State = "completed"
)

type Key struct {
	Principal string
	Scope     string
	Value     string
}

type Record struct {
	State       State
	RequestHash string
	StatusCode  int
	ContentType string
	Body        []byte
}

type Store interface {
	Begin(ctx context.Context, key Key, requestHash string, ttl time.Duration) (Record, bool, error)
	Complete(ctx context.Context, key Key, statusCode int, contentType string, body []byte) error
	Release(ctx context.Context, key Key) error
}

type PostgresStore struct {
	db postgres.Querier
}

func NewPostgresStore(db postgres.Querier) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Begin(ctx context.Context, key Key, requestHash string, ttl time.Duration) (Record, bool, error) {
	var created bool
	err := s.db.QueryRow(ctx, `
		INSERT INTO platform.idempotency_keys (principal, scope, key, request_hash, state, expires_at)
		VALUES ($1, $2, $3, $4, 'in_progress', now() + make_interval(secs => $5))
		ON CONFLICT (principal, scope, key) DO UPDATE
		SET request_hash = EXCLUDED.request_hash,
		    state = 'in_progress',
		    response = NULL,
		    status_code = NULL,
		    content_type = NULL,
		    created_at = now(),
		    expires_at = EXCLUDED.expires_at
		WHERE platform.idempotency_keys.expires_at <= now()
		RETURNING true`,
		key.Principal, key.Scope, key.Value, requestHash, ttl.Seconds(),
	).Scan(&created)
	switch {
	case err == nil:
		return Record{State: StateInProgress, RequestHash: requestHash}, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return Record{}, false, fmt.Errorf("begin idempotent request: %w", err)
	}

	var (
		rec         Record
		statusCode  *int
		contentType *string
	)
	err = s.db.QueryRow(ctx, `
		SELECT state, request_hash, status_code, content_type, response
		FROM platform.idempotency_keys
		WHERE principal = $1 AND scope = $2 AND key = $3`,
		key.Principal, key.Scope, key.Value,
	).Scan(&rec.State, &rec.RequestHash, &statusCode, &contentType, &rec.Body)
	if err != nil {
		return Record{}, false, fmt.Errorf("load idempotent request: %w", err)
	}
	if statusCode != nil {
		rec.StatusCode = *statusCode
	}
	if contentType != nil {
		rec.ContentType = *contentType
	}
	return rec, false, nil
}

func (s *PostgresStore) Complete(ctx context.Context, key Key, statusCode int, contentType string, body []byte) error {
	_, err := s.db.Exec(ctx, `
		UPDATE platform.idempotency_keys
		SET state = 'completed', status_code = $4, content_type = $5, response = $6
		WHERE principal = $1 AND scope = $2 AND key = $3`,
		key.Principal, key.Scope, key.Value, statusCode, contentType, body,
	)
	if err != nil {
		return fmt.Errorf("complete idempotent request: %w", err)
	}
	return nil
}

func (s *PostgresStore) Release(ctx context.Context, key Key) error {
	_, err := s.db.Exec(ctx,
		"DELETE FROM platform.idempotency_keys WHERE principal = $1 AND scope = $2 AND key = $3 AND state = 'in_progress'",
		key.Principal, key.Scope, key.Value,
	)
	if err != nil {
		return fmt.Errorf("release idempotent request: %w", err)
	}
	return nil
}

func DeleteExpired(ctx context.Context, q postgres.Querier) (int64, error) {
	tag, err := q.Exec(ctx, "DELETE FROM platform.idempotency_keys WHERE expires_at < now()")
	if err != nil {
		return 0, fmt.Errorf("delete expired idempotency keys: %w", err)
	}
	return tag.RowsAffected(), nil
}
