package memory

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type Store struct {
	mu     sync.Mutex
	orders map[string]domain.OrderSnapshot
	sagas  map[string]domain.SagaSnapshot
	events []kernel.DomainEvent
}

func NewStore() *Store {
	return &Store{orders: map[string]domain.OrderSnapshot{}, sagas: map[string]domain.SagaSnapshot{}}
}

func (s *Store) Orders() domain.OrderRepository { return orderRepository{s} }

func (s *Store) Sagas() domain.SagaRepository { return sagaRepository{s} }

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

type orderRepository struct{ s *Store }

func (r orderRepository) FindByID(_ context.Context, id domain.OrderID) (*domain.Order, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.orders[id.String()]
	if !ok {
		return nil, domain.ErrOrderNotFound
	}
	return domain.RehydrateOrder(cloneOrder(snap))
}

func (r orderRepository) Save(_ context.Context, order *domain.Order) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := order.Snapshot()
	current, exists := r.s.orders[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.orders[next.ID] = cloneOrder(next)
	order.AdvanceVersion()
	r.s.events = append(r.s.events, order.PullEvents()...)
	return nil
}

func (r orderRepository) DeliveredBefore(_ context.Context, before time.Time, limit int) ([]domain.OrderID, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snaps := make([]domain.OrderSnapshot, 0, len(r.s.orders))
	for _, snap := range r.s.orders {
		snaps = append(snaps, snap)
	}
	slices.SortFunc(snaps, func(a, b domain.OrderSnapshot) int { return a.DeliveredAt.Compare(b.DeliveredAt) })
	ids := []domain.OrderID{}
	for _, snap := range snaps {
		if snap.Status != string(domain.StatusDelivered) || snap.DeliveredAt.IsZero() || !snap.DeliveredAt.Before(before) {
			continue
		}
		if len(ids) >= limit {
			break
		}
		id, err := domain.ParseOrderID(snap.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

type sagaRepository struct{ s *Store }

func (r sagaRepository) FindByOrder(_ context.Context, id domain.OrderID) (*domain.CheckoutSaga, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.sagas[id.String()]
	if !ok {
		return nil, domain.ErrSagaNotFound
	}
	return domain.RehydrateSaga(cloneSaga(snap))
}

func (r sagaRepository) FindByPayment(_ context.Context, paymentID string) (*domain.CheckoutSaga, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, snap := range r.s.sagas {
		if snap.PaymentID == paymentID {
			return domain.RehydrateSaga(cloneSaga(snap))
		}
	}
	return nil, domain.ErrSagaNotFound
}

func (r sagaRepository) Expired(_ context.Context, now time.Time, limit int) ([]domain.OrderID, error) {
	return r.s.matching(limit, func(saga *domain.CheckoutSaga) bool { return saga.Expired(now) })
}

func (r sagaRepository) Compensating(_ context.Context, now time.Time, limit int) ([]domain.OrderID, error) {
	return r.s.matching(limit, func(saga *domain.CheckoutSaga) bool { return saga.DueForCompensation(now) })
}

func (r sagaRepository) Stalled(_ context.Context, before time.Time, limit int) ([]domain.OrderID, error) {
	return r.s.matching(limit, func(saga *domain.CheckoutSaga) bool {
		return saga.Committing() && saga.Snapshot().UpdatedAt.Before(before)
	})
}

func (s *Store) matching(limit int, match func(saga *domain.CheckoutSaga) bool) ([]domain.OrderID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snaps := make([]domain.SagaSnapshot, 0, len(s.sagas))
	for _, snap := range s.sagas {
		snaps = append(snaps, snap)
	}
	slices.SortFunc(snaps, func(a, b domain.SagaSnapshot) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
	ids := []domain.OrderID{}
	for _, snap := range snaps {
		saga, err := domain.RehydrateSaga(cloneSaga(snap))
		if err != nil {
			return nil, err
		}
		if match(saga) && len(ids) < limit {
			ids = append(ids, saga.OrderID())
		}
	}
	return ids, nil
}

func (r sagaRepository) Save(_ context.Context, saga *domain.CheckoutSaga) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := saga.Snapshot()
	current, exists := r.s.sagas[next.OrderID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.sagas[next.OrderID] = cloneSaga(next)
	saga.AdvanceVersion()
	return nil
}

func cloneOrder(snap domain.OrderSnapshot) domain.OrderSnapshot {
	out := snap
	out.Items, out.Parts, out.History = slices.Clone(snap.Items), slices.Clone(snap.Parts), slices.Clone(snap.History)
	return out
}

func cloneSaga(snap domain.SagaSnapshot) domain.SagaSnapshot {
	out := snap
	out.Compensated = slices.Clone(snap.Compensated)
	return out
}

type ReadModel struct {
	s *Store
}

func NewReadModel(store *Store) *ReadModel {
	return &ReadModel{s: store}
}

func (m *ReadModel) Order(_ context.Context, id string) (domain.OrderSnapshot, query.SagaState, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	snap, ok := m.s.orders[id]
	if !ok {
		return domain.OrderSnapshot{}, query.SagaState{}, domain.ErrOrderNotFound
	}
	saga := m.s.sagas[id]
	return cloneOrder(snap), query.SagaState{Status: saga.Status, Step: saga.Step, PaymentID: saga.PaymentID, Deadline: saga.Deadline}, nil
}

func (m *ReadModel) BuyerOrders(_ context.Context, buyerID, status string, limit int, after *pagination.Keyset) (pagination.Page[query.OrderSummaryView], error) {
	rows := []query.OrderSummaryView{}
	for _, snap := range m.sorted(after) {
		if snap.BuyerID != buyerID || (status != "" && snap.Status != status) {
			continue
		}
		count := 0
		for _, item := range snap.Items {
			count += item.Quantity
		}
		rows = append(rows, query.OrderSummaryView{
			ID: snap.ID, Status: snap.Status, Currency: snap.Currency, Total: snap.Total, ItemsCount: count, CreatedAt: snap.CreatedAt,
		})
	}
	return pagination.Build(rows, limit, func(v query.OrderSummaryView) pagination.Keyset { return pagination.Keyset{At: v.CreatedAt, ID: v.ID} }), nil
}

func (m *ReadModel) SellerOrders(_ context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[query.SellerOrderView], error) {
	rows := []query.SellerOrderView{}
	for _, snap := range m.sorted(after) {
		current, _ := domain.ParseStatus(snap.Status)
		index := slices.IndexFunc(snap.Parts, func(p domain.PartSnapshot) bool { return p.SellerID == sellerID })
		if index < 0 || !current.VisibleToSeller() || (status != "" && snap.Status != status) {
			continue
		}
		view := query.SellerOrderView{ID: snap.ID, Status: snap.Status, Currency: snap.Currency, Part: query.PartView(snap.Parts[index]), CreatedAt: snap.CreatedAt}
		for _, item := range snap.Items {
			if item.SellerID == sellerID {
				view.Items = append(view.Items, query.NewItemView(item))
			}
		}
		rows = append(rows, view)
	}
	return pagination.Build(rows, limit, func(v query.SellerOrderView) pagination.Keyset { return pagination.Keyset{At: v.CreatedAt, ID: v.ID} }), nil
}

func (m *ReadModel) Sagas(_ context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[query.SagaView], error) {
	m.s.mu.Lock()
	snaps := make([]domain.SagaSnapshot, 0, len(m.s.sagas))
	for _, snap := range m.s.sagas {
		snaps = append(snaps, cloneSaga(snap))
	}
	m.s.mu.Unlock()
	slices.SortFunc(snaps, func(a, b domain.SagaSnapshot) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.OrderID, a.OrderID)
	})
	rows := []query.SagaView{}
	for _, snap := range snaps {
		if (status != "" && snap.Status != status) || (after != nil && !before(snap.UpdatedAt, snap.OrderID, *after)) {
			continue
		}
		rows = append(rows, query.SagaView{
			OrderID: snap.OrderID, BuyerID: snap.BuyerID, Status: snap.Status, Step: snap.Step, ReservationID: snap.ReservationID,
			PaymentID: snap.PaymentID, Reason: snap.Reason, LastError: snap.LastError, Attempts: snap.Attempts,
			Compensated: snap.Compensated, Deadline: snap.Deadline, UpdatedAt: snap.UpdatedAt,
		})
	}
	return pagination.Build(rows, limit, func(v query.SagaView) pagination.Keyset { return pagination.Keyset{At: v.UpdatedAt, ID: v.OrderID} }), nil
}

func (m *ReadModel) sorted(after *pagination.Keyset) []domain.OrderSnapshot {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	out := []domain.OrderSnapshot{}
	for _, snap := range m.s.orders {
		if after == nil || before(snap.CreatedAt, snap.ID, *after) {
			out = append(out, cloneOrder(snap))
		}
	}
	slices.SortFunc(out, func(a, b domain.OrderSnapshot) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out
}

func before(at time.Time, id string, key pagination.Keyset) bool {
	return at.Before(key.At) || (at.Equal(key.At) && id < key.ID)
}
