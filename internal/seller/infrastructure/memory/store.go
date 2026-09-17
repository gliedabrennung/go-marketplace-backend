package memory

import (
	"context"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type commissionRecord struct {
	rate      int
	updatedAt time.Time
	version   int
}

type Store struct {
	mu          sync.Mutex
	sellers     map[string]domain.SellerSnapshot
	commissions map[string]commissionRecord
	audit       []application.AuditEntry
	events      []kernel.DomainEvent
}

func NewStore() *Store {
	return &Store{
		sellers:     make(map[string]domain.SellerSnapshot),
		commissions: make(map[string]commissionRecord),
	}
}

func (s *Store) Sellers() domain.SellerRepository { return sellerRepository{s} }

func (s *Store) CategoryCommissions() domain.CategoryCommissionRepository {
	return commissionRepository{s}
}

func (s *Store) Audit() application.AuditTrail { return auditTrail{s} }

func (s *Store) Events() []kernel.DomainEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

func (s *Store) AuditEntries() []application.AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.audit)
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

type sellerRepository struct{ s *Store }

func (r sellerRepository) FindByID(_ context.Context, id kernel.SellerID) (*domain.Seller, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.sellers[id.String()]
	if !ok {
		return nil, domain.ErrSellerNotFound
	}
	return domain.RehydrateSeller(clone(snap))
}

func (r sellerRepository) Save(_ context.Context, seller *domain.Seller) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := clone(seller.Snapshot())
	current, exists := r.s.sellers[next.ID]
	if (next.Version == 0 && exists) || (next.Version > 0 && (!exists || current.Version != next.Version)) {
		return kernel.ErrConcurrentModification
	}
	if next.Status != string(domain.StatusTerminated) {
		for id, other := range r.s.sellers {
			if id == next.ID || other.Status == string(domain.StatusTerminated) {
				continue
			}
			if other.OwnerID == next.OwnerID {
				return domain.ErrSellerAlreadyExists
			}
			if other.TaxID == next.TaxID {
				return domain.ErrTaxIDTaken
			}
		}
	}
	next.Version++
	r.s.sellers[next.ID] = next
	seller.AdvanceVersion()
	r.s.events = append(r.s.events, seller.PullEvents()...)
	return nil
}

type commissionRepository struct{ s *Store }

func (r commissionRepository) FindByCategory(_ context.Context, id domain.CategoryID) (*domain.CategoryCommission, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	rec, ok := r.s.commissions[id.String()]
	if !ok {
		return nil, domain.ErrCategoryCommissionNotFound
	}
	return domain.RehydrateCategoryCommission(id.String(), rec.rate, rec.updatedAt, rec.version)
}

func (r commissionRepository) Save(_ context.Context, c *domain.CategoryCommission) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	key := c.CategoryID().String()
	current, exists := r.s.commissions[key]
	if (c.Version() == 0 && exists) || (c.Version() > 0 && (!exists || current.version != c.Version())) {
		return kernel.ErrConcurrentModification
	}
	r.s.commissions[key] = commissionRecord{rate: c.Rate().Value(), updatedAt: c.UpdatedAt(), version: c.Version() + 1}
	c.AdvanceVersion()
	r.s.events = append(r.s.events, c.PullEvents()...)
	return nil
}

type auditTrail struct{ s *Store }

func (a auditTrail) Record(_ context.Context, e application.AuditEntry) error {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	a.s.audit = append(a.s.audit, e)
	return nil
}

func clone(snap domain.SellerSnapshot) domain.SellerSnapshot {
	out := snap
	out.Documents = slices.Clone(snap.Documents)
	out.Members = slices.Clone(snap.Members)
	out.CommissionOverrides = maps.Clone(snap.CommissionOverrides)
	if snap.Rating != nil {
		rating := *snap.Rating
		out.Rating = &rating
	}
	return out
}

func (s *Store) Seller(_ context.Context, sellerID string) (query.SellerView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.sellers[sellerID]
	if !ok {
		return query.SellerView{}, domain.ErrSellerNotFound
	}
	return query.NewSellerView(snap), nil
}

func (s *Store) ListByMember(_ context.Context, userID string) ([]query.SellerSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []query.SellerSummary
	for _, snap := range s.sellers {
		for _, m := range snap.Members {
			if m.UserID == userID {
				out = append(out, summary(snap, m.Role))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) ListByStatus(_ context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[query.SellerSummary], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []query.SellerSummary
	for _, snap := range s.sellers {
		if snap.Status != status {
			continue
		}
		if after != nil && !isAfter(snap, *after) {
			continue
		}
		rows = append(rows, summary(snap, ""))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].UpdatedAt.Before(rows[j].UpdatedAt)
	})
	if len(rows) > limit+1 {
		rows = rows[:limit+1]
	}
	return pagination.Build(rows, limit, func(v query.SellerSummary) pagination.Keyset {
		return pagination.Keyset{At: v.UpdatedAt, ID: v.ID}
	}), nil
}

func isAfter(snap domain.SellerSnapshot, k pagination.Keyset) bool {
	if snap.UpdatedAt.Equal(k.At) {
		return snap.ID > k.ID
	}
	return snap.UpdatedAt.After(k.At)
}

func summary(snap domain.SellerSnapshot, role string) query.SellerSummary {
	return query.SellerSummary{
		ID: snap.ID, Status: snap.Status, LegalName: snap.LegalName, TaxID: snap.TaxID,
		Role: role, CreatedAt: snap.CreatedAt, UpdatedAt: snap.UpdatedAt,
	}
}
