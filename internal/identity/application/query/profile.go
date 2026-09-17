package query

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type GetProfile struct {
	Actor auth.Principal
}

type Profile struct {
	ID            string
	Email         string
	EmailVerified bool
	Roles         []string
	Status        string
	CreatedAt     time.Time
}

type ProfileReadModel interface {
	Profile(ctx context.Context, userID string) (Profile, error)
}

type GetProfileHandler struct {
	reader ProfileReadModel
}

func NewGetProfileHandler(reader ProfileReadModel) *GetProfileHandler {
	return &GetProfileHandler{reader: reader}
}

func (h *GetProfileHandler) Handle(ctx context.Context, q GetProfile) (Profile, error) {
	if q.Actor.UserID == "" {
		return Profile{}, auth.ErrUnauthenticated
	}
	return h.reader.Profile(ctx, q.Actor.UserID)
}
