package httpapi

import (
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/ratelimit"
)

type Handlers struct {
	RegisterWithEmail cqrs.Handler[command.RegisterWithEmail, command.RegisterWithEmailResult]
	ConfirmEmail      cqrs.Handler[command.ConfirmEmail, struct{}]
	SignInWithEmail   cqrs.Handler[command.SignInWithEmail, application.AuthTokens]
	RefreshSession    cqrs.Handler[command.RefreshSession, application.AuthTokens]
	RevokeSession     cqrs.Handler[command.RevokeSession, struct{}]
	BlockUser         cqrs.Handler[command.BlockUser, struct{}]
	UnblockUser       cqrs.Handler[command.UnblockUser, struct{}]
	GrantRole         cqrs.Handler[command.GrantRole, struct{}]
	RevokeRole        cqrs.Handler[command.RevokeRole, struct{}]
	ListSessions      cqrs.Handler[query.ListSessions, pagination.Page[query.SessionView]]
	GetProfile        cqrs.Handler[query.GetProfile, query.Profile]
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

	rt.HandleFunc("POST /api/v1/auth/email/register", a.registerWithEmail)
	rt.HandleFunc("POST /api/v1/auth/email/confirm", a.confirmEmail)
	rt.HandleFunc("POST /api/v1/auth/email/sign-in", a.signInWithEmail)
	rt.HandleFunc("POST /api/v1/auth/token/refresh", a.refreshSession)
	rt.HandleFunc("POST /api/v1/auth/sign-out", a.signOut, authed)

	rt.HandleFunc("GET /api/v1/me", a.getProfile, authed)
	rt.HandleFunc("GET /api/v1/me/sessions", a.listSessions, authed)
	rt.HandleFunc("DELETE /api/v1/me/sessions/{id}", a.revokeSession, authed)

	rt.HandleFunc("POST /api/v1/admin/users/{id}/block", a.blockUser, authed, a.idem)
	rt.HandleFunc("POST /api/v1/admin/users/{id}/unblock", a.unblockUser, authed, a.idem)
	rt.HandleFunc("PUT /api/v1/admin/users/{id}/roles/{role}", a.grantRole, authed)
	rt.HandleFunc("DELETE /api/v1/admin/users/{id}/roles/{role}", a.revokeRole, authed)
}

func (a *API) registerWithEmail(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[emailRegisterRequest](a, w, r)
	if !ok {
		return
	}
	res, err := a.h.RegisterWithEmail.Handle(r.Context(), command.RegisterWithEmail{Email: req.Email, Password: req.Password})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusCreated, emailRegisterResponse(res))
}

func (a *API) confirmEmail(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[emailConfirmRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.ConfirmEmail.Handle(r.Context(), command.ConfirmEmail{Token: req.Token})
	a.writeEmpty(w, r, err)
}

func (a *API) signInWithEmail(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[emailSignInRequest](a, w, r)
	if !ok {
		return
	}
	tokens, err := a.h.SignInWithEmail.Handle(r.Context(), command.SignInWithEmail{
		Email:      req.Email,
		Password:   req.Password,
		DeviceName: req.DeviceName,
		UserAgent:  r.UserAgent(),
		IP:         ratelimit.ClientIP(r),
	})
	a.writeTokens(w, r, tokens, err)
}

func (a *API) refreshSession(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[refreshRequest](a, w, r)
	if !ok {
		return
	}
	tokens, err := a.h.RefreshSession.Handle(r.Context(), command.RefreshSession{RefreshToken: req.RefreshToken})
	a.writeTokens(w, r, tokens, err)
}

func (a *API) signOut(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	_, err := a.h.RevokeSession.Handle(r.Context(), command.RevokeSession{Actor: p, SessionID: p.SessionID})
	a.writeEmpty(w, r, err)
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := a.h.GetProfile.Handle(r.Context(), query.GetProfile{Actor: principal(r)})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toProfile(profile))
}

func (a *API) listSessions(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := httpx.PageParams(r)
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	page, err := a.h.ListSessions.Handle(r.Context(), query.ListSessions{Actor: principal(r), Limit: limit, Cursor: cursor})
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, httpx.NewPageResponse(page, toSession))
}

func (a *API) revokeSession(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.RevokeSession.Handle(r.Context(), command.RevokeSession{Actor: principal(r), SessionID: r.PathValue("id")})
	a.writeEmpty(w, r, err)
}

func (a *API) blockUser(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[blockRequest](a, w, r)
	if !ok {
		return
	}
	_, err := a.h.BlockUser.Handle(r.Context(), command.BlockUser{Actor: principal(r), UserID: r.PathValue("id"), Reason: req.Reason})
	a.writeEmpty(w, r, err)
}

func (a *API) unblockUser(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.UnblockUser.Handle(r.Context(), command.UnblockUser{Actor: principal(r), UserID: r.PathValue("id")})
	a.writeEmpty(w, r, err)
}

func (a *API) grantRole(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.GrantRole.Handle(r.Context(), command.GrantRole{Actor: principal(r), UserID: r.PathValue("id"), Role: r.PathValue("role")})
	a.writeEmpty(w, r, err)
}

func (a *API) revokeRole(w http.ResponseWriter, r *http.Request) {
	_, err := a.h.RevokeRole.Handle(r.Context(), command.RevokeRole{Actor: principal(r), UserID: r.PathValue("id"), Role: r.PathValue("role")})
	a.writeEmpty(w, r, err)
}

func (a *API) writeTokens(w http.ResponseWriter, r *http.Request, tokens application.AuthTokens, err error) {
	if err != nil {
		a.rs.Error(w, r, err)
		return
	}
	a.rs.JSON(w, r, http.StatusOK, toTokens(tokens))
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
