package httpapi

import (
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type Handlers struct {
	SetStock       cqrs.Handler[command.SetStock, command.SetStockResult]
	GetStock       cqrs.Handler[query.GetStock, query.StockView]
	ListSellerStk  cqrs.Handler[query.ListSellerStock, pagination.Page[query.StockView]]
	ListMovements  cqrs.Handler[query.ListMovements, []query.MovementView]
	GetReservation cqrs.Handler[query.GetReservation, query.ReservationView]
}

type API struct {
	h  Handlers
	rs *httpx.Responder
}

func NewAPI(h Handlers, rs *httpx.Responder) *API {
	return &API{h: h, rs: rs}
}

func (a *API) Register(rt *httpx.Router) {
	authed := httpx.RequireAuthenticated(a.rs)

	rt.HandleFunc("PATCH /api/v1/seller/offers/{id}/stock", a.setStock, authed)
	rt.HandleFunc("GET /api/v1/seller/offers/{id}/stock", a.getStock, authed)
	rt.HandleFunc("GET /api/v1/seller/offers/{id}/stock/movements", a.listMovements, authed)
	rt.HandleFunc("GET /api/v1/seller/sellers/{seller_id}/stock", a.listSellerStock, authed)
	rt.HandleFunc("GET /api/v1/seller/reservations/{id}", a.getReservation, authed)
}

func (a *API) setStock(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[stockRequest](a, w, r)
	if !ok {
		return
	}
	if req.Quantity == nil {
		a.rs.Error(w, r, ErrQuantityRequired)
		return
	}
	result, err := a.h.SetStock.Handle(r.Context(), command.SetStock{
		Actor: principal(r), SKU: r.PathValue("id"), Quantity: *req.Quantity, Reference: req.Reference,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, stockResponse{
		SKU: result.SKU, SellerID: result.SellerID, Available: result.Available,
		Reserved: result.Reserved, UpdatedAt: result.UpdatedAt,
	})
}

func (a *API) getStock(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetStock.Handle(r.Context(), query.GetStock{Actor: principal(r), SKU: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toStock(view))
}

func (a *API) listMovements(w http.ResponseWriter, r *http.Request) {
	limit, _, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	movements, err := a.h.ListMovements.Handle(r.Context(), query.ListMovements{
		Actor: principal(r), SKU: r.PathValue("id"), Limit: limit,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	out := make([]movementResponse, 0, len(movements))
	for _, movement := range movements {
		out = append(out, toMovement(movement))
	}
	a.rs.JSON(w, r, http.StatusOK, map[string]any{"data": out})
}

func (a *API) listSellerStock(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.ListSellerStk.Handle(r.Context(), query.ListSellerStock{
		Actor: principal(r), SellerID: r.PathValue("seller_id"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toStock))
}

func (a *API) getReservation(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetReservation.Handle(r.Context(), query.GetReservation{ReservationID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toReservation(view))
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
