package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/domain"
)

type Store struct {
	mu      sync.Mutex
	entries map[string]domain.EntrySnapshot
}

func NewStore() *Store {
	return &Store{entries: map[string]domain.EntrySnapshot{}}
}

func (s *Store) Entries() domain.Repository { return repository{s} }

type UnitOfWork struct {
	store *Store
}

func NewUnitOfWork(store *Store) *UnitOfWork {
	return &UnitOfWork{store: store}
}

func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context, repos application.Repositories) error) error {
	return fn(ctx, u.store)
}

type repository struct{ s *Store }

func key(orderID, sellerID string) string { return orderID + "|" + sellerID }

func (r repository) FindByOrderAndSeller(_ context.Context, orderID, sellerID string) (*domain.Entry, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.entries[key(orderID, sellerID)]
	if !ok {
		return nil, domain.ErrEntryNotFound
	}
	return domain.RehydrateEntry(snap)
}

func (r repository) Save(_ context.Context, entry *domain.Entry) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	id := key(entry.OrderID(), entry.SellerID().String())
	if _, exists := r.s.entries[id]; exists {
		return domain.ErrAlreadyAccrued
	}
	r.s.entries[id] = entry.Snapshot()
	return nil
}

func (r repository) ListBySeller(_ context.Context, sellerID string, from, to time.Time) ([]*domain.Entry, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snaps := make([]domain.EntrySnapshot, 0, len(r.s.entries))
	for _, snap := range r.s.entries {
		if snap.SellerID == sellerID && !snap.AccruedAt.Before(from) && snap.AccruedAt.Before(to) {
			snaps = append(snaps, snap)
		}
	}
	slices.SortFunc(snaps, func(a, b domain.EntrySnapshot) int { return a.AccruedAt.Compare(b.AccruedAt) })
	out := make([]*domain.Entry, 0, len(snaps))
	for _, snap := range snaps {
		entry, err := domain.RehydrateEntry(snap)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

type ReadModel struct {
	s *Store
}

func NewReadModel(store *Store) *ReadModel {
	return &ReadModel{s: store}
}

func (m *ReadModel) ListBySeller(ctx context.Context, sellerID string, from, to time.Time) ([]query.EntryView, error) {
	entries, err := repository{m.s}.ListBySeller(ctx, sellerID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]query.EntryView, 0, len(entries))
	for _, entry := range entries {
		snap := entry.Snapshot()
		out = append(out, query.EntryView{
			ID: snap.ID, OrderID: snap.OrderID, Currency: snap.Currency, Gross: snap.Gross, Commission: snap.Commission,
			Net: snap.Net, AccruedAt: snap.AccruedAt,
		})
	}
	return out, nil
}
