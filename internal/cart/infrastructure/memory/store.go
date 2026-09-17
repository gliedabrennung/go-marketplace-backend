package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Store struct {
	mu    sync.Mutex
	carts map[string]domain.CartSnapshot
}

func NewStore() *Store {
	return &Store{carts: make(map[string]domain.CartSnapshot)}
}

func (s *Store) Carts() domain.Repository { return s }

type UnitOfWork struct {
	store *Store
}

func NewUnitOfWork(store *Store) *UnitOfWork {
	return &UnitOfWork{store: store}
}

func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context, repos application.Repositories) error) error {
	return fn(ctx, u.store)
}

func (s *Store) FindByOwner(_ context.Context, owner domain.Owner) (*domain.Cart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, snap := range s.carts {
		if snap.OwnerKind == string(owner.Kind()) && snap.OwnerID == owner.ID() {
			return domain.Rehydrate(clone(snap))
		}
	}
	return nil, domain.ErrCartNotFound
}

func (s *Store) Save(_ context.Context, cart *domain.Cart) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cart.Snapshot()
	current, exists := s.carts[next.ID]
	if (next.Version == 0 && exists) || (next.Version > 0 && (!exists || current.Version != next.Version)) {
		return kernel.ErrConcurrentModification
	}
	if next.Version == 0 {
		for _, other := range s.carts {
			if other.OwnerKind == next.OwnerKind && other.OwnerID == next.OwnerID {
				return kernel.ErrConcurrentModification
			}
		}
	}
	next.Version++
	s.carts[next.ID] = clone(next)
	cart.AdvanceVersion()
	return nil
}

func (s *Store) Delete(_ context.Context, cart *domain.Cart) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.carts[cart.ID().String()]
	if !exists || current.Version != cart.Version() {
		return kernel.ErrConcurrentModification
	}
	delete(s.carts, cart.ID().String())
	return nil
}

func (s *Store) DeleteExpired(_ context.Context, before time.Time, limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, snap := range s.carts {
		if removed >= limit {
			break
		}
		if !snap.ExpiresAt.IsZero() && snap.ExpiresAt.Before(before) {
			delete(s.carts, id)
			removed++
		}
	}
	return removed, nil
}

func clone(snap domain.CartSnapshot) domain.CartSnapshot {
	out := snap
	out.Items = slices.Clone(snap.Items)
	return out
}
