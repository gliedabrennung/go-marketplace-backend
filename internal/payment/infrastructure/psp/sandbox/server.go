package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

type ServerConfig struct {
	APIKey     string
	Secret     string
	PublicURL  string
	WebhookURL string
	HTTP       *http.Client
	Now        func() time.Time
	Log        *slog.Logger
	Attempts   int
}

type refund struct {
	id        string
	reference string
	amount    int64
}

type intent struct {
	id          string
	reference   string
	status      string
	amount      int64
	captured    int64
	refunded    int64
	currency    string
	returnURL   string
	description string
	saveMethod  bool
	methodToken string
	createdAt   time.Time
	refunds     []refund
}

type stored struct {
	status int
	body   []byte
}

type Server struct {
	cfg       ServerConfig
	mu        sync.Mutex
	intents   map[string]*intent
	responses map[string]stored
	mux       *http.ServeMux
}

type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.message }

func NewServer(cfg ServerConfig) *Server {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 5 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Attempts <= 0 {
		cfg.Attempts = 3
	}
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	s := &Server{cfg: cfg, intents: map[string]*intent{}, responses: map[string]stored{}, mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /v1/intents", s.api(s.createIntent))
	s.mux.HandleFunc("GET /v1/intents/{id}", s.api(s.getIntent))
	s.mux.HandleFunc("POST /v1/intents/{id}/capture", s.api(s.capture))
	s.mux.HandleFunc("POST /v1/intents/{id}/cancel", s.api(s.cancel))
	s.mux.HandleFunc("POST /v1/intents/{id}/refunds", s.api(s.refund))
	s.mux.HandleFunc("GET /v1/transactions", s.api(s.transactions))
	s.mux.HandleFunc("GET /pay/{id}", s.page)
	s.mux.HandleFunc("POST /pay/{id}/approve", s.confirm(true))
	s.mux.HandleFunc("POST /pay/{id}/decline", s.confirm(false))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) api(handle func(r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(AccessHeader)
		if s.cfg.APIKey == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.APIKey)) != 1 {
			writeJSON(w, http.StatusUnauthorized, failure("unauthorized", "invalid api key"))
			return
		}
		key := r.Header.Get("Idempotency-Key")
		scope := r.Method + " " + r.URL.Path + " " + key
		if key != "" {
			s.mu.Lock()
			previous, ok := s.responses[scope]
			s.mu.Unlock()
			if ok {
				writeRaw(w, previous.status, previous.body)
				return
			}
		}
		status, body := http.StatusOK, []byte(nil)
		result, err := handle(r)
		var failed *apiError
		switch {
		case errors.As(err, &failed):
			status, body = failed.status, mustJSON(failure(failed.code, failed.message))
		case err != nil:
			status, body = http.StatusInternalServerError, mustJSON(failure("internal", err.Error()))
		default:
			body = mustJSON(result)
		}
		if key != "" && status < http.StatusBadRequest {
			s.mu.Lock()
			s.responses[scope] = stored{status: status, body: body}
			s.mu.Unlock()
		}
		writeRaw(w, status, body)
	}
}

func (s *Server) createIntent(r *http.Request) (any, error) {
	var in intentRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		return nil, invalid("malformed body")
	}
	target, err := url.Parse(in.ReturnURL)
	switch {
	case in.Amount <= 0:
		return nil, invalid("amount must be positive")
	case len(in.Currency) != 3:
		return nil, invalid("currency is invalid")
	case in.Reference == "":
		return nil, invalid("reference is required")
	case err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "":
		return nil, invalid("return_url must be an absolute http(s) url")
	}
	it := &intent{
		id: "pi_" + randomID(), reference: in.Reference, status: statusRequiresConfirmation, amount: in.Amount,
		currency: strings.ToUpper(in.Currency), returnURL: in.ReturnURL, description: in.Description,
		saveMethod: in.SaveMethod && in.MethodToken == "", methodToken: in.MethodToken, createdAt: s.cfg.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intents[it.id] = it
	return s.view(it), nil
}

func (s *Server) getIntent(r *http.Request) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.find(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return s.view(it), nil
}

