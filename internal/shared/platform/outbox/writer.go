package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/reqctx"
)

const (
	MetaOccurredAt = "occurred_at"
	MetaRequestID  = "request_id"
)

type Message struct {
	ID          int64
	AggregateID string
	EventName   string
	Payload     []byte
	Metadata    map[string]string
	CreatedAt   time.Time
	Attempts    int
}

type Encoder interface {
	Encode(e kernel.DomainEvent) ([]byte, error)
}

type Writer struct {
	table   string
	encoder Encoder
}

func NewWriter(schema, table string, encoder Encoder) *Writer {
	return &Writer{table: pgx.Identifier{schema, table}.Sanitize(), encoder: encoder}
}

func (w *Writer) Write(ctx context.Context, q postgres.Querier, events []kernel.DomainEvent) error {
	for _, e := range events {
		payload, err := w.encoder.Encode(e)
		if err != nil {
			return err
		}
		meta, err := json.Marshal(metadata(ctx, e))
		if err != nil {
			return fmt.Errorf("marshal outbox metadata: %w", err)
		}
		_, err = q.Exec(ctx,
			"INSERT INTO "+w.table+" (aggregate_id, event_name, payload, metadata) VALUES ($1, $2, $3, $4)",
			e.AggregateID(), e.EventName(), payload, meta,
		)
		if err != nil {
			return fmt.Errorf("insert outbox %s: %w", e.EventName(), err)
		}
	}
	return nil
}

func metadata(ctx context.Context, e kernel.DomainEvent) map[string]string {
	md := map[string]string{
		MetaOccurredAt: e.OccurredAt().UTC().Format(time.RFC3339Nano),
	}
	if id := reqctx.RequestID(ctx); id != "" {
		md[MetaRequestID] = id
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(md))
	return md
}
