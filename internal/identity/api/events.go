package api

import "time"

const (
	EventUserRegistered     = "identity.user_registered.v1"
	EventUserEmailConfirmed = "identity.user_email_confirmed.v1"
	EventUserBlocked        = "identity.user_blocked.v1"
	EventUserUnblocked      = "identity.user_unblocked.v1"
	EventUserRoleGranted    = "identity.user_role_granted.v1"
	EventUserRoleRevoked    = "identity.user_role_revoked.v1"
	EventSessionStarted     = "identity.session_started.v1"
	EventSessionRevoked     = "identity.session_revoked.v1"
)

type UserRegisteredV1 struct {
	UserID     string    `json:"user_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type UserEmailConfirmedV1 struct {
	UserID     string    `json:"user_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type UserBlockedV1 struct {
	UserID     string    `json:"user_id"`
	Reason     string    `json:"reason"`
	BlockedBy  string    `json:"blocked_by"`
	OccurredAt time.Time `json:"occurred_at"`
}

type UserUnblockedV1 struct {
	UserID      string    `json:"user_id"`
	UnblockedBy string    `json:"unblocked_by"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type UserRoleChangedV1 struct {
	UserID     string    `json:"user_id"`
	Role       string    `json:"role"`
	ChangedBy  string    `json:"changed_by"`
	OccurredAt time.Time `json:"occurred_at"`
}

type SessionStartedV1 struct {
	SessionID  string    `json:"session_id"`
	UserID     string    `json:"user_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type SessionRevokedV1 struct {
	SessionID  string    `json:"session_id"`
	UserID     string    `json:"user_id"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}
