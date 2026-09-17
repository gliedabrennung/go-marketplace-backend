package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/query"
)

type emailRegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type emailRegisterResponse struct {
	UserID string `json:"user_id"`
}

type emailConfirmRequest struct {
	Token string `json:"token"`
}

type emailSignInRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type blockRequest struct {
	Reason string `json:"reason"`
}

type tokensResponse struct {
	TokenType        string    `json:"token_type"`
	UserID           string    `json:"user_id"`
	SessionID        string    `json:"session_id"`
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

func toTokens(t application.AuthTokens) tokensResponse {
	return tokensResponse{
		TokenType:        "Bearer",
		UserID:           t.UserID,
		SessionID:        t.SessionID,
		AccessToken:      t.AccessToken,
		AccessExpiresAt:  t.AccessExpiresAt.UTC(),
		RefreshToken:     t.RefreshToken,
		RefreshExpiresAt: t.RefreshExpiresAt.UTC(),
	}
}

type profileResponse struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	Roles         []string  `json:"roles"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

func toProfile(p query.Profile) profileResponse {
	return profileResponse{
		ID:            p.ID,
		Email:         p.Email,
		EmailVerified: p.EmailVerified,
		Roles:         p.Roles,
		Status:        p.Status,
		CreatedAt:     p.CreatedAt.UTC(),
	}
}

type sessionResponse struct {
	ID         string    `json:"id"`
	DeviceName string    `json:"device_name"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func toSession(v query.SessionView) sessionResponse {
	return sessionResponse{
		ID:         v.ID,
		DeviceName: v.DeviceName,
		UserAgent:  v.UserAgent,
		IP:         v.IP,
		CreatedAt:  v.CreatedAt.UTC(),
		LastUsedAt: v.LastUsedAt.UTC(),
		ExpiresAt:  v.ExpiresAt.UTC(),
		Current:    v.Current,
	}
}
