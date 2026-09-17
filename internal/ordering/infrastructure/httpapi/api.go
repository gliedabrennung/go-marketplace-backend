package httpapi

import (
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type Handlers struct {
	Place        cqrs.Handler[command.PlaceOrder, command.PlaceOrderResult]
	Cancel       cqrs.Handler[command.CancelOrder, struct{}]
	RetryPayment cqrs.Handler[command.RetryPayment, command.RetryPaymentResult]
	ResumeSaga   cqrs.Handler[command.ResumeSaga, struct{}]
	Ship         cqrs.Handler[command.MarkOrderShipped, struct{}]
	Deliver      cqrs.Handler[command.MarkOrderDelivered, struct{}]
	Get          cqrs.Handler[query.GetOrder, query.OrderView]
	List         cqrs.Handler[query.ListOrders, pagination.Page[query.OrderSummaryView]]
	SellerOrders cqrs.Handler[query.ListSellerOrders, pagination.Page[query.SellerOrderView]]
	Sagas        cqrs.Handler[query.ListSagas, pagination.Page[query.SagaView]]
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

	rt.HandleFunc("POST /api/v1/orders", a.place, authed, a.idem)
	rt.HandleFunc("GET /api/v1/orders", a.list, authed)
	rt.HandleFunc("GET /api/v1/orders/{id}", a.get, authed)
	rt.HandleFunc("POST /api/v1/orders/{id}/cancel", a.cancel, authed, a.idem)
	rt.HandleFunc("POST /api/v1/payments/{id}/retry", a.retry, authed, a.idem)
	rt.HandleFunc("GET /api/v1/seller/orders", a.sellerOrders, authed)
	rt.HandleFunc("POST /api/v1/seller/orders/{id}/ship", a.ship, authed)
	rt.HandleFunc("POST /api/v1/seller/orders/{id}/deliver", a.deliver, authed)
	rt.HandleFunc("GET /api/v1/admin/checkout-sagas", a.sagas, authed)
	rt.HandleFunc("POST /api/v1/admin/checkout-sagas/{id}/resume", a.resume, authed)
}

func (a *API) place(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[placeRequest](a, w, r)
	if !ok {
		return
	}
	result, err := a.h.Place.Handle(r.Context(), command.PlaceOrder{
		Actor: principal(r), DeliveryMethod: req.DeliveryMethod, MethodID: req.Payment.MethodID,
		SaveMethod: req.Payment.SaveMethod, ExpectedTotal: req.ExpectedTotal,
		Address: domain.Address{
			Recipient: req.Address.Recipient, Phone: req.Address.Phone, Country: req.Address.Country, City: req.Address.City,
			Line: req.Address.Line, PostalCode: req.Address.PostalCode,
		},
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, placedResponse(result))
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.List.Handle(r.Context(), query.ListOrders{
		Actor: principal(r), Status: r.URL.Query().Get("status"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toSummary))
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.Get.Handle(r.Context(), query.GetOrder{Actor: principal(r), OrderID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toOrder(view))
}

func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[cancelRequest](a, w, r)
	if !ok {
		return
	}
	actor := principal(r)
	if _, err := a.h.Cancel.Handle(r.Context(), command.CancelOrder{Actor: actor, OrderID: r.PathValue("id"), Reason: req.Reason}); err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.get(w, r)
}

func (a *API) retry(w http.ResponseWriter, r *http.Request) {
	result, err := a.h.RetryPayment.Handle(r.Context(), command.RetryPayment{Actor: principal(r), PaymentID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, retryResponse(result))
}

func (a *API) ship(w http.ResponseWriter, r *http.Request) {
	if _, err := a.h.Ship.Handle(r.Context(), command.MarkOrderShipped{Actor: principal(r), OrderID: r.PathValue("id")}); err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.NoContent(w)
}

func (a *API) deliver(w http.ResponseWriter, r *http.Request) {
	if _, err := a.h.Deliver.Handle(r.Context(), command.MarkOrderDelivered{Actor: principal(r), OrderID: r.PathValue("id")}); err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.NoContent(w)
}

func (a *API) sellerOrders(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	params := r.URL.Query()
	page, err := a.h.SellerOrders.Handle(r.Context(), query.ListSellerOrders{
		Actor: principal(r), SellerID: params.Get("seller_id"), Status: params.Get("status"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toSellerOrder))
}

func (a *API) sagas(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.Sagas.Handle(r.Context(), query.ListSagas{
		Actor: principal(r), Status: r.URL.Query().Get("status"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toSaga))
}

func (a *API) resume(w http.ResponseWriter, r *http.Request) {
	if _, err := a.h.ResumeSaga.Handle(r.Context(), command.ResumeSaga{Actor: principal(r), OrderID: r.PathValue("id")}); err != nil {
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
