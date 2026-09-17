package inventory

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const moduleName = "inventory"

type Dependencies struct {
	Pool      *pgxpool.Pool
	Clock     application.Clock
	Policy    application.Policy
	Sellers   application.SellerDirectory
	Responder *httpx.Responder
	Logger    *slog.Logger
	Metrics   cqrs.Metrics
}

type Module struct {
	api      *httpapi.API
	reserver *reserver
	reads    *postgres.ReadModel
}

func NewModule(d Dependencies) *Module {
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock, d.Sellers, d.Policy)
	reads := postgres.NewReadModel(d.Pool)

	handlers := httpapi.Handlers{
		SetStock:       decorate(d, command.NewSetStockHandler(base)),
		GetStock:       decorate(d, query.NewGetStockHandler(reads, d.Sellers)),
		ListSellerStk:  decorate(d, query.NewListSellerStockHandler(reads, d.Sellers)),
		ListMovements:  decorate(d, query.NewListMovementsHandler(reads, d.Sellers)),
		GetReservation: decorate(d, query.NewGetReservationHandler(reads)),
	}

	return &Module{
		api:   httpapi.NewAPI(handlers, d.Responder),
		reads: reads,
		reserver: &reserver{
			reserve: decorate(d, command.NewReserveStockHandler(base)),
			commit:  decorate(d, command.NewCommitReservationHandler(base)),
			release: decorate(d, command.NewReleaseReservationHandler(base)),
			restore: decorate(d, command.NewRestoreReservationHandler(base)),
		},
	}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func (m *Module) Reserver() api.Reserver { return m.reserver }

func (m *Module) Availability() api.Availability { return m.reads }

type reserver struct {
	reserve cqrs.Handler[command.ReserveStock, command.ReserveStockResult]
	commit  cqrs.Handler[command.CommitReservation, struct{}]
	release cqrs.Handler[command.ReleaseReservation, struct{}]
	restore cqrs.Handler[command.RestoreReservation, struct{}]
}

func (r *reserver) Reserve(ctx context.Context, reservationID, orderID string, lines []api.ReserveLine) (api.Reservation, error) {
	cmd := command.ReserveStock{ReservationID: reservationID, OrderID: orderID, Lines: make([]command.Line, 0, len(lines))}
	for _, line := range lines {
		cmd.Lines = append(cmd.Lines, command.Line{SKU: line.SKU, Quantity: line.Quantity})
	}
	result, err := r.reserve.Handle(ctx, cmd)
	if err != nil {
		return api.Reservation{}, err
	}
	return api.Reservation{ReservationID: result.ReservationID, ExpiresAt: result.ExpiresAt}, nil
}

func (r *reserver) Commit(ctx context.Context, reservationID string) error {
	_, err := r.commit.Handle(ctx, command.CommitReservation{ReservationID: reservationID})
	return err
}

func (r *reserver) Release(ctx context.Context, reservationID string) error {
	_, err := r.release.Handle(ctx, command.ReleaseReservation{ReservationID: reservationID})
	return err
}

func (r *reserver) Restore(ctx context.Context, reservationID string) error {
	_, err := r.restore.Handle(ctx, command.RestoreReservation{ReservationID: reservationID})
	return err
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}
