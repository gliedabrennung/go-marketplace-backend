package memory

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Store struct {
	mu       sync.Mutex
	payments map[string]domain.PaymentSnapshot
	methods  map[string]domain.SavedMethodSnapshot
	webhooks map[string]bool
	reports  map[string]application.ReconciliationReport
	events   []kernel.DomainEvent
}

func NewStore() *Store {
	return &Store{
		payments: make(map[string]domain.PaymentSnapshot),
		methods:  make(map[string]domain.SavedMethodSnapshot),
		webhooks: make(map[string]bool),
		reports:  make(map[string]application.ReconciliationReport),
	}
}

func (s *Store) Payments() domain.PaymentRepository { return paymentRepository{s} }

func (s *Store) Methods() domain.MethodRepository { return methodRepository{s} }

func (s *Store) Webhooks() domain.WebhookLog { return webhookLog{s} }

func (s *Store) Events() []kernel.DomainEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

type UnitOfWork struct {
	store *Store
}

func NewUnitOfWork(store *Store) *UnitOfWork {
	return &UnitOfWork{store: store}
}

func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context, repos application.Repositories) error) error {
	return fn(ctx, u.store)
}

func stale(exists bool, current, next int) bool {
	return (next == 0 && exists) || (next > 0 && (!exists || current != next))
}

type paymentRepository struct{ s *Store }

func (r paymentRepository) FindByID(_ context.Context, id domain.PaymentID) (*domain.Payment, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.payments[id.String()]
	if !ok {
		return nil, domain.ErrPaymentNotFound
	}
	return domain.RehydratePayment(clonePayment(snap))
}

func (r paymentRepository) FindByProviderID(_ context.Context, provider, providerPaymentID string) (*domain.Payment, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, snap := range r.s.payments {
		if snap.Provider == provider && snap.ProviderPaymentID == providerPaymentID && providerPaymentID != "" {
			return domain.RehydratePayment(clonePayment(snap))
		}
	}
	return nil, domain.ErrPaymentNotFound
}

func (r paymentRepository) Save(_ context.Context, payment *domain.Payment) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := payment.Snapshot()
	current, exists := r.s.payments[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.payments[next.ID] = clonePayment(next)
	payment.AdvanceVersion()
	r.s.events = append(r.s.events, payment.PullEvents()...)
	return nil
}

type methodRepository struct{ s *Store }

func (r methodRepository) FindByID(_ context.Context, id domain.MethodID) (*domain.SavedMethod, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.methods[id.String()]
	if !ok {
		return nil, domain.ErrMethodNotFound
	}
	return domain.RehydrateSavedMethod(snap)
}

func (r methodRepository) FindByToken(_ context.Context, provider, token string) (*domain.SavedMethod, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, snap := range r.s.methods {
		if snap.Provider == provider && snap.Token == token {
			return domain.RehydrateSavedMethod(snap)
		}
	}
	return nil, domain.ErrMethodNotFound
}

func (r methodRepository) Save(_ context.Context, method *domain.SavedMethod) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := method.Snapshot()
	current, exists := r.s.methods[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.methods[next.ID] = next
	method.AdvanceVersion()
	return nil
}

type webhookLog struct{ s *Store }

func (w webhookLog) Record(_ context.Context, provider, eventID, _ string, _ time.Time) (bool, error) {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	key := provider + "|" + eventID
	if w.s.webhooks[key] {
		return false, nil
	}
	w.s.webhooks[key] = true
	return true, nil
}

func clonePayment(snap domain.PaymentSnapshot) domain.PaymentSnapshot {
	out := snap
	out.Refunds = slices.Clone(snap.Refunds)
	return out
}

type Reconciliations struct {
	s *Store
}

func NewReconciliations(store *Store) *Reconciliations {
	return &Reconciliations{s: store}
}

func (r *Reconciliations) Ledger(_ context.Context, provider string, day time.Time) ([]application.LedgerEntry, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	from, to := day.UTC(), day.UTC().AddDate(0, 0, 1)
	entries := []application.LedgerEntry{}
	for _, snap := range r.s.payments {
		created := snap.CreatedAt.UTC()
		if snap.Provider != provider || snap.ProviderPaymentID == "" || created.Before(from) || !created.Before(to) {
			continue
		}
		entries = append(entries, application.LedgerEntry{
			PaymentID: snap.ID, ProviderPaymentID: snap.ProviderPaymentID, Status: snap.Status,
			Captured: snap.Captured, Refunded: snap.Refunded, Currency: snap.Currency,
		})
	}
	slices.SortFunc(entries, func(a, b application.LedgerEntry) int { return cmp.Compare(a.PaymentID, b.PaymentID) })
	return entries, nil
}

func (r *Reconciliations) Save(_ context.Context, report application.ReconciliationReport) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.reports[reportKey(report.Provider, report.Day)] = report
	return nil
}

func reportKey(provider string, day time.Time) string {
	return provider + "|" + day.UTC().Format(time.DateOnly)
}

type ReadModel struct {
	s *Store
}

func NewReadModel(store *Store) *ReadModel {
	return &ReadModel{s: store}
}

func (m *ReadModel) Payment(_ context.Context, paymentID string) (query.PaymentView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	snap, ok := m.s.payments[paymentID]
	if !ok {
		return query.PaymentView{}, domain.ErrPaymentNotFound
	}
	return query.NewPaymentView(snap), nil
}

func (m *ReadModel) OrderPayments(_ context.Context, orderID string) ([]query.PaymentView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	views := []query.PaymentView{}
	for _, snap := range m.s.payments {
		if snap.OrderID == orderID {
			views = append(views, query.NewPaymentView(snap))
		}
	}
	slices.SortFunc(views, func(a, b query.PaymentView) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return views, nil
}

func (m *ReadModel) Methods(_ context.Context, buyerID string) ([]query.MethodView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	views := []query.MethodView{}
	for _, snap := range m.s.methods {
		if snap.BuyerID == buyerID && snap.RemovedAt.IsZero() {
			views = append(views, query.MethodView{ID: snap.ID, Provider: snap.Provider, Label: snap.Label, CreatedAt: snap.CreatedAt})
		}
	}
	slices.SortFunc(views, func(a, b query.MethodView) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return views, nil
}

func (m *ReadModel) Reconciliation(_ context.Context, provider string, day time.Time) (query.ReconciliationView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	report, ok := m.s.reports[reportKey(provider, day)]
	if !ok {
		return query.ReconciliationView{}, query.ErrReportNotFound
	}
	return query.NewReconciliationView(report), nil
}
