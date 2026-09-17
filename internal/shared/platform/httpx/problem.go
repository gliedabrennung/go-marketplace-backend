package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const problemTypeBase = "https://api.marketplace.kz/errors/"

type Problem struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Code     string       `json:"code"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	TraceID  string       `json:"trace_id,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type retryAfter interface {
	RetryAfter() time.Duration
}

type messaged interface {
	Message() string
}

type detailed interface {
	Detail() string
}

type fielded interface {
	Fields() []kernel.FieldViolation
}

func StatusFor(kind kernel.ErrorKind) int {
	switch kind {
	case kernel.KindValidation:
		return http.StatusBadRequest
	case kernel.KindUnauthenticated:
		return http.StatusUnauthorized
	case kernel.KindForbidden:
		return http.StatusForbidden
	case kernel.KindNotFound:
		return http.StatusNotFound
	case kernel.KindConflict:
		return http.StatusConflict
	case kernel.KindBusinessRule:
		return http.StatusUnprocessableEntity
	case kernel.KindRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func NewProblem(r *http.Request, err error) Problem {
	kind := kernel.KindOf(err)
	p := Problem{
		Status:   StatusFor(kind),
		Instance: r.URL.Path,
		TraceID:  traceID(r),
	}
	if kind == kernel.KindInternal {
		p.Code = "INTERNAL"
		p.Title = "Internal server error"
		p.Type = problemTypeBase + "internal"
		return p
	}

	p.Code = kernel.CodeOf(err)
	p.Type = problemTypeBase + slug(p.Code)
	p.Title = http.StatusText(p.Status)

	var m messaged
	if errors.As(err, &m) {
		p.Title = m.Message()
	}
	var d detailed
	if errors.As(err, &d) {
		p.Detail = d.Detail()
	}
	var f fielded
	if errors.As(err, &f) {
		for _, v := range f.Fields() {
			p.Errors = append(p.Errors, FieldError{Field: v.Field, Code: v.Code, Message: v.Message})
		}
	}
	return p
}

func slug(code string) string {
	return strings.ReplaceAll(strings.ToLower(code), "_", "-")
}

func traceID(r *http.Request) string {
	sc := trace.SpanContextFromContext(r.Context())
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

type Responder struct {
	log *slog.Logger
}

func NewResponder(log *slog.Logger) *Responder {
	return &Responder{log: log}
}

func (rs *Responder) JSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		rs.log.WarnContext(r.Context(), "write response failed", "err", err)
	}
}

func (rs *Responder) NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func (rs *Responder) Error(w http.ResponseWriter, r *http.Request, err error) {
	p := NewProblem(r, err)
	if p.Status >= http.StatusInternalServerError {
		rs.log.ErrorContext(r.Context(), "request failed", "err", err, "path", r.URL.Path)
	}
	var ra retryAfter
	if errors.As(err, &ra) {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(ra.RetryAfter().Seconds()))))
	}
	rs.WriteProblem(w, r, p)
}

func (rs *Responder) WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	if encErr := json.NewEncoder(w).Encode(p); encErr != nil {
		rs.log.WarnContext(r.Context(), "write problem failed", "err", encErr)
	}
}
