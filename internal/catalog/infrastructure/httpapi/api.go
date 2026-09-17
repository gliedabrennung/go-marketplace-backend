package httpapi

import (
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type Handlers struct {
	CreateCategory      cqrs.Handler[command.CreateCategory, command.CreateCategoryResult]
	RenameCategory      cqrs.Handler[command.RenameCategory, struct{}]
	DefineAttribute     cqrs.Handler[command.DefineAttribute, struct{}]
	RemoveAttribute     cqrs.Handler[command.RemoveAttribute, struct{}]
	CreateProduct       cqrs.Handler[command.CreateProduct, command.CreateProductResult]
	UpdateProduct       cqrs.Handler[command.UpdateProduct, struct{}]
	SubmitProduct       cqrs.Handler[command.SubmitProduct, struct{}]
	PublishProduct      cqrs.Handler[command.PublishProduct, struct{}]
	RejectProduct       cqrs.Handler[command.RejectProduct, struct{}]
	RequestImageUpload  cqrs.Handler[command.RequestImageUpload, command.RequestImageUploadResult]
	ConfirmImageUpload  cqrs.Handler[command.ConfirmImageUpload, struct{}]
	RemoveImage         cqrs.Handler[command.RemoveImage, struct{}]
	ReorderImages       cqrs.Handler[command.ReorderImages, struct{}]
	CreateVariantGroup  cqrs.Handler[command.CreateVariantGroup, command.CreateVariantGroupResult]
	AddVariantMember    cqrs.Handler[command.AddVariantMember, struct{}]
	RemoveVariantMember cqrs.Handler[command.RemoveVariantMember, struct{}]
	CreateOffer         cqrs.Handler[command.CreateOffer, command.CreateOfferResult]
	UpdateOfferTerms    cqrs.Handler[command.UpdateOfferTerms, struct{}]
	SetOfferStatus      cqrs.Handler[command.SetOfferStatus, struct{}]
	RequestImportUpload cqrs.Handler[command.RequestImportUpload, command.RequestImportUploadResult]
	ScheduleImport      cqrs.Handler[command.ScheduleImport, command.ScheduleImportResult]

	ListCategories  cqrs.Handler[query.ListCategories, []query.CategoryNode]
	GetCategory     cqrs.Handler[query.GetCategory, query.CategoryView]
	GetProduct      cqrs.Handler[query.GetProduct, query.ProductView]
	ListSellerProds cqrs.Handler[query.ListSellerProducts, pagination.Page[query.ProductSummary]]
	ModerationQueue cqrs.Handler[query.ListModerationQueue, pagination.Page[query.ProductSummary]]
	ProductOffers   cqrs.Handler[query.ListProductOffers, []query.OfferView]
	SellerOffers    cqrs.Handler[query.ListSellerOffers, pagination.Page[query.OfferView]]
	GetImportJob    cqrs.Handler[query.GetImportJob, query.ImportJobView]
	ListImportJobs  cqrs.Handler[query.ListImportJobs, pagination.Page[query.ImportJobView]]
}

type API struct {
	h    Handlers
	rs   *httpx.Responder
	idem httpx.Middleware
}

func NewAPI(h Handlers, rs *httpx.Responder, idem httpx.Middleware) *API {
	return &API{h: h, rs: rs, idem: idem}
}

func (a *API) Register(rt *httpx.Router) {
	authed := httpx.RequireAuthenticated(a.rs)

	rt.HandleFunc("GET /api/v1/catalog/categories", a.listCategories)
	rt.HandleFunc("GET /api/v1/catalog/categories/{id}", a.getCategory)
	rt.HandleFunc("GET /api/v1/catalog/products/{id}", a.getProduct)
	rt.HandleFunc("GET /api/v1/catalog/products/{id}/offers", a.listProductOffers)

	rt.HandleFunc("POST /api/v1/admin/catalog/categories", a.createCategory, authed, a.idem)
	rt.HandleFunc("PUT /api/v1/admin/catalog/categories/{id}", a.renameCategory, authed)
	rt.HandleFunc("PUT /api/v1/admin/catalog/categories/{id}/attributes/{code}", a.defineAttribute, authed)
	rt.HandleFunc("DELETE /api/v1/admin/catalog/categories/{id}/attributes/{code}", a.removeAttribute, authed)
	rt.HandleFunc("GET /api/v1/admin/catalog/moderation-queue", a.moderationQueue, authed)
	rt.HandleFunc("GET /api/v1/admin/catalog/products/{id}", a.getProduct, authed)
	rt.HandleFunc("POST /api/v1/admin/catalog/products/{id}/publish", a.publishProduct, authed, a.idem)
	rt.HandleFunc("POST /api/v1/admin/catalog/products/{id}/reject", a.rejectProduct, authed, a.idem)

	rt.HandleFunc("POST /api/v1/seller/sellers/{seller_id}/products", a.createProduct, authed, a.idem)
	rt.HandleFunc("GET /api/v1/seller/sellers/{seller_id}/products", a.listSellerProducts, authed)
	rt.HandleFunc("GET /api/v1/seller/products/{id}", a.getProduct, authed)
	rt.HandleFunc("PUT /api/v1/seller/products/{id}", a.updateProduct, authed)
	rt.HandleFunc("POST /api/v1/seller/products/{id}/submit", a.submitProduct, authed, a.idem)
	rt.HandleFunc("POST /api/v1/seller/products/{id}/images", a.requestImageUpload, authed, a.idem)
	rt.HandleFunc("POST /api/v1/seller/products/{id}/images/{image_id}/confirm", a.confirmImageUpload, authed, a.idem)
	rt.HandleFunc("DELETE /api/v1/seller/products/{id}/images/{image_id}", a.removeImage, authed)
	rt.HandleFunc("PUT /api/v1/seller/products/{id}/images/order", a.reorderImages, authed)

	rt.HandleFunc("POST /api/v1/seller/sellers/{seller_id}/variant-groups", a.createVariantGroup, authed, a.idem)
	rt.HandleFunc("POST /api/v1/seller/variant-groups/{id}/products", a.addVariantMember, authed, a.idem)
	rt.HandleFunc("DELETE /api/v1/seller/variant-groups/{id}/products/{product_id}", a.removeVariantMember, authed)

	rt.HandleFunc("POST /api/v1/seller/sellers/{seller_id}/offers", a.createOffer, authed, a.idem)
	rt.HandleFunc("GET /api/v1/seller/sellers/{seller_id}/offers", a.listSellerOffers, authed)
	rt.HandleFunc("PUT /api/v1/seller/offers/{id}", a.updateOffer, authed)
	rt.HandleFunc("PUT /api/v1/seller/offers/{id}/status", a.setOfferStatus, authed)
	rt.HandleFunc("POST /api/v1/seller/offers/bulk", a.scheduleImport, authed, a.idem)
	rt.HandleFunc("POST /api/v1/seller/sellers/{seller_id}/offer-imports/upload", a.requestImportUpload, authed, a.idem)
	rt.HandleFunc("GET /api/v1/seller/sellers/{seller_id}/offer-imports", a.listImportJobs, authed)
	rt.HandleFunc("GET /api/v1/seller/offer-imports/{id}", a.getImportJob, authed)
}

func (a *API) listCategories(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.h.ListCategories.Handle(r.Context(), query.ListCategories{})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	out := make([]categoryNodeResponse, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, toCategoryNode(node))
	}
	a.rs.JSON(w, r, http.StatusOK, map[string]any{"data": out})
}

