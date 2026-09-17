package httpapi

import (
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

type Handlers struct {
	Report cqrs.Handler[query.GetSellerSettlements, query.Report]
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
	rt.HandleFunc("GET /api/v1/seller/settlements", a.report, authed)
}

func (a *API) report(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	report, err := a.h.Report.Handle(r.Context(), query.GetSellerSettlements{
		Actor: principal(r), SellerID: params.Get("seller_id"), Period: params.Get("period"),
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toReport(report))
}

func principal(r *http.Request) auth.Principal {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		return auth.Principal{}
	}
	return p
}
