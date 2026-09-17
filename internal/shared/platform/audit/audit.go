package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type Entry struct {
	ActorID    string
	ActorRoles []string
	Action     string
	ObjectType string
	ObjectID   string
	Details    map[string]string
	OccurredAt time.Time
}

type Writer struct{}

func (Writer) Write(ctx context.Context, q postgres.Querier, e Entry) error {
	details, err := json.Marshal(e.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	_, err = q.Exec(ctx, `
		INSERT INTO platform.audit_log (actor_id, actor_roles, action, object_type, object_id, details, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ActorID, e.ActorRoles, e.Action, e.ObjectType, e.ObjectID, details, e.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}
