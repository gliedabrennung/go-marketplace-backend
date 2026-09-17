package seller

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const moduleName = "seller"

type Dependencies struct {
	Pool             *pgxpool.Pool
	Clock            application.Clock
	RatingPolicy     domain.RatingPolicy
	CommissionPolicy domain.CommissionPolicy
	Responder        *httpx.Responder
	Idempotency      httpx.Middleware
	Logger           *slog.Logger
	Metrics          cqrs.Metrics
}

type Module struct {
	api              *httpapi.API
	directory        *postgres.Directory
	applyPerformance cqrs.Handler[command.ApplyPerformance, command.ApplyPerformanceResult]
}

func NewModule(d Dependencies) *Module {
	unit := command.NewUnit(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock)
	reads := postgres.NewReadModel(d.Pool)

	handlers := httpapi.Handlers{
		OpenApplication:         decorate(d, command.NewOpenApplicationHandler(unit)),
		UpdateLegalDetails:      decorate(d, command.NewUpdateLegalDetailsHandler(unit)),
		ChangeBankAccount:       decorate(d, command.NewChangeBankAccountHandler(unit)),
		AttachDocument:          decorate(d, command.NewAttachDocumentHandler(unit)),
		SubmitApplication:       decorate(d, command.NewSubmitApplicationHandler(unit)),
		AddMember:               decorate(d, command.NewAddMemberHandler(unit)),
		RemoveMember:            decorate(d, command.NewRemoveMemberHandler(unit)),
		ApproveApplication:      decorate(d, command.NewApproveApplicationHandler(unit)),
		RejectApplication:       decorate(d, command.NewRejectApplicationHandler(unit)),
		VerifyBankAccount:       decorate(d, command.NewVerifyBankAccountHandler(unit)),
		SuspendSeller:           decorate(d, command.NewSuspendSellerHandler(unit)),
		ReinstateSeller:         decorate(d, command.NewReinstateSellerHandler(unit)),
		TerminateSeller:         decorate(d, command.NewTerminateSellerHandler(unit)),
		SetCommissionOverride:   decorate(d, command.NewSetCommissionOverrideHandler(unit)),
		ClearCommissionOverride: decorate(d, command.NewClearCommissionOverrideHandler(unit)),
		SetCategoryCommission:   decorate(d, command.NewSetCategoryCommissionHandler(unit)),
		GetSeller:               decorate(d, query.NewGetSellerHandler(reads)),
		ListMySellers:           decorate(d, query.NewListMySellersHandler(reads)),
		ListApplications:        decorate(d, query.NewListApplicationsHandler(reads)),
	}

	return &Module{
		api:       httpapi.NewAPI(handlers, d.Responder, d.Idempotency),
		directory: postgres.NewDirectory(d.Pool, d.CommissionPolicy),
		applyPerformance: decorate(d,
			command.NewApplyPerformanceHandler(unit, d.RatingPolicy)),
	}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func (m *Module) Directory() api.Directory {
	return m.directory
}

func (m *Module) ApplyPerformance() cqrs.Handler[command.ApplyPerformance, command.ApplyPerformanceResult] {
	return m.applyPerformance
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}
