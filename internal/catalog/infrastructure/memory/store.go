package memory

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Store struct {
	mu         sync.Mutex
	categories map[string]domain.CategorySnapshot
	products   map[string]domain.ProductSnapshot
	groups     map[string]domain.VariantGroupSnapshot
	offers     map[string]domain.OfferSnapshot
	imports    map[string]domain.ImportJobSnapshot
	audit      []application.AuditEntry
	events     []kernel.DomainEvent
}

func NewStore() *Store {
	return &Store{
		categories: make(map[string]domain.CategorySnapshot),
		products:   make(map[string]domain.ProductSnapshot),
		groups:     make(map[string]domain.VariantGroupSnapshot),
		offers:     make(map[string]domain.OfferSnapshot),
		imports:    make(map[string]domain.ImportJobSnapshot),
	}
}

func (s *Store) Categories() domain.CategoryRepository { return categoryRepository{s} }

func (s *Store) Products() domain.ProductRepository { return productRepository{s} }

func (s *Store) VariantGroups() domain.VariantGroupRepository { return variantGroupRepository{s} }

func (s *Store) Offers() domain.OfferRepository { return offerRepository{s} }

func (s *Store) Imports() domain.ImportJobRepository { return importRepository{s} }

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

func stale(exists bool, current, next int) bool {
	return (next == 0 && exists) || (next > 0 && (!exists || current != next))
}

type categoryRepository struct{ s *Store }

func (r categoryRepository) FindByID(_ context.Context, id domain.CategoryID) (*domain.Category, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.categories[id.String()]
	if !ok {
		return nil, domain.ErrCategoryNotFound
	}
	return domain.RehydrateCategory(cloneCategory(snap))
}

func (r categoryRepository) FindChain(_ context.Context, id domain.CategoryID) ([]*domain.Category, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	chain, err := r.s.chain(id.String())
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Category, 0, len(chain))
	for _, snap := range chain {
		category, err := domain.RehydrateCategory(cloneCategory(snap))
		if err != nil {
			return nil, err
		}
		out = append(out, category)
	}
	return out, nil
}

func (r categoryRepository) DescendantAttributeCodes(_ context.Context, id domain.CategoryID) ([]string, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var codes []string
	for _, c := range r.s.categories {
		if !slices.Contains(c.Ancestors, id.String()) {
			continue
		}
		for _, a := range c.Attributes {
			codes = append(codes, a.Code)
		}
	}
	return codes, nil
}

func (r categoryRepository) Save(_ context.Context, category *domain.Category) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := cloneCategory(category.Snapshot())
	current, exists := r.s.categories[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	for id, other := range r.s.categories {
		if id != next.ID && other.ParentID == next.ParentID && other.Slug == next.Slug {
			return domain.ErrCategorySlugTaken
		}
	}
	next.Version++
	r.s.categories[next.ID] = next
	category.AdvanceVersion()
	r.s.events = append(r.s.events, category.PullEvents()...)
	return nil
}

type productRepository struct{ s *Store }

func (r productRepository) FindByID(_ context.Context, id domain.ProductID) (*domain.Product, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.products[id.String()]
	if !ok {
		return nil, domain.ErrProductNotFound
	}
	return domain.RehydrateProduct(cloneProduct(snap))
}

func (r productRepository) Save(_ context.Context, product *domain.Product) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := cloneProduct(product.Snapshot())
	current, exists := r.s.products[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.products[next.ID] = next
	product.AdvanceVersion()
	r.s.events = append(r.s.events, product.PullEvents()...)
	return nil
}

type variantGroupRepository struct{ s *Store }

func (r variantGroupRepository) FindByID(_ context.Context, id domain.VariantGroupID) (*domain.VariantGroup, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.groups[id.String()]
	if !ok {
		return nil, domain.ErrVariantGroupNotFound
	}
	return domain.RehydrateVariantGroup(cloneGroup(snap))
}

func (r variantGroupRepository) Save(_ context.Context, group *domain.VariantGroup) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := cloneGroup(group.Snapshot())
	current, exists := r.s.groups[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	for id, other := range r.s.groups {
		if id == next.ID {
			continue
		}
		for _, m := range next.Members {
			if slices.ContainsFunc(other.Members, func(o domain.VariantMemberSnapshot) bool { return o.ProductID == m.ProductID }) {
				return domain.ErrProductAlreadyGrouped
			}
		}
	}
	next.Version++
	r.s.groups[next.ID] = next
	group.AdvanceVersion()
	r.s.events = append(r.s.events, group.PullEvents()...)
	return nil
}

