package settlement

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const moduleName = "settlement"

type Dependencies struct {
	Pool      *pgxpool.Pool
	Clock     application.Clock
	Policy    application.Policy
	Sellers   application.CommissionRates
	Responder *httpx.Responder
	Logger    *slog.Logger
	Metrics   cqrs.Metrics
}

type Module struct {
	api *httpapi.API
}

func NewModule(d Dependencies) *Module {
	reads := postgres.NewReadModel(d.Pool)
	handlers := httpapi.Handlers{
		Report: cqrs.Decorate(moduleName, query.NewGetSellerSettlementsHandler(reads, d.Sellers), d.Logger, d.Metrics),
	}
	return &Module{api: httpapi.NewAPI(handlers, d.Responder)}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}
