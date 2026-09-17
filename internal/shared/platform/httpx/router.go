package httpx

import (
	"context"
	"net/http"
	"slices"
	"strings"
)

type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for _, mw := range slices.Backward(mws) {
		h = mw(h)
	}
	return h
}

type Router struct {
	mux *http.ServeMux
	rs  *Responder
}

func NewRouter(rs *Responder) *Router {
	return &Router{mux: http.NewServeMux(), rs: rs}
}

func (rt *Router) Handle(pattern string, h http.Handler, mws ...Middleware) {
	rt.mux.Handle(pattern, Chain(h, mws...))
}

func (rt *Router) HandleFunc(pattern string, h http.HandlerFunc, mws ...Middleware) {
	rt.Handle(pattern, h, mws...)
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, pattern := rt.mux.Handler(r)
	if pattern == "" {
		rt.unmatched(w, r, h)
		return
	}
	recordRoute(r.Context(), pattern)
	rt.mux.ServeHTTP(w, r)
}

func (rt *Router) unmatched(w http.ResponseWriter, r *http.Request, fallback http.Handler) {
	probe := &probeWriter{header: http.Header{}}
	fallback.ServeHTTP(probe, r)
	if probe.status != http.StatusMethodNotAllowed {
		rt.rs.Error(w, r, ErrRouteNotFound)
		return
	}
	if allow := probe.header.Get("Allow"); allow != "" {
		w.Header().Set("Allow", allow)
	}
	p := NewProblem(r, ErrMethodNotAllowed)
	p.Status = http.StatusMethodNotAllowed
	rt.rs.WriteProblem(w, r, p)
}

type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header { return p.header }

func (p *probeWriter) Write(b []byte) (int, error) { return len(b), nil }

func (p *probeWriter) WriteHeader(code int) { p.status = code }

type routeKey struct{}

type routeHolder struct {
	pattern string
}

func withRouteHolder(ctx context.Context) (context.Context, *routeHolder) {
	h := &routeHolder{}
	return context.WithValue(ctx, routeKey{}, h), h
}

func recordRoute(ctx context.Context, pattern string) {
	if h, ok := ctx.Value(routeKey{}).(*routeHolder); ok {
		h.pattern = pattern
	}
}

func (h *routeHolder) route() string {
	if h.pattern == "" {
		return "unmatched"
	}
	if _, path, found := strings.Cut(h.pattern, " "); found {
		return path
	}
	return h.pattern
}