func (s *Server) capture(r *http.Request) (any, error) {
	var in amountRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		return nil, invalid("malformed body")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.find(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if it.status != statusAuthorized {
		return nil, conflict("intent is %s", it.status)
	}
	if in.Amount <= 0 || in.Amount > it.amount {
		return nil, invalid("capture amount exceeds authorization")
	}
	it.status, it.captured = statusCaptured, in.Amount
	return s.view(it), nil
}

func (s *Server) cancel(r *http.Request) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.find(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	switch it.status {
	case statusRequiresConfirmation, statusAuthorized:
		it.status = statusCancelled
	case statusCancelled, statusFailed:
	default:
		return nil, conflict("intent is %s", it.status)
	}
	return s.view(it), nil
}

func (s *Server) refund(r *http.Request) (any, error) {
	var in amountRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		return nil, invalid("malformed body")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.find(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if in.Reference != "" {
		if index := slices.IndexFunc(it.refunds, func(rf refund) bool { return rf.reference == in.Reference }); index >= 0 {
			existing := it.refunds[index]
			return refundResponse{ID: existing.id, Reference: existing.reference, Status: "succeeded", Amount: existing.amount}, nil
		}
	}
	if it.status != statusCaptured {
		return nil, conflict("intent is %s", it.status)
	}
	if in.Amount <= 0 || in.Amount > it.captured-it.refunded {
		return nil, invalid("refund amount exceeds captured funds")
	}
	created := refund{id: "re_" + randomID(), reference: in.Reference, amount: in.Amount}
	it.refunds = append(it.refunds, created)
	it.refunded += in.Amount
	if it.refunded == it.captured {
		it.status = statusRefunded
	}
	return refundResponse{ID: created.id, Reference: created.reference, Status: "succeeded", Amount: created.amount}, nil
}

func (s *Server) transactions(r *http.Request) (any, error) {
	day, err := time.Parse(time.DateOnly, r.URL.Query().Get("date"))
	if err != nil {
		return nil, invalid("date must be YYYY-MM-DD")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := transactionsResponse{Data: []intentResponse{}}
	for _, it := range s.intents {
		if !it.createdAt.Before(day) && it.createdAt.Before(day.AddDate(0, 0, 1)) {
			out.Data = append(out.Data, s.view(it))
		}
	}
	slices.SortFunc(out.Data, func(a, b intentResponse) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

var payPage = template.Must(template.New("pay").Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sandbox PSP</title>
<style>body{font-family:system-ui,sans-serif;max-width:420px;margin:48px auto;padding:0 16px;color:#1b1b1f}
.card{border:1px solid #d9d9e0;border-radius:12px;padding:24px}.amount{font-size:32px;font-weight:600;margin:8px 0 16px}
.muted{color:#6b6b76;font-size:14px}form{display:inline}button{font-size:16px;padding:10px 18px;border-radius:8px;border:0;margin:16px 8px 0 0;cursor:pointer}
.approve{background:#1f7a4d;color:#fff}.decline{background:#e9e9ef;color:#1b1b1f}</style></head>
<body><div class="card"><div class="muted">Sandbox PSP · тестовый платёж</div>
<div class="amount">{{.Amount}} {{.Currency}}</div><div>{{.Description}}</div>
{{if .Saved}}<p class="muted">Сохранённый способ оплаты</p>{{end}}
{{if .Open}}<form method="post" action="./{{.ID}}/approve"><button class="approve" type="submit">Оплатить</button></form>
<form method="post" action="./{{.ID}}/decline"><button class="decline" type="submit">Отклонить</button></form>
{{else}}<p>Статус: {{.Status}}</p>{{end}}</div></body></html>`))

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	it, err := s.find(r.PathValue("id"))
	var data map[string]any
	if err == nil {
		data = map[string]any{
			"ID": it.id, "Amount": formatMinor(it.amount), "Currency": it.currency, "Description": it.description,
			"Saved": it.methodToken != "", "Open": it.status == statusRequiresConfirmation, "Status": it.status,
		}
	}
	s.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := payPage.Execute(w, data); err != nil {
		s.cfg.Log.ErrorContext(r.Context(), "sandbox pay page", slog.Any("error", err))
	}
}

func (s *Server) confirm(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, target, err := s.resolve(r.PathValue("id"), approve)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err := s.deliver(r.Context(), event); err != nil {
			s.cfg.Log.ErrorContext(r.Context(), "sandbox webhook delivery failed",
				slog.String("event_id", event.ID), slog.Any("error", err))
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func (s *Server) resolve(id string, approve bool) (webhook, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.find(id)
	if err != nil {
		return webhook{}, "", err
	}
	if it.status != statusRequiresConfirmation {
		return webhook{}, "", conflict("intent is %s", it.status)
	}
	event := webhook{
		ID: "evt_" + randomID(), Created: s.cfg.Now().Unix(),
		Data: webhookData{IntentID: it.id, Amount: it.amount, Currency: it.currency},
	}
	if approve {
		it.status, event.Type = statusAuthorized, "payment.authorized"
		if it.saveMethod {
			event.Data.MethodToken, event.Data.MethodLabel = "tok_"+randomID(), "Sandbox Visa •••• 4242"
		}
	} else {
		it.status, event.Type, event.Data.Reason = statusFailed, "payment.failed", "declined by customer"
	}
	target, err := url.Parse(it.returnURL)
	if err != nil {
		return webhook{}, "", invalid("return url is invalid")
	}
	query := target.Query()
	query.Set("payment_intent", it.id)
	query.Set("status", it.status)
	target.RawQuery = query.Encode()
	return event, target.String(), nil
}

func (s *Server) deliver(ctx context.Context, event webhook) error {
	if s.cfg.WebhookURL == "" {
		return nil
	}
	body := mustJSON(event)
	var lastErr error
	for attempt := range s.cfg.Attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
		if lastErr = s.post(ctx, body); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func (s *Server) post(ctx context.Context, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(SignatureHeader, Sign(s.cfg.Secret, body, s.cfg.Now()))
	response, err := s.cfg.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook endpoint answered %d", response.StatusCode)
	}
	return nil
}

func (s *Server) find(id string) (*intent, error) {
	it, ok := s.intents[id]
	if !ok {
		return nil, &apiError{status: http.StatusNotFound, code: "not_found", message: "intent not found"}
	}
	return it, nil
}

func (s *Server) view(it *intent) intentResponse {
	return intentResponse{
		ID: it.id, Reference: it.reference, Status: it.status, Amount: it.amount, Captured: it.captured,
		Refunded: it.refunded, Currency: it.currency, RedirectURL: s.cfg.PublicURL + "/pay/" + it.id,
	}
}

func invalid(message string) error {
	return &apiError{status: http.StatusUnprocessableEntity, code: "invalid_request", message: message}
}

func conflict(format string, args ...any) error {
	return &apiError{status: http.StatusConflict, code: "invalid_state", message: fmt.Sprintf(format, args...)}
}

func failure(code, message string) errorBody {
	var body errorBody
	body.Error.Code, body.Error.Message = code, message
	return body
}

func mustJSON(v any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return body
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	writeRaw(w, status, mustJSON(v))
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func randomID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func formatMinor(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}