type offerRepository struct{ s *Store }

func (r offerRepository) FindByID(_ context.Context, id domain.OfferID) (*domain.Offer, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.offers[id.String()]
	if !ok {
		return nil, domain.ErrOfferNotFound
	}
	return domain.RehydrateOffer(snap)
}

func (r offerRepository) FindBySellerSKU(_ context.Context, seller kernel.SellerID, sku domain.SellerSKU) (*domain.Offer, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, snap := range r.s.offers {
		if snap.SellerID == seller.String() && snap.SellerSKU == sku.String() && snap.Status != string(domain.OfferArchived) {
			return domain.RehydrateOffer(snap)
		}
	}
	return nil, domain.ErrOfferNotFound
}

func (r offerRepository) Save(_ context.Context, offer *domain.Offer) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := offer.Snapshot()
	current, exists := r.s.offers[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	if next.Status != string(domain.OfferArchived) {
		for id, other := range r.s.offers {
			if id == next.ID || other.SellerID != next.SellerID || other.Status == string(domain.OfferArchived) {
				continue
			}
			if other.SellerSKU == next.SellerSKU {
				return domain.ErrSellerSKUTaken
			}
			if other.ProductID == next.ProductID {
				return domain.ErrOfferExists
			}
		}
	}
	next.Version++
	r.s.offers[next.ID] = next
	offer.AdvanceVersion()
	r.s.events = append(r.s.events, offer.PullEvents()...)
	return nil
}

type importRepository struct{ s *Store }

func (r importRepository) FindByID(_ context.Context, id domain.ImportJobID) (*domain.ImportJob, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.imports[id.String()]
	if !ok {
		return nil, domain.ErrImportJobNotFound
	}
	return domain.RehydrateImportJob(cloneImport(snap))
}

func (r importRepository) Save(_ context.Context, job *domain.ImportJob) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := cloneImport(job.Snapshot())
	current, exists := r.s.imports[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.imports[next.ID] = next
	job.AdvanceVersion()
	r.s.events = append(r.s.events, job.PullEvents()...)
	return nil
}

type auditTrail struct{ s *Store }

func (a auditTrail) Record(_ context.Context, e application.AuditEntry) error {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	a.s.audit = append(a.s.audit, e)
	return nil
}

func (s *Store) chain(id string) ([]domain.CategorySnapshot, error) {
	leaf, ok := s.categories[id]
	if !ok {
		return nil, domain.ErrCategoryNotFound
	}
	out := make([]domain.CategorySnapshot, 0, len(leaf.Ancestors)+1)
	for _, ancestorID := range leaf.Ancestors {
		ancestor, ok := s.categories[ancestorID]
		if !ok {
			return nil, domain.ErrBrokenCategoryChain
		}
		out = append(out, cloneCategory(ancestor))
	}
	return append(out, cloneCategory(leaf)), nil
}

func cloneCategory(snap domain.CategorySnapshot) domain.CategorySnapshot {
	out := snap
	out.Ancestors = slices.Clone(snap.Ancestors)
	out.Attributes = make([]domain.AttributeSpec, len(snap.Attributes))
	for i, a := range snap.Attributes {
		a.Options = slices.Clone(a.Options)
		out.Attributes[i] = a
	}
	return out
}

func cloneProduct(snap domain.ProductSnapshot) domain.ProductSnapshot {
	out := snap
	out.Attributes = maps.Clone(snap.Attributes)
	out.Images = slices.Clone(snap.Images)
	return out
}

func cloneGroup(snap domain.VariantGroupSnapshot) domain.VariantGroupSnapshot {
	out := snap
	out.Axes = slices.Clone(snap.Axes)
	out.Members = make([]domain.VariantMemberSnapshot, len(snap.Members))
	for i, m := range snap.Members {
		out.Members[i] = domain.VariantMemberSnapshot{ProductID: m.ProductID, AxisValues: maps.Clone(m.AxisValues)}
	}
	return out
}

func cloneImport(snap domain.ImportJobSnapshot) domain.ImportJobSnapshot {
	out := snap
	out.Errors = slices.Clone(snap.Errors)
	return out
}
