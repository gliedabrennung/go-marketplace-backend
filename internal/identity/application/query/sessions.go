package query

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type ListSessions struct {
	Actor  auth.Principal
	Limit  int
	Cursor string
}

type SessionView struct {
	ID         string
	DeviceName string
	UserAgent  string
	IP         string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
	Current    bool
}

type SessionReadModel interface {
	ListActiveByUser(ctx context.Context, userID string, now time.Time, limit int, after *pagination.Keyset) (pagination.Page[SessionView], error)
}

type Clock interface {
	Now() time.Time
}

type ListSessionsHandler struct {
	reader SessionReadModel
	clock  Clock
}

func NewListSessionsHandler(reader SessionReadModel, clock Clock) *ListSessionsHandler {
	return &ListSessionsHandler{reader: reader, clock: clock}
}

func (h *ListSessionsHandler) Handle(ctx context.Context, q ListSessions) (pagination.Page[SessionView], error) {
	if q.Actor.UserID == "" {
		return pagination.Page[SessionView]{}, auth.ErrUnauthenticated
	}
	keyset, ok, err := pagination.DecodeKeyset(q.Cursor)
	if err != nil {
		return pagination.Page[SessionView]{}, err
	}
	var after *pagination.Keyset
	if ok {
		after = &keyset
	}
	page, err := h.reader.ListActiveByUser(ctx, q.Actor.UserID, h.clock.Now(), pagination.NormalizeLimit(q.Limit), after)
	if err != nil {
		return pagination.Page[SessionView]{}, err
	}
	for i := range page.Items {
		page.Items[i].Current = page.Items[i].ID == q.Actor.SessionID
	}
	return page, nil
}
