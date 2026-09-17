package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

var ErrInvalidNumber = kernel.Validation("SEARCH_INVALID_NUMBER", "numeric query parameter is invalid")

type Handlers struct {
	SearchProducts cqrs.Handler[query.SearchProducts, query.SearchResult]
	CategoryFacets cqrs.Handler[query.CategoryFacets, query.FacetSet]
	RequestReindex cqrs.Handler[command.RequestReindex, command.RequestReindexResult]
	GetReindexJob  cqrs.Handler[command.GetReindexJob, application.ReindexJob]
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

	rt.HandleFunc("GET /api/v1/search/products", a.search)
	rt.HandleFunc("GET /api/v1/catalog/categories/{id}/facets", a.facets)
	rt.HandleFunc("POST /api/v1/admin/search/reindex", a.reindex, authed, a.idem)
	rt.HandleFunc("GET /api/v1/admin/search/reindex/{id}", a.reindexJob, authed)
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	params := r.URL.Query()
	priceMin, priceMax, err := priceRange(params.Get("price_min"), params.Get("price_max"))
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	result, err := a.h.SearchProducts.Handle(r.Context(), query.SearchProducts{
		Query: params.Get("q"), CategoryID: params.Get("category"), Filters: params.Get("filters"),
		PriceMin: priceMin, PriceMax: priceMax, SellerID: params.Get("seller"), Condition: params.Get("condition"),
		InStock: params.Get("in_stock") == "true", Sort: params.Get("sort"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toSearchResponse(result))
}

func (a *API) facets(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	priceMin, priceMax, err := priceRange(params.Get("price_min"), params.Get("price_max"))
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	set, err := a.h.CategoryFacets.Handle(r.Context(), query.CategoryFacets{
		CategoryID: r.PathValue("id"), Query: params.Get("q"), Filters: params.Get("filters"),
		PriceMin: priceMin, PriceMax: priceMax, SellerID: params.Get("seller"), Condition: params.Get("condition"),
		InStock: params.Get("in_stock") == "true",
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toFacets(set))
}

func (a *API) reindex(w http.ResponseWriter, r *http.Request) {
	res, err := a.h.RequestReindex.Handle(r.Context(), command.RequestReindex{Actor: principal(r)})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusAccepted, reindexScheduledResponse{JobID: res.JobID})
}

func (a *API) reindexJob(w http.ResponseWriter, r *http.Request) {
	job, err := a.h.GetReindexJob.Handle(r.Context(), command.GetReindexJob{Actor: principal(r), JobID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toReindexJob(job))
}

func priceRange(min, max string) (int64, int64, error) {
	from, err := number(min)
	if err != nil {
		return 0, 0, err
	}
	to, err := number(max)
	if err != nil {
		return 0, 0, err
	}
	return from, to, nil
}

func number(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, ErrInvalidNumber.WithDetail("%q", raw)
	}
	return value, nil
}

func principal(r *http.Request) auth.Principal {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		return auth.Principal{}
	}
	return p
}
