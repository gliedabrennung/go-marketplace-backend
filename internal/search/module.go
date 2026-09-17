package search

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const moduleName = "search"

type Dependencies struct {
	Pool        *pgxpool.Pool
	Clock       application.Clock
	Policy      application.Policy
	Responder   *httpx.Responder
	Idempotency httpx.Middleware
	Logger      *slog.Logger
	Metrics     cqrs.Metrics
}

type Module struct {
	api *httpapi.API
}

func NewModule(d Dependencies) *Module {
	base := command.NewBase(postgres.NewIndex(d.Pool), d.Clock, d.Policy)
	jobs := postgres.NewReindexJobs(d.Pool)
	reads := postgres.NewReadModel(d.Pool)

	handlers := httpapi.Handlers{
		SearchProducts: decorate(d, query.NewSearchProductsHandler(reads, d.Policy)),
		CategoryFacets: decorate(d, query.NewCategoryFacetsHandler(reads, d.Policy)),
		RequestReindex: decorate(d, command.NewRequestReindexHandler(base, jobs)),
		GetReindexJob:  decorate(d, command.NewGetReindexJobHandler(jobs)),
	}
	return &Module{api: httpapi.NewAPI(handlers, d.Responder, d.Idempotency)}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}
