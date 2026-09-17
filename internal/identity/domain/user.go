package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type UserStatus string

const (
	UserStatusPending UserStatus = "pending_verification"
	UserStatusActive  UserStatus = "active"
	UserStatusBlocked UserStatus = "blocked"
)

type User struct {
	id            kernel.UserID
	email         Email
	emailVerified bool
	passwordHash  PasswordHash
	roles         []Role
	status        UserStatus
	blockReason   string
	createdAt     time.Time
	updatedAt     time.Time
	version       int

	events kernel.EventBuffer
}

func RegisterWithEmail(id kernel.UserID, email Email, hash PasswordHash, now time.Time) (*User, error) {
	if id.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	if email.IsZero() {
		return nil, ErrInvalidEmail
	}
	if hash.IsZero() {
		return nil, ErrInvalidPasswordHash
	}
	u := &User{
		id:           id,
		email:        email,
		passwordHash: hash,
		roles:        []Role{RoleBuyer},
		status:       UserStatusPending,
		createdAt:    now,
		updatedAt:    now,
	}
	u.events.Record(UserRegistered{UserID: id, At: now})
	return u, nil
}

func (u *User) ConfirmEmail(now time.Time) error {
	if u.emailVerified {
		return ErrEmailAlreadyConfirmed
	}
	u.emailVerified = true
	if u.status == UserStatusPending {
		u.status = UserStatusActive
	}
	u.updatedAt = now
	u.events.Record(UserEmailConfirmed{UserID: u.id, At: now})
	return nil
}

func (u *User) EnsureCanSignIn() error {
	switch u.status {
	case UserStatusBlocked:
		return ErrUserBlocked
	case UserStatusPending:
		return ErrEmailNotConfirmed
	default:
		return nil
	}
}

func (u *User) Block(reason string, by kernel.UserID, now time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}
	if by == u.id {
		return ErrCannotBlockSelf
	}
	if u.status == UserStatusBlocked {
		return ErrUserAlreadyBlocked
	}
	u.status = UserStatusBlocked
	u.blockReason = reason
	u.updatedAt = now
	u.events.Record(UserBlocked{UserID: u.id, Reason: reason, BlockedBy: by, At: now})
	return nil
}

func (u *User) Unblock(by kernel.UserID, now time.Time) error {
	if u.status != UserStatusBlocked {
		return ErrUserNotBlocked
	}
	u.status = UserStatusPending
	if u.emailVerified {
		u.status = UserStatusActive
	}
	u.blockReason = ""
	u.updatedAt = now
	u.events.Record(UserUnblocked{UserID: u.id, UnblockedBy: by, At: now})
	return nil
}

func (u *User) GrantRole(role Role, by kernel.UserID, now time.Time) error {
	if _, err := ParseRole(string(role)); err != nil {
		return err
	}
	if u.HasRole(role) {
		return nil
	}
	u.roles = append(u.roles, role)
	u.updatedAt = now
	u.events.Record(UserRoleGranted{UserID: u.id, Role: role, GrantedBy: by, At: now})
	return nil
}

func (u *User) RevokeRole(role Role, by kernel.UserID, now time.Time) error {
	if role == RoleBuyer {
		return ErrBaseRoleRequired
	}
	idx := slices.Index(u.roles, role)
	if idx < 0 {
		return nil
	}
	u.roles = slices.Delete(u.roles, idx, idx+1)
	u.updatedAt = now
	u.events.Record(UserRoleRevoked{UserID: u.id, Role: role, RevokedBy: by, At: now})
	return nil
}

func (u *User) ID() kernel.UserID { return u.id }

func (u *User) Email() Email { return u.email }

func (u *User) EmailVerified() bool { return u.emailVerified }

func (u *User) PasswordHash() PasswordHash { return u.passwordHash }

func (u *User) Status() UserStatus { return u.status }

func (u *User) BlockReason() string { return u.blockReason }

func (u *User) CreatedAt() time.Time { return u.createdAt }

func (u *User) HasRole(role Role) bool { return slices.Contains(u.roles, role) }

func (u *User) Roles() []Role { return slices.Clone(u.roles) }

func (u *User) RoleNames() []string {
	out := make([]string, len(u.roles))
	for i, r := range u.roles {
		out[i] = string(r)
	}
	return out
}

func (u *User) Version() int { return u.version }

func (u *User) AdvanceVersion() { u.version++ }

func (u *User) PullEvents() []kernel.DomainEvent { return u.events.Pull() }

type UserSnapshot struct {
	ID            string
	Email         string
	EmailVerified bool
	PasswordHash  string
	Roles         []string
	Status        string
	BlockReason   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Version       int
}

func (u *User) Snapshot() UserSnapshot {
	return UserSnapshot{
		ID:            u.id.String(),
		Email:         u.email.String(),
		EmailVerified: u.emailVerified,
		PasswordHash:  u.passwordHash.String(),
		Roles:         u.RoleNames(),
		Status:        string(u.status),
		BlockReason:   u.blockReason,
		CreatedAt:     u.createdAt,
		UpdatedAt:     u.updatedAt,
		Version:       u.version,
	}
}

func RehydrateUser(s UserSnapshot) (*User, error) {
	id, err := kernel.ParseUserID(s.ID)
	if err != nil {
		return nil, fmt.Errorf("rehydrate user: %w", err)
	}
	email, err := NewEmail(s.Email)
	if err != nil {
		return nil, fmt.Errorf("rehydrate user %s email: %w", s.ID, err)
	}
	u := &User{
		id:            id,
		email:         email,
		emailVerified: s.EmailVerified,
		passwordHash:  PasswordHash{value: s.PasswordHash},
		status:        UserStatus(s.Status),
		blockReason:   s.BlockReason,
		createdAt:     s.CreatedAt,
		updatedAt:     s.UpdatedAt,
		version:       s.Version,
	}
	for _, name := range s.Roles {
		role, err := ParseRole(name)
		if err != nil {
			return nil, fmt.Errorf("rehydrate user %s roles: %w", s.ID, err)
		}
		u.roles = append(u.roles, role)
	}
	return u, nil
}
