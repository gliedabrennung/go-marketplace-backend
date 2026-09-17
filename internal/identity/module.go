package identity

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/security"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const moduleName = "identity"

type Dependencies struct {
	Pool        *pgxpool.Pool
	Limiter     application.AttemptLimiter
	Tokens      application.AccessTokenIssuer
	Sender      application.ConfirmationSender
	Hasher      application.PasswordHasher
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
	uow := postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter())
	reads := postgres.NewReadModel(d.Pool)
	secrets := security.Secrets{}
	sessions := command.NewSessionStarter(uow, secrets, d.Tokens, d.Policy)
	confirmations := command.NewEmailConfirmations(uow, secrets, d.Sender, d.Policy)

	handlers := httpapi.Handlers{
		RegisterWithEmail: decorate(d,
			command.NewRegisterWithEmailHandler(uow, d.Hasher, confirmations, d.Limiter, d.Clock)),
		ConfirmEmail: decorate(d,
			command.NewConfirmEmailHandler(uow, d.Clock)),
		SignInWithEmail: decorate(d,
			command.NewSignInWithEmailHandler(uow, d.Hasher, d.Limiter, d.Clock, sessions)),
		RefreshSession: decorate(d,
			command.NewRefreshSessionHandler(uow, secrets, d.Clock, sessions)),
		RevokeSession: decorate(d,
			command.NewRevokeSessionHandler(uow, d.Clock)),
		BlockUser: decorate(d,
			command.NewBlockUserHandler(uow, d.Clock)),
		UnblockUser: decorate(d,
			command.NewUnblockUserHandler(uow, d.Clock)),
		GrantRole: decorate(d,
			command.NewGrantRoleHandler(uow, d.Clock)),
		RevokeRole: decorate(d,
			command.NewRevokeRoleHandler(uow, d.Clock)),
		ListSessions: decorate(d,
			query.NewListSessionsHandler(reads, d.Clock)),
		GetProfile: decorate(d,
			query.NewGetProfileHandler(reads)),
	}
	return &Module{api: httpapi.NewAPI(handlers, d.Responder, d.Idempotency)}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}