func (a *API) getCategory(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetCategory.Handle(r.Context(), query.GetCategory{CategoryID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toCategory(view))
}

func (a *API) createCategory(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[categoryRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.CreateCategory.Handle(r.Context(), command.CreateCategory{
		Actor: principal(r), ParentID: req.ParentID, Name: req.Name, Slug: req.Slug,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, categoryCreatedResponse{CategoryID: res.CategoryID})
}

func (a *API) renameCategory(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[categoryRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.RenameCategory.Handle(r.Context(), command.RenameCategory{
		Actor: principal(r), CategoryID: r.PathValue("id"), Name: req.Name, Slug: req.Slug,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) defineAttribute(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[attributeRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.DefineAttribute.Handle(r.Context(), command.DefineAttribute{
		Actor: principal(r), CategoryID: r.PathValue("id"), Code: r.PathValue("code"), Name: req.Name, Type: req.Type,
		Required: req.Required, Filterable: req.Filterable, Options: req.Options, Unit: req.Unit,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) removeAttribute(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.RemoveAttribute.Handle(r.Context(), command.RemoveAttribute{
		Actor: principal(r), CategoryID: r.PathValue("id"), Code: r.PathValue("code"),
	})
	a.writeEmpty(w, r, err)
}

func (a *API) getProduct(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetProduct.Handle(r.Context(), query.GetProduct{Actor: principal(r), ProductID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toProduct(view))
}

func (a *API) listProductOffers(w http.ResponseWriter, r *http.Request) {
	offers, err := a.h.ProductOffers.Handle(r.Context(), query.ListProductOffers{ProductID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	out := make([]offerResponse, 0, len(offers))
	for _, offer := range offers {
		out = append(out, toOffer(offer))
	}
	a.rs.JSON(w, r, http.StatusOK, map[string]any{"data": out})
}

func (a *API) createProduct(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[productRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.CreateProduct.Handle(r.Context(), command.CreateProduct{
		Actor: principal(r), SellerID: r.PathValue("seller_id"), CategoryID: req.CategoryID,
		Title: req.Title, Description: req.Description, Brand: req.Brand, Attributes: req.Attributes,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, productCreatedResponse{ProductID: res.ProductID})
}

func (a *API) updateProduct(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[productRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.UpdateProduct.Handle(r.Context(), command.UpdateProduct{
		Actor: principal(r), ProductID: r.PathValue("id"),
		Title: req.Title, Description: req.Description, Brand: req.Brand, Attributes: req.Attributes,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) submitProduct(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.SubmitProduct.Handle(r.Context(), command.SubmitProduct{Actor: principal(r), ProductID: r.PathValue("id")})
	a.writeEmpty(w, r, err)
}

func (a *API) publishProduct(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.PublishProduct.Handle(r.Context(), command.PublishProduct{Actor: principal(r), ProductID: r.PathValue("id")})
	a.writeEmpty(w, r, err)
}

func (a *API) rejectProduct(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[reasonRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.RejectProduct.Handle(r.Context(), command.RejectProduct{
		Actor: principal(r), ProductID: r.PathValue("id"), Reason: req.Reason,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) moderationQueue(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.ModerationQueue.Handle(r.Context(), query.ListModerationQueue{Actor: principal(r), Limit: limit, Cursor: cursor})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toProductSummary))
}

func (a *API) listSellerProducts(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.ListSellerProds.Handle(r.Context(), query.ListSellerProducts{
		Actor: principal(r), SellerID: r.PathValue("seller_id"), Status: r.URL.Query().Get("status"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toProductSummary))
}

func (a *API) requestImageUpload(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[imageRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.RequestImageUpload.Handle(r.Context(), command.RequestImageUpload{
		Actor: principal(r), ProductID: r.PathValue("id"), ContentType: req.ContentType, Size: req.Size,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, imageUploadResponse{ImageID: res.ImageID, Upload: toUploadTarget(res.Upload)})
}

func (a *API) confirmImageUpload(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.ConfirmImageUpload.Handle(r.Context(), command.ConfirmImageUpload{
		Actor: principal(r), ProductID: r.PathValue("id"), ImageID: r.PathValue("image_id"),
	})
	a.writeEmpty(w, r, err)
}

func (a *API) removeImage(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.RemoveImage.Handle(r.Context(), command.RemoveImage{
		Actor: principal(r), ProductID: r.PathValue("id"), ImageID: r.PathValue("image_id"),
	})
	a.writeEmpty(w, r, err)
}

func (a *API) reorderImages(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[imageOrderRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.ReorderImages.Handle(r.Context(), command.ReorderImages{
		Actor: principal(r), ProductID: r.PathValue("id"), ImageIDs: req.ImageIDs,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) createVariantGroup(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[variantGroupRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.CreateVariantGroup.Handle(r.Context(), command.CreateVariantGroup{
		Actor: principal(r), SellerID: r.PathValue("seller_id"), CategoryID: req.CategoryID, Axes: req.Axes,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, variantGroupCreatedResponse{GroupID: res.GroupID})
}

func (a *API) addVariantMember(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[variantMemberRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.AddVariantMember.Handle(r.Context(), command.AddVariantMember{
		Actor: principal(r), GroupID: r.PathValue("id"), ProductID: req.ProductID,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) removeVariantMember(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.RemoveVariantMember.Handle(r.Context(), command.RemoveVariantMember{
		Actor: principal(r), GroupID: r.PathValue("id"), ProductID: r.PathValue("product_id"),
	})
	a.writeEmpty(w, r, err)
}

func (a *API) createOffer(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[offerRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.CreateOffer.Handle(r.Context(), command.CreateOffer{
		Actor: principal(r), SellerID: r.PathValue("seller_id"), ProductID: req.ProductID, SellerSKU: req.SellerSKU,
		Price: req.Price, Currency: req.Currency, Condition: req.Condition, ProcessingDays: req.ProcessingDays,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, offerCreatedResponse{OfferID: res.OfferID})
}

func (a *API) updateOffer(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[offerRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.UpdateOfferTerms.Handle(r.Context(), command.UpdateOfferTerms{
		Actor: principal(r), OfferID: r.PathValue("id"), Price: req.Price, Currency: req.Currency,
		Condition: req.Condition, ProcessingDays: req.ProcessingDays,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) setOfferStatus(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[offerStatusRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.SetOfferStatus.Handle(r.Context(), command.SetOfferStatus{
		Actor: principal(r), OfferID: r.PathValue("id"), Status: req.Status,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) listSellerOffers(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.SellerOffers.Handle(r.Context(), query.ListSellerOffers{
		Actor: principal(r), SellerID: r.PathValue("seller_id"), Status: r.URL.Query().Get("status"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toOffer))
}

func (a *API) requestImportUpload(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[importUploadRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.RequestImportUpload.Handle(r.Context(), command.RequestImportUpload{
		Actor: principal(r), SellerID: r.PathValue("seller_id"), Format: req.Format, Size: req.Size,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, importUploadResponse{ObjectKey: res.ObjectKey, Upload: toUploadTarget(res.Upload)})
}

func (a *API) scheduleImport(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[bulkOffersRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.ScheduleImport.Handle(r.Context(), command.ScheduleImport{
		Actor: principal(r), SellerID: req.SellerID, Format: req.Format, ObjectKey: req.ObjectKey, Rows: toImportRecords(req.Rows),
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusAccepted, importScheduledResponse{JobID: res.JobID})
}

func (a *API) getImportJob(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetImportJob.Handle(r.Context(), query.GetImportJob{Actor: principal(r), JobID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toImportJob(view))
}

func (a *API) listImportJobs(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.ListImportJobs.Handle(r.Context(), query.ListImportJobs{
		Actor: principal(r), SellerID: r.PathValue("seller_id"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toImportJob))
}

func (a *API) writeEmpty(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.NoContent(w)
}

func decode[T any](a *API, w http.ResponseWriter, r *http.Request) (T, bool) {
	var req T
	if err := httpx.DecodeJSON(r, &req); err != nil {
		a.rs.Error(w, r, err)
		return req, false
	}
	return req, true
}

func principal(r *http.Request) auth.Principal {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		return auth.Principal{}
	}
	return p
}
