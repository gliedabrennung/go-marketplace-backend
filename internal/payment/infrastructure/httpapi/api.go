package httpapi

import (
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/ratelimit"
)

const maxWebhookBytes = 64 << 10

var (
	errForbiddenSource = kernel.Forbidden("PAYMENT_WEBHOOK_SOURCE_FORBIDDEN", "webhook source address is not allowed")
	errInvalidDay      = kernel.Validation("PAYMENT_INVALID_DAY", "day must be formatted as YYYY-MM-DD",
		kernel.FieldViolation{Field: "day", Code: "INVALID_FORMAT", Message: "expected YYYY-MM-DD"})
)

type Handlers struct {
	Webhook           cqrs.Handler[command.HandleWebhook, command.HandleWebhookResult]
	RemoveMethod      cqrs.Handler[command.RemoveSavedMethod, struct{}]
	GetPayment        cqrs.Handler[query.GetPayment, query.PaymentView]
	ListMethods       cqrs.Handler[query.ListSavedMethods, []query.MethodView]
	GetReconciliation cqrs.Handler[query.GetReconciliation, query.ReconciliationView]
}

type API struct {
	h         Handlers
	rs        *httpx.Responder
	allowlist []netip.Prefix
}

func NewAPI(h Handlers, rs *httpx.Responder, allowlist []netip.Prefix) *API {
	return &API{h: h, rs: rs, allowlist: allowlist}
}

func (a *API) Register(rt *httpx.Router) {
	authed := httpx.RequireAuthenticated(a.rs)

	rt.HandleFunc("POST /webhooks/payments/{provider}", a.webhook, a.sources)
	rt.HandleFunc("GET /api/v1/payments/{id}", a.getPayment, authed)
	rt.HandleFunc("GET /api/v1/me/payment-methods", a.listMethods, authed)
	rt.HandleFunc("DELETE /api/v1/me/payment-methods/{id}", a.removeMethod, authed)
	rt.HandleFunc("GET /api/v1/admin/payments/reconciliations/{day}", a.getReconciliation, authed)
}

func (a *API) sources(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Allowed(a.allowlist, ratelimit.ClientIP(r)) {
			a.rs.Error(w, r, errForbiddenSource)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Allowed(allowlist []netip.Prefix, ip string) bool {
	if len(allowlist) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range allowlist {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func ParseAllowlist(entries []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, "/") {
			addr, err := netip.ParseAddr(entry)
			if err != nil {
				return nil, err
			}
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
}

func (a *API) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBytes+1))
	if err != nil || len(body) > maxWebhookBytes {
		a.rs.Error(w, r, httpx.ErrBodyTooLarge)
		return
	}
	headers := make(map[string]string, len(r.Header))
	for name := range r.Header {
		headers[strings.ToLower(name)] = r.Header.Get(name)
	}
	result, err := a.h.Webhook.Handle(r.Context(), command.HandleWebhook{
		Provider: r.PathValue("provider"), Headers: headers, Body: body,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, webhookResponse{Received: true, Duplicate: result.Duplicate})
}

func (a *API) getPayment(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetPayment.Handle(r.Context(), query.GetPayment{Actor: principal(r), PaymentID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toPayment(view))
}

func (a *API) listMethods(w http.ResponseWriter, r *http.Request) {
	views, err := a.h.ListMethods.Handle(r.Context(), query.ListSavedMethods{Actor: principal(r)})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	out := methodsResponse{Items: make([]methodResponse, 0, len(views))}
	for _, view := range views {
		out.Items = append(out.Items, methodResponse{ID: view.ID, Provider: view.Provider, Label: view.Label, CreatedAt: view.CreatedAt})
	}
	a.rs.JSON(w, r, http.StatusOK, out)
}

func (a *API) removeMethod(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.RemoveMethod.Handle(r.Context(), command.RemoveSavedMethod{Actor: principal(r), MethodID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.NoContent(w)
}

func (a *API) getReconciliation(w http.ResponseWriter, r *http.Request) {
	day, err := time.Parse(time.DateOnly, r.PathValue("day"))
	if err != nil {
		a.rs.Error(w, r, errInvalidDay)
		return
	}
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "sandbox"
	}
	view, err := a.h.GetReconciliation.Handle(r.Context(), query.GetReconciliation{Actor: principal(r), Provider: provider, Day: day})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toReconciliation(view))
}

func principal(r *http.Request) auth.Principal {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		return auth.Principal{}
	}
	return p
}
