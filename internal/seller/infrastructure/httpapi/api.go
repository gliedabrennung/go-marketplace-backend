package httpapi

import (
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type Handlers struct {
	OpenApplication         cqrs.Handler[command.OpenApplication, command.OpenApplicationResult]
	UpdateLegalDetails      cqrs.Handler[command.UpdateLegalDetails, struct{}]
	ChangeBankAccount       cqrs.Handler[command.ChangeBankAccount, struct{}]
	AttachDocument          cqrs.Handler[command.AttachDocument, struct{}]
	SubmitApplication       cqrs.Handler[command.SubmitApplication, struct{}]
	AddMember               cqrs.Handler[command.AddMember, struct{}]
	RemoveMember            cqrs.Handler[command.RemoveMember, struct{}]
	ApproveApplication      cqrs.Handler[command.ApproveApplication, struct{}]
	RejectApplication       cqrs.Handler[command.RejectApplication, struct{}]
	VerifyBankAccount       cqrs.Handler[command.VerifyBankAccount, struct{}]
	SuspendSeller           cqrs.Handler[command.SuspendSeller, struct{}]
	ReinstateSeller         cqrs.Handler[command.ReinstateSeller, struct{}]
	TerminateSeller         cqrs.Handler[command.TerminateSeller, struct{}]
	SetCommissionOverride   cqrs.Handler[command.SetCommissionOverride, struct{}]
	ClearCommissionOverride cqrs.Handler[command.ClearCommissionOverride, struct{}]
	SetCategoryCommission   cqrs.Handler[command.SetCategoryCommission, struct{}]
	GetSeller               cqrs.Handler[query.GetSeller, query.SellerView]
	ListMySellers           cqrs.Handler[query.ListMySellers, []query.SellerSummary]
	ListApplications        cqrs.Handler[query.ListApplications, pagination.Page[query.SellerSummary]]
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

	rt.HandleFunc("POST /api/v1/seller/applications", a.openApplication, authed, a.idem)
	rt.HandleFunc("GET /api/v1/seller/sellers", a.listMySellers, authed)
	rt.HandleFunc("GET /api/v1/seller/sellers/{id}", a.getSeller, authed)
	rt.HandleFunc("PUT /api/v1/seller/sellers/{id}/legal-details", a.updateLegalDetails, authed)
	rt.HandleFunc("PUT /api/v1/seller/sellers/{id}/bank-account", a.changeBankAccount, authed)
	rt.HandleFunc("POST /api/v1/seller/sellers/{id}/documents", a.attachDocument, authed, a.idem)
	rt.HandleFunc("POST /api/v1/seller/sellers/{id}/submit", a.submitApplication, authed, a.idem)
	rt.HandleFunc("POST /api/v1/seller/sellers/{id}/members", a.addMember, authed, a.idem)
	rt.HandleFunc("DELETE /api/v1/seller/sellers/{id}/members/{user_id}", a.removeMember, authed)

	rt.HandleFunc("GET /api/v1/admin/seller-applications", a.listApplications, authed)
	rt.HandleFunc("GET /api/v1/admin/sellers/{id}", a.getSeller, authed)
	rt.HandleFunc("POST /api/v1/admin/sellers/{id}/approve", a.approve, authed, a.idem)
	rt.HandleFunc("POST /api/v1/admin/sellers/{id}/reject", a.reject, authed, a.idem)
	rt.HandleFunc("POST /api/v1/admin/sellers/{id}/verify-bank-account", a.verifyBankAccount, authed, a.idem)
	rt.HandleFunc("POST /api/v1/admin/sellers/{id}/suspend", a.suspend, authed, a.idem)
	rt.HandleFunc("POST /api/v1/admin/sellers/{id}/reinstate", a.reinstate, authed, a.idem)
	rt.HandleFunc("POST /api/v1/admin/sellers/{id}/terminate", a.terminate, authed, a.idem)
	rt.HandleFunc("PUT /api/v1/admin/sellers/{id}/commission-overrides/{category_id}", a.setCommissionOverride, authed)
	rt.HandleFunc("DELETE /api/v1/admin/sellers/{id}/commission-overrides/{category_id}", a.clearCommissionOverride, authed)
	rt.HandleFunc("PUT /api/v1/admin/category-commissions/{category_id}", a.setCategoryCommission, authed)
}

func (a *API) openApplication(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[legalDetailsRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.OpenApplication.Handle(r.Context(), command.OpenApplication{
		Actor: principal(r), LegalForm: req.LegalForm, LegalName: req.LegalName, TaxID: req.TaxID, LegalAddress: req.LegalAddress,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, sellerCreatedResponse(res))
}

func (a *API) listMySellers(w http.ResponseWriter, r *http.Request) {
	sellers, err := a.h.ListMySellers.Handle(r.Context(), query.ListMySellers{Actor: principal(r)})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	out := make([]sellerSummaryResponse, 0, len(sellers))
	for _, s := range sellers {
		out = append(out, toSummary(s))
	}
	a.rs.JSON(w, r, http.StatusOK, map[string]any{"data": out})
}

func (a *API) getSeller(w http.ResponseWriter, r *http.Request) {
	view, err := a.h.GetSeller.Handle(r.Context(), query.GetSeller{Actor: principal(r), SellerID: r.PathValue("id")})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toSeller(view))
}

func (a *API) updateLegalDetails(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[legalDetailsRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.UpdateLegalDetails.Handle(r.Context(), command.UpdateLegalDetails{
		Actor: principal(r), SellerID: r.PathValue("id"),
		LegalForm: req.LegalForm, LegalName: req.LegalName, TaxID: req.TaxID, LegalAddress: req.LegalAddress,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) changeBankAccount(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[bankAccountRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.ChangeBankAccount.Handle(r.Context(), command.ChangeBankAccount{
		Actor: principal(r), SellerID: r.PathValue("id"),
		IBAN: req.IBAN, BIC: req.BIC, BankName: req.BankName, Beneficiary: req.Beneficiary,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) attachDocument(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[documentRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.AttachDocument.Handle(r.Context(), command.AttachDocument{
		Actor: principal(r), SellerID: r.PathValue("id"), Kind: req.Kind, ObjectKey: req.ObjectKey,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) submitApplication(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.SubmitApplication.Handle(r.Context(), command.SubmitApplication{Actor: principal(r), SellerID: r.PathValue("id")})
	a.writeEmpty(w, r, err)
}

func (a *API) addMember(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[memberRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.AddMember.Handle(r.Context(), command.AddMember{
		Actor: principal(r), SellerID: r.PathValue("id"), UserID: req.UserID, Role: req.Role,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) removeMember(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.RemoveMember.Handle(r.Context(), command.RemoveMember{
		Actor: principal(r), SellerID: r.PathValue("id"), UserID: r.PathValue("user_id"),
	})
	a.writeEmpty(w, r, err)
}

func (a *API) listApplications(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.ListApplications.Handle(r.Context(), query.ListApplications{
		Actor: principal(r), Status: r.URL.Query().Get("status"), Limit: limit, Cursor: cursor,
	})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toSummary))
}

func (a *API) approve(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.ApproveApplication.Handle(r.Context(), command.ApproveApplication{Actor: principal(r), SellerID: r.PathValue("id")})
	a.writeEmpty(w, r, err)
}

func (a *API) reject(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[reasonRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.RejectApplication.Handle(r.Context(), command.RejectApplication{Actor: principal(r), SellerID: r.PathValue("id"), Reason: req.Reason})
	a.writeEmpty(w, r, err)
}

func (a *API) verifyBankAccount(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.VerifyBankAccount.Handle(r.Context(), command.VerifyBankAccount{Actor: principal(r), SellerID: r.PathValue("id")})
	a.writeEmpty(w, r, err)
}

func (a *API) suspend(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[reasonRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.SuspendSeller.Handle(r.Context(), command.SuspendSeller{Actor: principal(r), SellerID: r.PathValue("id"), Reason: req.Reason})
	a.writeEmpty(w, r, err)
}

func (a *API) reinstate(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.ReinstateSeller.Handle(r.Context(), command.ReinstateSeller{Actor: principal(r), SellerID: r.PathValue("id")})
	a.writeEmpty(w, r, err)
}

func (a *API) terminate(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[reasonRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.TerminateSeller.Handle(r.Context(), command.TerminateSeller{Actor: principal(r), SellerID: r.PathValue("id"), Reason: req.Reason})
	a.writeEmpty(w, r, err)
}

func (a *API) setCommissionOverride(w http.ResponseWriter, r *http.Request) {
	rate, ok := a.decodeRate(w, r)
	if !ok {
		return
	}
	_, err := a.h.SetCommissionOverride.Handle(r.Context(), command.SetCommissionOverride{
		Actor: principal(r), SellerID: r.PathValue("id"), CategoryID: r.PathValue("category_id"), BasisPoints: rate,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) clearCommissionOverride(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.ClearCommissionOverride.Handle(r.Context(), command.ClearCommissionOverride{
		Actor: principal(r), SellerID: r.PathValue("id"), CategoryID: r.PathValue("category_id"),
	})
	a.writeEmpty(w, r, err)
}

func (a *API) setCategoryCommission(w http.ResponseWriter, r *http.Request) {
	rate, ok := a.decodeRate(w, r)
	if !ok {
		return
	}
	_, err := a.h.SetCategoryCommission.Handle(r.Context(), command.SetCategoryCommission{
		Actor: principal(r), CategoryID: r.PathValue("category_id"), BasisPoints: rate,
	})
	a.writeEmpty(w, r, err)
}

func (a *API) decodeRate(w http.ResponseWriter, r *http.Request) (int, bool) {
	req, ok := decode[rateRequest](a, w, r)
	if !ok {
		return 0, false
	}
	if req.BasisPoints == nil {
		a.rs.Error(w, r, ErrBasisPointsRequired)
		return 0, false
	}
	return *req.BasisPoints, true
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
