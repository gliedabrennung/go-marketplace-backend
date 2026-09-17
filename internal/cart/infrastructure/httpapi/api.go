package httpapi

import (
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const DeviceHeader = "X-Device-ID"

type Handlers struct {
	Add         cqrs.Handler[command.AddItem, struct{}]
	Update      cqrs.Handler[command.UpdateItem, struct{}]
	Remove      cqrs.Handler[command.RemoveItem, struct{}]
	ApplyPromo  cqrs.Handler[command.ApplyPromoCode, struct{}]
	RemovePromo cqrs.Handler[command.RemovePromoCode, struct{}]
	Merge       cqrs.Handler[command.MergeCarts, struct{}]
	Get         cqrs.Handler[query.GetCart, application.View]
}

type API struct {
	h  Handlers
	rs *httpx.Responder
}

func NewAPI(h Handlers, rs *httpx.Responder) *API {
	return &API{h: h, rs: rs}
}

func (a *API) Register(rt *httpx.Router) {
	rt.HandleFunc("GET /api/v1/cart", a.get)
	rt.HandleFunc("POST /api/v1/cart/items", a.add)
	rt.HandleFunc("PATCH /api/v1/cart/items/{sku}", a.update)
	rt.HandleFunc("DELETE /api/v1/cart/items/{sku}", a.remove)
	rt.HandleFunc("POST /api/v1/cart/promo-code", a.applyPromo)
	rt.HandleFunc("DELETE /api/v1/cart/promo-code", a.removePromo)
	rt.HandleFunc("POST /api/v1/cart/checkout-preview", a.preview)
	rt.HandleFunc("POST /api/v1/cart/merge", a.merge, httpx.RequireAuthenticated(a.rs))
}

func owner(r *http.Request) application.OwnerRef {
	if p, ok := auth.FromContext(r.Context()); ok && p.UserID != "" {
		return application.OwnerRef{UserID: p.UserID}
	}
	return application.OwnerRef{DeviceID: r.Header.Get(DeviceHeader)}
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, owner(r), "", nil)
}

func (a *API) add(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[itemRequest](a, w, r)
	if !ok {
		return
	}
	ref := owner(r)
	_, err := a.h.Add.Handle(r.Context(), command.AddItem{Owner: ref, SKU: req.SKU, Quantity: req.Quantity})
	a.render(w, r, ref, "", err)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[quantityRequest](a, w, r)
	if !ok {
		return
	}
	ref := owner(r)
	_, err := a.h.Update.Handle(r.Context(), command.UpdateItem{Owner: ref, SKU: r.PathValue("sku"), Quantity: req.Quantity})
	a.render(w, r, ref, "", err)
}

func (a *API) remove(w http.ResponseWriter, r *http.Request) {
	ref := owner(r)
	_, err := a.h.Remove.Handle(r.Context(), command.RemoveItem{Owner: ref, SKU: r.PathValue("sku")})
	a.render(w, r, ref, "", err)
}

func (a *API) applyPromo(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[promoRequest](a, w, r)
	if !ok {
		return
	}
	ref := owner(r)
	_, err := a.h.ApplyPromo.Handle(r.Context(), command.ApplyPromoCode{Owner: ref, Code: req.Code})
	a.render(w, r, ref, "", err)
}

func (a *API) removePromo(w http.ResponseWriter, r *http.Request) {
	ref := owner(r)
	_, err := a.h.RemovePromo.Handle(r.Context(), command.RemovePromoCode{Owner: ref})
	a.render(w, r, ref, "", err)
}

func (a *API) preview(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[previewRequest](a, w, r)
	if !ok {
		return
	}
	a.render(w, r, owner(r), req.DeliveryMethod, nil)
}

func (a *API) merge(w http.ResponseWriter, r *http.Request) {
	ref := owner(r)
	_, err := a.h.Merge.Handle(r.Context(), command.MergeCarts{UserID: ref.UserID, DeviceID: r.Header.Get(DeviceHeader)})
	a.render(w, r, ref, "", err)
}

func (a *API) render(w http.ResponseWriter, r *http.Request, ref application.OwnerRef, method string, err error) {
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	view, err := a.h.Get.Handle(r.Context(), query.GetCart{Owner: ref, DeliveryMethod: method})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toCart(view))
}

func decode[T any](a *API, w http.ResponseWriter, r *http.Request) (T, bool) {
	var req T
	if err := httpx.DecodeJSON(r, &req); err != nil {
		a.rs.Error(w, r, err)
		return req, false
	}
	return req, true
}
