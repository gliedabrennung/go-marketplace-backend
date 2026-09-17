package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/observability"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/reqctx"
)

func TestLogger_MasksSecretsAndPersonalData(t *testing.T) {
	var buf bytes.Buffer
	log := observability.NewLogger(observability.LogConfig{
		Level: "debug", Service: "api", Version: "1.0.0", Env: "test",
	}, &buf)

	ctx := reqctx.WithRequestID(context.Background(), "req-1")
	log.InfoContext(ctx, "login attempt",
		"password", "hunter2",
		"refresh_token", "opaque",
		"phone", "+77011234567",
		"user", map[string]string{"email": "buyer@example.com"},
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	assert.Equal(t, "login attempt", entry["message"])
	assert.Contains(t, entry, "timestamp")
	assert.Equal(t, "api", entry["service"])
	assert.Equal(t, "1.0.0", entry["version"])
	assert.Equal(t, "test", entry["env"])
	assert.Equal(t, "req-1", entry["request_id"])
	assert.Equal(t, "***", entry["password"])
	assert.Equal(t, "***", entry["refresh_token"])
	assert.Equal(t, "***67", entry["phone"])
	assert.NotContains(t, buf.String(), "hunter2")
	assert.NotContains(t, buf.String(), "+77011234567")
}
