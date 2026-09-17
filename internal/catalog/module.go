package catalog

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const moduleName = "catalog"

type Dependencies struct {
	Pool        *pgxpool.Pool
	Clock       application.Clock
	Policy      application.Policy
	Sellers     application.SellerDirectory
	Storage     application.ObjectStorage
	Prober      application.ImageProber
	Limiter     application.ImportLimiter
	Responder   *httpx.Responder
	Idempotency httpx.Middleware
	Logger      *slog.Logger
	Metrics     cqrs.Metrics
}

type Module struct {
	api *httpapi.API
}

func NewModule(d Dependencies) *Module {
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock, d.Sellers, d.Policy)
	reads := postgres.NewReadModel(d.Pool)

	handlers := httpapi.Handlers{
		CreateCategory:      decorate(d, command.NewCreateCategoryHandler(base)),
		RenameCategory:      decorate(d, command.NewRenameCategoryHandler(base)),
		DefineAttribute:     decorate(d, command.NewDefineAttributeHandler(base)),
		RemoveAttribute:     decorate(d, command.NewRemoveAttributeHandler(base)),
		CreateProduct:       decorate(d, command.NewCreateProductHandler(base)),
		UpdateProduct:       decorate(d, command.NewUpdateProductHandler(base)),
		SubmitProduct:       decorate(d, command.NewSubmitProductHandler(base)),
		PublishProduct:      decorate(d, command.NewPublishProductHandler(base)),
		RejectProduct:       decorate(d, command.NewRejectProductHandler(base)),
		RequestImageUpload:  decorate(d, command.NewRequestImageUploadHandler(base, d.Storage)),
		ConfirmImageUpload:  decorate(d, command.NewConfirmImageUploadHandler(base, d.Storage, d.Prober)),
		RemoveImage:         decorate(d, command.NewRemoveImageHandler(base)),
		ReorderImages:       decorate(d, command.NewReorderImagesHandler(base)),
		CreateVariantGroup:  decorate(d, command.NewCreateVariantGroupHandler(base)),
		AddVariantMember:    decorate(d, command.NewAddVariantMemberHandler(base)),
		RemoveVariantMember: decorate(d, command.NewRemoveVariantMemberHandler(base)),
		CreateOffer:         decorate(d, command.NewCreateOfferHandler(base)),
		UpdateOfferTerms:    decorate(d, command.NewUpdateOfferTermsHandler(base)),
		SetOfferStatus:      decorate(d, command.NewSetOfferStatusHandler(base)),
		RequestImportUpload: decorate(d, command.NewRequestImportUploadHandler(base, d.Storage)),
		ScheduleImport:      decorate(d, command.NewScheduleImportHandler(base, d.Storage, d.Limiter)),

		ListCategories:  decorate(d, query.NewListCategoriesHandler(reads)),
		GetCategory:     decorate(d, query.NewGetCategoryHandler(reads)),
		GetProduct:      decorate(d, query.NewGetProductHandler(reads, d.Sellers, d.Storage)),
		ListSellerProds: decorate(d, query.NewListSellerProductsHandler(reads, d.Sellers)),
		ModerationQueue: decorate(d, query.NewListModerationQueueHandler(reads)),
		ProductOffers:   decorate(d, query.NewListProductOffersHandler(reads, d.Sellers)),
		SellerOffers:    decorate(d, query.NewListSellerOffersHandler(reads, d.Sellers)),
		GetImportJob:    decorate(d, query.NewGetImportJobHandler(reads, d.Sellers, d.Storage)),
		ListImportJobs:  decorate(d, query.NewListImportJobsHandler(reads, d.Sellers)),
	}

	return &Module{api: httpapi.NewAPI(handlers, d.Responder, d.Idempotency)}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}
