package settlement

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	orderingapi "github.com/gliedabrennung/go-marketplace-backend/internal/ordering/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

const accrualConsumer = "settlement.accrue_on_order_completed"

type WorkerDependencies struct {
	Pool    *pgxpool.Pool
	Clock   application.Clock
	Policy  application.Policy
	Sellers application.CommissionRates
	Logger  *slog.Logger
	Metrics cqrs.Metrics
}

type Worker struct {
	logger *slog.Logger
	accrue cqrs.Handler[command.AccrueSettlement, int]
}

func NewWorker(d WorkerDependencies) *Worker {
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool), d.Clock, d.Sellers, d.Policy)
	return &Worker{
		logger: d.Logger,
		accrue: cqrs.Decorate(moduleName, command.NewAccrueSettlementHandler(base), d.Logger, d.Metrics),
	}
}

func (w *Worker) Subscriptions() []outbox.Subscription {
	return []outbox.Subscription{{
		Consumer:   accrualConsumer,
		EventNames: []string{orderingapi.EventOrderCompleted},
		Handle:     w.orderCompleted,
	}}
}

func (w *Worker) orderCompleted(ctx context.Context, msg outbox.Message) error {
	var event orderingapi.OrderV1
	if err := json.Unmarshal(msg.Payload, &event); err != nil || event.OrderID == "" {
		w.logger.ErrorContext(ctx, "skipping malformed event", "event_name", msg.EventName, "outbox_id", msg.ID, "err", err)
		return nil
	}
	items := make([]command.AccrueItem, 0, len(event.Items))
	for _, item := range event.Items {
		items = append(items, command.AccrueItem{SellerID: item.SellerID, CategoryID: item.CategoryID, Amount: item.Total})
	}
	_, err := w.accrue.Handle(ctx, command.AccrueSettlement{OrderID: event.OrderID, Currency: event.Currency, Items: items})
	return err
}
