package memory

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type Store struct {
	mu        sync.Mutex
	items     map[string]domain.StockItemSnapshot
	movements []domain.Movement
	events    []kernel.DomainEvent
}

func NewStore() *Store {
	return &Store{items: make(map[string]domain.StockItemSnapshot)}
}

func (s *Store) Stock() domain.StockRepository { return stockRepository{s} }

func (s *Store) Events() []kernel.DomainEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

func (s *Store) Movements() []domain.Movement {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.movements)
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

type stockRepository struct{ s *Store }

func (r stockRepository) FindBySKU(_ context.Context, sku domain.SKU) (*domain.StockItem, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.items[sku.String()]
	if !ok {
		return nil, domain.ErrStockNotFound
	}
	return domain.RehydrateStockItem(clone(snap))
}

func (r stockRepository) Lock(_ context.Context, skus []domain.SKU) ([]*domain.StockItem, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.load(func(snap domain.StockItemSnapshot) bool {
		return slices.ContainsFunc(skus, func(sku domain.SKU) bool { return sku.String() == snap.SKU })
	}, len(skus))
}

func (r stockRepository) LockByReservation(_ context.Context, id domain.ReservationID) ([]*domain.StockItem, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.load(func(snap domain.StockItemSnapshot) bool {
		return slices.ContainsFunc(snap.Holds, func(h domain.HoldSnapshot) bool { return h.ReservationID == id.String() })
	}, 0)
}

func (r stockRepository) LockExpired(_ context.Context, before time.Time, limit int) ([]*domain.StockItem, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.load(func(snap domain.StockItemSnapshot) bool {
		return slices.ContainsFunc(snap.Holds, func(h domain.HoldSnapshot) bool {
			return h.Status == string(domain.HoldHeld) && !h.ExpiresAt.After(before)
		})
	}, limit)
}

func (r stockRepository) Save(_ context.Context, item *domain.StockItem) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := clone(item.Snapshot())
	current, exists := r.s.items[next.SKU]
	if next.Version == 0 && exists {
		return domain.ErrStockExists
	}
	if next.Version > 0 && (!exists || current.Version != next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.items[next.SKU] = next
	item.AdvanceVersion()
	for _, movement := range item.PullMovements() {
		if slices.ContainsFunc(r.s.movements, func(m domain.Movement) bool {
			return m.SKU == movement.SKU && m.Reason == movement.Reason && m.ReferenceID == movement.ReferenceID
		}) {
			continue
		}
		r.s.movements = append(r.s.movements, movement)
	}
	r.s.events = append(r.s.events, item.PullEvents()...)
	return nil
}

func (s *Store) load(match func(domain.StockItemSnapshot) bool, limit int) ([]*domain.StockItem, error) {
	var snaps []domain.StockItemSnapshot
	for _, snap := range s.items {
		if match(snap) {
			snaps = append(snaps, clone(snap))
		}
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].SKU < snaps[j].SKU })
	if limit > 0 && len(snaps) > limit {
		snaps = snaps[:limit]
	}
	items := make([]*domain.StockItem, 0, len(snaps))
	for _, snap := range snaps {
		item, err := domain.RehydrateStockItem(snap)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func clone(snap domain.StockItemSnapshot) domain.StockItemSnapshot {
	out := snap
	out.Holds = slices.Clone(snap.Holds)
	return out
}

type ReadModel struct {
	s *Store
}

func NewReadModel(store *Store) *ReadModel {
	return &ReadModel{s: store}
}

func (m *ReadModel) Stock(_ context.Context, sku string) (query.StockView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	snap, ok := m.s.items[sku]
	if !ok {
		return query.StockView{}, domain.ErrStockNotFound
	}
	return view(snap), nil
}

func (m *ReadModel) SellerStock(_ context.Context, sellerID string, limit int, after *pagination.Keyset) (pagination.Page[query.StockView], error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	var rows []query.StockView
	for _, snap := range m.s.items {
		if snap.SellerID != sellerID {
			continue
		}
		if after != nil && snap.SKU <= after.ID {
			continue
		}
		rows = append(rows, view(snap))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SKU < rows[j].SKU })
	if len(rows) > limit+1 {
		rows = rows[:limit+1]
	}
	return pagination.Build(rows, limit, func(v query.StockView) pagination.Keyset {
		return pagination.Keyset{At: v.UpdatedAt, ID: v.SKU}
	}), nil
}

func (m *ReadModel) Movements(_ context.Context, sku string, limit int) ([]query.MovementView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	out := []query.MovementView{}
	for _, movement := range slices.Backward(m.s.movements) {
		if movement.SKU != sku {
			continue
		}
		out = append(out, query.MovementView{
			SKU: movement.SKU, Delta: movement.Delta, Reason: string(movement.Reason),
			ReferenceID: movement.ReferenceID, OccurredAt: movement.OccurredAt,
		})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *ReadModel) Reservation(_ context.Context, reservationID string) (query.ReservationView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	view := query.ReservationView{ReservationID: reservationID}
	for _, snap := range m.s.items {
		for _, hold := range snap.Holds {
			if hold.ReservationID != reservationID {
				continue
			}
			view.OrderID, view.ExpiresAt = hold.OrderID, hold.ExpiresAt
			view.Status = hold.Status
			view.Lines = append(view.Lines, query.ReservationLineView{
				SKU: snap.SKU, Quantity: hold.Quantity, Status: hold.Status,
			})
		}
	}
	if len(view.Lines) == 0 {
		return query.ReservationView{}, domain.ErrReservationNotFound
	}
	sort.Slice(view.Lines, func(i, j int) bool { return view.Lines[i].SKU < view.Lines[j].SKU })
	return view, nil
}

func (m *ReadModel) Available(_ context.Context, skus []string) (map[string]int, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	out := make(map[string]int, len(skus))
	for _, sku := range skus {
		if snap, ok := m.s.items[sku]; ok {
			out[sku] = snap.Available
		}
	}
	return out, nil
}

func view(snap domain.StockItemSnapshot) query.StockView {
	return query.StockView{
		SKU: snap.SKU, SellerID: snap.SellerID, Available: snap.Available,
		Reserved: snap.Reserved, UpdatedAt: snap.UpdatedAt,
	}
}
