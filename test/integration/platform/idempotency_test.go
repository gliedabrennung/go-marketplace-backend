//go:build integration

package platform_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/idempotency"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

func TestIdempotencyStore_Lifecycle(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.idempotency_keys")
	ctx := context.Background()
	store := idempotency.NewPostgresStore(pool)
	key := idempotency.Key{Principal: "buyer-1", Scope: "POST /api/v1/orders", Value: "0190f5a2-7c3e-7b1a-9c2d-1e2f3a4b5c6d"}

	rec, created, err := store.Begin(ctx, key, "hash-1", time.Hour)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, idempotency.StateInProgress, rec.State)

	rec, created, err = store.Begin(ctx, key, "hash-1", time.Hour)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, idempotency.StateInProgress, rec.State)

	require.NoError(t, store.Complete(ctx, key, 201, "application/json", []byte(`{"order_id":"1"}`)))

	rec, created, err = store.Begin(ctx, key, "hash-1", time.Hour)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, idempotency.StateCompleted, rec.State)
	assert.Equal(t, 201, rec.StatusCode)
	assert.Equal(t, "application/json", rec.ContentType)
	assert.JSONEq(t, `{"order_id":"1"}`, string(rec.Body))
	assert.Equal(t, "hash-1", rec.RequestHash)
}

func TestIdempotencyStore_KeysAreScopedByPrincipal(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.idempotency_keys")
	ctx := context.Background()
	store := idempotency.NewPostgresStore(pool)
	value := "0190f5a2-7c3e-7b1a-9c2d-1e2f3a4b5c6d"

	_, created, err := store.Begin(ctx, idempotency.Key{Principal: "buyer-1", Scope: "s", Value: value}, "h", time.Hour)
	require.NoError(t, err)
	assert.True(t, created)

	_, created, err = store.Begin(ctx, idempotency.Key{Principal: "buyer-2", Scope: "s", Value: value}, "h", time.Hour)
	require.NoError(t, err)
	assert.True(t, created)
}

func TestIdempotencyStore_ExpiredKeyCanBeReused(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.idempotency_keys")
	ctx := context.Background()
	store := idempotency.NewPostgresStore(pool)
	key := idempotency.Key{Principal: "buyer-1", Scope: "s", Value: "k"}

	_, _, err := store.Begin(ctx, key, "old", time.Hour)
	require.NoError(t, err)
	require.NoError(t, store.Complete(ctx, key, 200, "application/json", []byte(`{}`)))
	_, err = pool.Exec(ctx, "UPDATE platform.idempotency_keys SET expires_at = now() - interval '1 second'")
	require.NoError(t, err)

	rec, created, err := store.Begin(ctx, key, "new", time.Hour)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "new", rec.RequestHash)

	deleted, err := idempotency.DeleteExpired(ctx, pool)
	require.NoError(t, err)
	assert.Zero(t, deleted)
}

func TestIdempotencyStore_ReleaseAllowsRetry(t *testing.T) {
	pool := testdb.Pool(t)
	testdb.Truncate(t, pool, "platform.idempotency_keys")
	ctx := context.Background()
	store := idempotency.NewPostgresStore(pool)
	key := idempotency.Key{Principal: "buyer-1", Scope: "s", Value: "k"}

	_, _, err := store.Begin(ctx, key, "h", time.Hour)
	require.NoError(t, err)
	require.NoError(t, store.Release(ctx, key))

	_, created, err := store.Begin(ctx, key, "h", time.Hour)
	require.NoError(t, err)
	assert.True(t, created)
}
