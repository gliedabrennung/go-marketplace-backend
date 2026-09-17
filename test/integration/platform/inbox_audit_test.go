//go:build integration

package platform_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/audit"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/inbox"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/migrations"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

func TestInbox_ProcessesMessageOnce(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.processed_messages")
	ctx := context.Background()
	guard := inbox.NewGuard(pool)

	calls := 0
	handler := func(context.Context, pgx.Tx) error {
		calls++
		return nil
	}

	processed, err := guard.Once(ctx, "settlement", "msg-1", handler)
	require.NoError(t, err)
	assert.True(t, processed)

	processed, err = guard.Once(ctx, "settlement", "msg-1", handler)
	require.NoError(t, err)
	assert.False(t, processed)
	assert.Equal(t, 1, calls)

	processed, err = guard.Once(ctx, "search", "msg-1", handler)
	require.NoError(t, err)
	assert.True(t, processed)
}

func TestInbox_FailedHandlerCanBeRetried(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.processed_messages")
	ctx := context.Background()
	guard := inbox.NewGuard(pool)

	errTransient := errors.New("transient")
	_, err := guard.Once(ctx, "settlement", "msg-1", func(context.Context, pgx.Tx) error { return errTransient })
	require.ErrorIs(t, err, errTransient)

	processed, err := guard.Once(ctx, "settlement", "msg-1", func(context.Context, pgx.Tx) error { return nil })
	require.NoError(t, err)
	assert.True(t, processed)
}

func TestAuditLog_IsAppendOnly(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()

	err := audit.Writer{}.Write(ctx, pool, audit.Entry{
		ActorID:    "admin-1",
		ActorRoles: []string{"platform_admin"},
		Action:     "seller.suspend",
		ObjectType: "seller",
		ObjectID:   "seller-1",
		Details:    map[string]string{"reason": "fraud"},
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "UPDATE platform.audit_log SET action = 'tampered'")
	require.Error(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM platform.audit_log")
	require.Error(t, err)
}

func TestMigrations_AreReversible(t *testing.T) {
	ctx := context.Background()
	url := testdb.URL()
	require.NoError(t, postgres.Migrate(ctx, url, migrations.FS, "reset", io.Discard))
	require.NoError(t, postgres.Migrate(ctx, url, migrations.FS, "up", io.Discard))

	pool := testdb.Pool(t)
	var exists bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT to_regclass('platform.outbox') IS NOT NULL").Scan(&exists))
	assert.True(t, exists)

	err := postgres.InTx(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error { return nil })
	require.NoError(t, err)
}
