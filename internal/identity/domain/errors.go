package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

var (
	ErrInvalidEmail        = kernel.Validation("IDENTITY_INVALID_EMAIL", "email is invalid")
	ErrWeakPassword        = kernel.Validation("IDENTITY_WEAK_PASSWORD", "password must be 8-128 characters long and contain letters and digits")
	ErrInvalidRole         = kernel.Validation("IDENTITY_INVALID_ROLE", "role is unknown")
	ErrReasonRequired      = kernel.Validation("IDENTITY_REASON_REQUIRED", "reason is required")
	ErrInvalidSecret       = kernel.Validation("IDENTITY_INVALID_SECRET", "secret must not be empty")
	ErrInvalidPasswordHash = kernel.Validation("IDENTITY_INVALID_PASSWORD_HASH", "password hash must not be empty")
	ErrInvalidTTL          = kernel.Validation("IDENTITY_INVALID_TTL", "ttl must be positive")

	ErrUserNotFound      = kernel.NotFound("IDENTITY_USER_NOT_FOUND", "user not found")
	ErrSessionNotFound   = kernel.NotFound("IDENTITY_SESSION_NOT_FOUND", "session not found")
	ErrChallengeNotFound = kernel.NotFound("IDENTITY_CHALLENGE_NOT_FOUND", "confirmation challenge not found")

	ErrEmailTaken = kernel.Conflict("IDENTITY_EMAIL_TAKEN", "email is already registered")

	ErrUserBlocked              = kernel.Forbidden("IDENTITY_USER_BLOCKED", "user is blocked")
	ErrEmailNotConfirmed        = kernel.Forbidden("IDENTITY_EMAIL_NOT_CONFIRMED", "email is not confirmed")
	ErrInvalidCredentials       = kernel.Unauthenticated("IDENTITY_INVALID_CREDENTIALS", "invalid credentials")
	ErrInvalidConfirmationToken = kernel.Unauthenticated("IDENTITY_INVALID_CONFIRMATION_TOKEN", "confirmation token is invalid")
	ErrSessionRevoked           = kernel.Unauthenticated("IDENTITY_SESSION_REVOKED", "session is revoked")
	ErrSessionExpired           = kernel.Unauthenticated("IDENTITY_SESSION_EXPIRED", "session has expired")
	ErrRefreshTokenReused       = kernel.Unauthenticated("IDENTITY_REFRESH_TOKEN_REUSED", "refresh token was already used, session is revoked")
	ErrInvalidRefresh           = kernel.Unauthenticated("IDENTITY_INVALID_REFRESH_TOKEN", "refresh token is invalid")

	ErrChallengeExpired      = kernel.BusinessRule("IDENTITY_CHALLENGE_EXPIRED", "confirmation link has expired")
	ErrChallengeExhausted    = kernel.BusinessRule("IDENTITY_CHALLENGE_EXHAUSTED", "too many invalid attempts, register again to receive a new link")
	ErrChallengeAlreadyUsed  = kernel.BusinessRule("IDENTITY_CHALLENGE_USED", "confirmation link was already used")
	ErrEmailAlreadyConfirmed = kernel.BusinessRule("IDENTITY_EMAIL_ALREADY_CONFIRMED", "email is already confirmed")
	ErrUserAlreadyBlocked    = kernel.BusinessRule("IDENTITY_USER_ALREADY_BLOCKED", "user is already blocked")
	ErrUserNotBlocked        = kernel.BusinessRule("IDENTITY_USER_NOT_BLOCKED", "user is not blocked")
	ErrCannotBlockSelf       = kernel.BusinessRule("IDENTITY_CANNOT_BLOCK_SELF", "user cannot block themselves")
	ErrBaseRoleRequired      = kernel.BusinessRule("IDENTITY_BASE_ROLE_REQUIRED", "buyer role cannot be revoked")
)
