package httpapi

import (
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type Handlers struct {
	CreatePromotion    cqrs.Handler[command.CreatePromotion, command.CreatePromotionResult]
	UpdatePromotion    cqrs.Handler[command.UpdatePromotion, struct{}]
	SetPromotionStatus cqrs.Handler[command.SetPromotionStatus, struct{}]
	CreatePromoCode    cqrs.Handler[command.CreatePromoCode, command.CreatePromoCodeResult]
	SetPromoCodeStatus cqrs.Handler[command.SetPromoCodeStatus, struct{}]
	SetCompareAt       cqrs.Handler[command.SetCompareAtPrice, struct{}]
	Quote              cqrs.Handler[query.Quote, query.QuoteView]
	GetPromotion       cqrs.Handler[query.GetPromotion, query.PromotionView]
	ListPromotions     cqrs.Handler[query.ListPromotions, pagination.Page[query.PromotionView]]
	GetPromoCode       cqrs.Handler[query.GetPromoCode, query.PromoCodeView]
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

	rt.HandleFunc("POST /api/v1/pricing/quote", a.quote)
	rt.HandleFunc("PUT /api/v1/seller/offers/{id}/compare-at-price", a.setCompareAt, authed)

	rt.HandleFunc("POST /api/v1/admin/pricing/promotions", a.createPromotion, authed, a.idem)
	rt.HandleFunc("GET /api/v1/admin/pricing/promotions", a.listPromotions, authed)
	rt.HandleFunc("GET /api/v1/admin/pricing/promotions/{id}", a.getPromotion, authed)
	rt.HandleFunc("PUT /api/v1/admin/pricing/promotions/{id}", a.updatePromotion, authed)
	rt.HandleFunc("PUT /api/v1/admin/pricing/promotions/{id}/status", a.setPromotionStatus, authed)
	rt.HandleFunc("POST /api/v1/admin/pricing/promo-codes", a.createPromoCode, authed, a.idem)
	rt.HandleFunc("GET /api/v1/admin/pricing/promo-codes/{code}", a.getPromoCode, authed)
	rt.HandleFunc("PUT /api/v1/admin/pricing/promo-codes/{code}/status", a.setPromoCodeStatus, authed)
}

func (a *API) quote(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[quoteRequest](a, w, r)
	if !ok {
		return
	}
	q := query.Quote{PromoCode: req.PromoCode, CustomerID: principal(r).UserID, Lines: make([]query.QuoteLine, 0, len(req.Lines))}
	for _, line := range req.Lines {
		q.Lines = append(q.Lines, query.QuoteLine{SKU: line.SKU, Quantity: line.Quantity})
	}
	view, err := a.h.Quote.Handle(r.Context(), q)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toQuote(view))
}

func (a *API) setCompareAt(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[compareAtRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.SetCompareAt.Handle(r.Context(), command.SetCompareAtPrice{
		Actor: principal(r), SKU: r.PathValue("id"), CompareAt: req.CompareAt,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) createPromotion(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[promotionRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.CreatePromotion.Handle(r.Context(), command.CreatePromotion{Actor: principal(r), Promotion: toPromotionInput(req)})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, promotionCreatedResponse{PromotionID: res.PromotionID})
}

func (a *API) updatePromotion(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[promotionRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.UpdatePromotion.Handle(r.Context(), command.UpdatePromotion{
		Actor: principal(r), PromotionID: r.PathValue("id"), Promotion: toPromotionInput(req),
	})
	a.writeEmpty(w, r, err)
}

func (a *API) setPromotionStatus(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[statusRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.SetPromotionStatus.Handle(r.Context(), command.SetPromotionStatus{
		Actor: principal(r), PromotionID: r.PathValue("id"), Status: req.Status,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) getPromotion(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetPromotion.Handle(r.Context(), query.GetPromotion{Actor: principal(r), PromotionID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toPromotion(view))
}

func (a *API) listPromotions(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.ListPromotions.Handle(r.Context(), query.ListPromotions{
		Actor: principal(r), Status: r.URL.Query().Get("status"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toPromotion))
}

func (a *API) createPromoCode(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[promoCodeRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.CreatePromoCode.Handle(r.Context(), command.CreatePromoCode{
		Actor: principal(r), Code: req.Code, Discount: toDiscountInput(req.Discount), MinCartAmount: req.MinCartAmount,
		TotalLimit: req.TotalLimit, PerCustomerLimit: req.PerCustomerLimit, StartsAt: value(req.StartsAt), EndsAt: value(req.EndsAt),
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, promoCodeCreatedResponse{Code: res.Code})
}

func (a *API) getPromoCode(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetPromoCode.Handle(r.Context(), query.GetPromoCode{Actor: principal(r), Code: r.PathValue("code")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toPromoCode(view))
}

func (a *API) setPromoCodeStatus(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[statusRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.SetPromoCodeStatus.Handle(r.Context(), command.SetPromoCodeStatus{
		Actor: principal(r), Code: r.PathValue("code"), Status: req.Status,
	})
	a.writeEmpty(w, r, err)
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
