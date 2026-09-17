package command_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/importfile"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/media"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/infrastructure/memory"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
)

var ctx = context.Background()

var errImportsLimited = kernel.RateLimited("CATALOG_IMPORT_RATE_LIMITED", "too many import jobs")

type sellerDirectory struct {
	mu      sync.Mutex
	sellers map[string]sellerapi.SellerInfo
	members map[string]map[string]string
}

func (d *sellerDirectory) Seller(_ context.Context, sellerID string) (sellerapi.SellerInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	info, ok := d.sellers[sellerID]
	if !ok {
		return sellerapi.SellerInfo{}, sellerapi.ErrSellerNotFound
	}
	return info, nil
}

func (d *sellerDirectory) MemberRole(_ context.Context, sellerID, userID string) (string, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	role, ok := d.members[sellerID][userID]
	return role, ok, nil
}

func (d *sellerDirectory) add(owner auth.Principal) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := kernel.NewSellerID().String()
	d.sellers[id] = sellerapi.SellerInfo{ID: id, Status: "active", CanSell: true, PayoutsAllowed: true}
	d.members[id] = map[string]string{owner.UserID: sellerapi.RoleSellerAdmin}
	return id
}

func (d *sellerDirectory) suspend(sellerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	info := d.sellers[sellerID]
	info.Status, info.CanSell = "suspended", false
	d.sellers[sellerID] = info
}

type importLimiter struct {
	mu        sync.Mutex
	remaining int
}

func (l *importLimiter) Allow(context.Context, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.remaining <= 0 {
		return errImportsLimited
	}
	l.remaining--
	return nil
}

type env struct {
	store   *memory.Store
	objects *memory.Objects
	clock   *clock.Manual
	sellers *sellerDirectory
	limiter *importLimiter

	createCategory  *command.CreateCategoryHandler
	renameCategory  *command.RenameCategoryHandler
	defineAttribute *command.DefineAttributeHandler
	removeAttribute *command.RemoveAttributeHandler
	createProduct   *command.CreateProductHandler
	updateProduct   *command.UpdateProductHandler
	submitProduct   *command.SubmitProductHandler
	publishProduct  *command.PublishProductHandler
	rejectProduct   *command.RejectProductHandler
	requestImage    *command.RequestImageUploadHandler
	confirmImage    *command.ConfirmImageUploadHandler
	removeImage     *command.RemoveImageHandler
	reorderImages   *command.ReorderImagesHandler
	processImage    *command.ProcessImageHandler
	createGroup     *command.CreateVariantGroupHandler
	addMember       *command.AddVariantMemberHandler
	removeMember    *command.RemoveVariantMemberHandler
	createOffer     *command.CreateOfferHandler
	updateOffer     *command.UpdateOfferTermsHandler
	setOfferStatus  *command.SetOfferStatusHandler
	requestImport   *command.RequestImportUploadHandler
	scheduleImport  *command.ScheduleImportHandler
	processImport   *command.ProcessImportHandler
	requeueImports  *command.RequeueStaleImportsHandler

	listCategories  *query.ListCategoriesHandler
	getCategory     *query.GetCategoryHandler
	getProduct      *query.GetProductHandler
	sellerProducts  *query.ListSellerProductsHandler
	moderationQueue *query.ListModerationQueueHandler
	productOffers   *query.ListProductOffersHandler
	sellerOffers    *query.ListSellerOffersHandler
	getImport       *query.GetImportJobHandler
	listImports     *query.ListImportJobsHandler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		store:   memory.NewStore(),
		objects: memory.NewObjects(),
		clock:   clock.NewManual(time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)),
		sellers: &sellerDirectory{sellers: map[string]sellerapi.SellerInfo{}, members: map[string]map[string]string{}},
		limiter: &importLimiter{remaining: 100},
	}
	policy := application.DefaultPolicy()
	policy.ImportProgress = 2
	base := command.NewBase(memory.NewUnitOfWork(e.store), e.clock, e.sellers, policy)

	e.createCategory = command.NewCreateCategoryHandler(base)
	e.renameCategory = command.NewRenameCategoryHandler(base)
	e.defineAttribute = command.NewDefineAttributeHandler(base)
	e.removeAttribute = command.NewRemoveAttributeHandler(base)
	e.createProduct = command.NewCreateProductHandler(base)
	e.updateProduct = command.NewUpdateProductHandler(base)
	e.submitProduct = command.NewSubmitProductHandler(base)
	e.publishProduct = command.NewPublishProductHandler(base)
	e.rejectProduct = command.NewRejectProductHandler(base)
	e.requestImage = command.NewRequestImageUploadHandler(base, e.objects)
	e.confirmImage = command.NewConfirmImageUploadHandler(base, e.objects, media.NewProber())
	e.removeImage = command.NewRemoveImageHandler(base)
	e.reorderImages = command.NewReorderImagesHandler(base)
	e.processImage = command.NewProcessImageHandler(base, media.NewThumbnailer(e.objects))
	e.createGroup = command.NewCreateVariantGroupHandler(base)
	e.addMember = command.NewAddVariantMemberHandler(base)
	e.removeMember = command.NewRemoveVariantMemberHandler(base)
	e.createOffer = command.NewCreateOfferHandler(base)
	e.updateOffer = command.NewUpdateOfferTermsHandler(base)
	e.setOfferStatus = command.NewSetOfferStatusHandler(base)
	e.requestImport = command.NewRequestImportUploadHandler(base, e.objects)
	e.scheduleImport = command.NewScheduleImportHandler(base, e.objects, e.limiter)
	e.processImport = command.NewProcessImportHandler(base, importfile.NewSource(e.objects), importfile.NewReportWriter(e.objects))
	e.requeueImports = command.NewRequeueStaleImportsHandler(base, e.store)

	e.listCategories = query.NewListCategoriesHandler(e.store)
	e.getCategory = query.NewGetCategoryHandler(e.store)
	e.getProduct = query.NewGetProductHandler(e.store, e.sellers, e.objects)
	e.sellerProducts = query.NewListSellerProductsHandler(e.store, e.sellers)
	e.moderationQueue = query.NewListModerationQueueHandler(e.store)
	e.productOffers = query.NewListProductOffersHandler(e.store, e.sellers)
	e.sellerOffers = query.NewListSellerOffersHandler(e.store, e.sellers)
	e.getImport = query.NewGetImportJobHandler(e.store, e.sellers, e.objects)
	e.listImports = query.NewListImportJobsHandler(e.store, e.sellers)
	return e
}

