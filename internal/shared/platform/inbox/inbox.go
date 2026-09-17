package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/postgres"
)

type Guard struct {
	pool *pgxpool.Pool
}

func NewGuard(pool *pgxpool.Pool) *Guard {
	return &Guard{pool: pool}
}

func (g *Guard) Once(ctx context.Context, consumerGroup, messageID string, fn func(ctx context.Context, tx pgx.Tx) error) (bool, error) {
	processed := false
	err := postgres.InTx(ctx, g.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			"INSERT INTO platform.processed_messages (consumer_group, message_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			consumerGroup, messageID,
		)
		if err != nil {
			return fmt.Errorf("register processed message: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if err := fn(ctx, tx); err != nil {
			return err
		}
		processed = true
		return nil
	})
	return processed, err
}

func DeleteProcessedBefore(ctx context.Context, q postgres.Querier, before time.Time) (int64, error) {
	tag, err := q.Exec(ctx, "DELETE FROM platform.processed_messages WHERE processed_at < $1", before)
	if err != nil {
		return 0, fmt.Errorf("delete processed messages: %w", err)
	}
	return tag.RowsAffected(), nil
}
