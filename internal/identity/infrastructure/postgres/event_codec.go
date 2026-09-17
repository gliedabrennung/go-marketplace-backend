package postgres

import (
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

func NewEventCodec() *outbox.Codec {
	c := outbox.NewCodec()
	outbox.Register(c, func(e domain.UserRegistered) any {
		return api.UserRegisteredV1{UserID: e.UserID.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.UserEmailConfirmed) any {
		return api.UserEmailConfirmedV1{UserID: e.UserID.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.UserBlocked) any {
		return api.UserBlockedV1{UserID: e.UserID.String(), Reason: e.Reason, BlockedBy: e.BlockedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.UserUnblocked) any {
		return api.UserUnblockedV1{UserID: e.UserID.String(), UnblockedBy: e.UnblockedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.UserRoleGranted) any {
		return api.UserRoleChangedV1{UserID: e.UserID.String(), Role: e.Role.String(), ChangedBy: e.GrantedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.UserRoleRevoked) any {
		return api.UserRoleChangedV1{UserID: e.UserID.String(), Role: e.Role.String(), ChangedBy: e.RevokedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SessionStarted) any {
		return api.SessionStartedV1{SessionID: e.SessionID.String(), UserID: e.UserID.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SessionRevoked) any {
		return api.SessionRevokedV1{SessionID: e.SessionID.String(), UserID: e.UserID.String(), Reason: string(e.Reason), OccurredAt: e.At.UTC()}
	})
	return c
}