func user(roles ...string) auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: append([]string{"buyer"}, roles...)}
}

func (e *env) seller() (auth.Principal, string) {
	owner := user()
	return owner, e.sellers.add(owner)
}

type tree struct {
	root        string
	electronics string
	phones      string
}

func (e *env) tree(t *testing.T) tree {
	t.Helper()
	admin := user("platform_admin")
	create := func(parent, name, slug string) string {
		res, err := e.createCategory.Handle(ctx, command.CreateCategory{Actor: admin, ParentID: parent, Name: name, Slug: slug})
		require.NoError(t, err)
		return res.CategoryID
	}
	define := func(categoryID string, cmd command.DefineAttribute) {
		cmd.Actor, cmd.CategoryID = admin, categoryID
		_, err := e.defineAttribute.Handle(ctx, cmd)
		require.NoError(t, err)
	}
	var tr tree
	tr.root = create("", "Все товары", "all")
	define(tr.root, command.DefineAttribute{Code: "brand_country", Name: "Страна бренда", Type: "string"})
	tr.electronics = create(tr.root, "Электроника", "electronics")
	define(tr.electronics, command.DefineAttribute{Code: "warranty_months", Name: "Гарантия", Type: "number", Required: true})
	tr.phones = create(tr.electronics, "Смартфоны", "smartphones")
	define(tr.phones, command.DefineAttribute{Code: "color", Name: "Цвет", Type: "enum", Filterable: true, Options: []string{"black", "white", "red"}})
	define(tr.phones, command.DefineAttribute{Code: "memory_gb", Name: "Память", Type: "unit", Unit: "GB", Filterable: true, Required: true})
	define(tr.phones, command.DefineAttribute{Code: "nfc", Name: "NFC", Type: "boolean", Filterable: true})
	define(tr.phones, command.DefineAttribute{Code: "model", Name: "Модель", Type: "string"})
	return tr
}

func phone(color string) map[string]string {
	return map[string]string{"color": color, "memory_gb": "256", "nfc": "true", "warranty_months": "12", "model": "X1"}
}

func productCommand(owner auth.Principal, sellerID, categoryID string, attributes map[string]string) command.CreateProduct {
	return command.CreateProduct{
		Actor: owner, SellerID: sellerID, CategoryID: categoryID,
		Title: "Смартфон Nova X", Description: "Флагман с отличной камерой", Brand: "Nova", Attributes: attributes,
	}
}

func (e *env) draft(t *testing.T, owner auth.Principal, sellerID, categoryID string, attributes map[string]string) string {
	t.Helper()
	res, err := e.createProduct.Handle(ctx, productCommand(owner, sellerID, categoryID, attributes))
	require.NoError(t, err)
	e.clock.Advance(time.Second)
	return res.ProductID
}

func (e *env) published(t *testing.T, owner auth.Principal, sellerID, categoryID, color string) string {
	t.Helper()
	id := e.draft(t, owner, sellerID, categoryID, phone(color))
	_, err := e.submitProduct.Handle(ctx, command.SubmitProduct{Actor: owner, ProductID: id})
	require.NoError(t, err)
	_, err = e.publishProduct.Handle(ctx, command.PublishProduct{Actor: user("content_moderator"), ProductID: id})
	require.NoError(t, err)
	return id
}

func (e *env) actions() []string {
	var out []string
	for _, entry := range e.store.AuditEntries() {
		out = append(out, entry.Action)
	}
	return out
}
