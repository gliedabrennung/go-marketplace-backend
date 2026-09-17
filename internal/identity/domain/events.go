package domain

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type UserRegistered struct {
	UserID kernel.UserID
	At     time.Time
}

func (e UserRegistered) EventName() string     { return "identity.user_registered.v1" }
func (e UserRegistered) AggregateID() string   { return e.UserID.String() }
func (e UserRegistered) OccurredAt() time.Time { return e.At }

type UserEmailConfirmed struct {
	UserID kernel.UserID
	At     time.Time
}

func (e UserEmailConfirmed) EventName() string     { return "identity.user_email_confirmed.v1" }
func (e UserEmailConfirmed) AggregateID() string   { return e.UserID.String() }
func (e UserEmailConfirmed) OccurredAt() time.Time { return e.At }

type UserBlocked struct {
	UserID    kernel.UserID
	Reason    string
	BlockedBy kernel.UserID
	At        time.Time
}

func (e UserBlocked) EventName() string     { return "identity.user_blocked.v1" }
func (e UserBlocked) AggregateID() string   { return e.UserID.String() }
func (e UserBlocked) OccurredAt() time.Time { return e.At }

type UserUnblocked struct {
	UserID      kernel.UserID
	UnblockedBy kernel.UserID
	At          time.Time
}

func (e UserUnblocked) EventName() string     { return "identity.user_unblocked.v1" }
func (e UserUnblocked) AggregateID() string   { return e.UserID.String() }
func (e UserUnblocked) OccurredAt() time.Time { return e.At }

type UserRoleGranted struct {
	UserID    kernel.UserID
	Role      Role
	GrantedBy kernel.UserID
	At        time.Time
}

func (e UserRoleGranted) EventName() string     { return "identity.user_role_granted.v1" }
func (e UserRoleGranted) AggregateID() string   { return e.UserID.String() }
func (e UserRoleGranted) OccurredAt() time.Time { return e.At }

type UserRoleRevoked struct {
	UserID    kernel.UserID
	Role      Role
	RevokedBy kernel.UserID
	At        time.Time
}

func (e UserRoleRevoked) EventName() string     { return "identity.user_role_revoked.v1" }
func (e UserRoleRevoked) AggregateID() string   { return e.UserID.String() }
func (e UserRoleRevoked) OccurredAt() time.Time { return e.At }

type SessionStarted struct {
	SessionID SessionID
	UserID    kernel.UserID
	At        time.Time
}

func (e SessionStarted) EventName() string     { return "identity.session_started.v1" }
func (e SessionStarted) AggregateID() string   { return e.SessionID.String() }
func (e SessionStarted) OccurredAt() time.Time { return e.At }

type SessionRevoked struct {
	SessionID SessionID
	UserID    kernel.UserID
	Reason    RevokeReason
	At        time.Time
}

func (e SessionRevoked) EventName() string     { return "identity.session_revoked.v1" }
func (e SessionRevoked) AggregateID() string   { return e.SessionID.String() }
func (e SessionRevoked) OccurredAt() time.Time { return e.At }
