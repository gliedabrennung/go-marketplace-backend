package postgres_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestEventCodec_EncodesEveryDomainEvent(t *testing.T) {
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	user := kernel.NewUserID()
	admin := kernel.NewUserID()
	session := domain.NewSessionID()

	cases := []struct {
		event kernel.DomainEvent
		name  string
	}{
		{domain.UserRegistered{UserID: user, At: at}, api.EventUserRegistered},
		{domain.UserEmailConfirmed{UserID: user, At: at}, api.EventUserEmailConfirmed},
		{domain.UserBlocked{UserID: user, Reason: "fraud", BlockedBy: admin, At: at}, api.EventUserBlocked},
		{domain.UserUnblocked{UserID: user, UnblockedBy: admin, At: at}, api.EventUserUnblocked},
		{domain.UserRoleGranted{UserID: user, Role: domain.RoleSupportAgent, GrantedBy: admin, At: at}, api.EventUserRoleGranted},
		{domain.UserRoleRevoked{UserID: user, Role: domain.RoleSupportAgent, RevokedBy: admin, At: at}, api.EventUserRoleRevoked},
		{domain.SessionStarted{SessionID: session, UserID: user, At: at}, api.EventSessionStarted},
		{domain.SessionRevoked{SessionID: session, UserID: user, Reason: domain.RevokedByReuse, At: at}, api.EventSessionRevoked},
	}

	codec := postgres.NewEventCodec()
	for _, tc := range cases {
		assert.Equal(t, tc.name, tc.event.EventName())
		raw, err := codec.Encode(tc.event)
		require.NoError(t, err, tc.name)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))
		assert.Equal(t, "2026-09-15T12:00:00Z", payload["occurred_at"], tc.name)
	}

	raw, err := codec.Encode(domain.UserBlocked{UserID: user, Reason: "fraud", BlockedBy: admin, At: at})
	require.NoError(t, err)
	var blocked api.UserBlockedV1
	require.NoError(t, json.Unmarshal(raw, &blocked))
	assert.Equal(t, api.UserBlockedV1{UserID: user.String(), Reason: "fraud", BlockedBy: admin.String(), OccurredAt: at}, blocked)
}
