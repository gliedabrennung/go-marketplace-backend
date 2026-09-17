package memory

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type redemption struct {
	order    string
	customer string
}

type Store struct {
	mu          sync.Mutex
	promotions  map[string]domain.PromotionSnapshot
	codes       map[string]domain.PromoCodeSnapshot
	redemptions map[string][]redemption
	prices      map[string]domain.OfferPriceSnapshot
	categories  map[string][]string
	events      []kernel.DomainEvent
}

func NewStore() *Store {
	return &Store{
		promotions:  make(map[string]domain.PromotionSnapshot),
		codes:       make(map[string]domain.PromoCodeSnapshot),
		redemptions: make(map[string][]redemption),
		prices:      make(map[string]domain.OfferPriceSnapshot),
		categories:  make(map[string][]string),
	}
}

func (s *Store) Promotions() domain.PromotionRepository { return promotionRepository{s} }

func (s *Store) PromoCodes() domain.PromoCodeRepository { return promoCodeRepository{s} }

func (s *Store) OfferPrices() domain.OfferPriceRepository { return offerPriceRepository{s} }

func (s *Store) Categories() application.CategoryIndex { return categoryIndex{s} }

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

type promotionRepository struct{ s *Store }

func (r promotionRepository) FindByID(_ context.Context, id domain.PromotionID) (*domain.Promotion, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.promotions[id.String()]
	if !ok {
		return nil, domain.ErrPromotionNotFound
	}
	return domain.RehydratePromotion(clonePromotion(snap))
}

func (r promotionRepository) Save(_ context.Context, promotion *domain.Promotion) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := clonePromotion(promotion.Snapshot())
	current, exists := r.s.promotions[next.ID]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.promotions[next.ID] = next
	promotion.AdvanceVersion()
	r.s.events = append(r.s.events, promotion.PullEvents()...)
	return nil
}

type promoCodeRepository struct{ s *Store }

func (r promoCodeRepository) FindByCode(_ context.Context, code domain.Code) (*domain.PromoCode, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.codes[code.String()]
	if !ok {
		return nil, domain.ErrPromoCodeNotFound
	}
	return domain.RehydratePromoCode(snap)
}

func (r promoCodeRepository) CustomerUsage(_ context.Context, code domain.Code, customer kernel.UserID) (int, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.usage(code.String(), customer.String()), nil
}

func (r promoCodeRepository) Redeemed(_ context.Context, code domain.Code, order domain.OrderID) (bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return slices.ContainsFunc(r.s.redemptions[code.String()], func(red redemption) bool {
		return red.order == order.String()
	}), nil
}

func (r promoCodeRepository) Save(_ context.Context, promo *domain.PromoCode) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := promo.Snapshot()
	current, exists := r.s.codes[next.Code]
	if next.Version == 0 && exists {
		return domain.ErrPromoCodeExists
	}
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	records := slices.Clone(r.s.redemptions[next.Code])
	for _, redeemed := range promo.PullRedemptions() {
		if slices.ContainsFunc(records, func(red redemption) bool { return red.order == redeemed.OrderID.String() }) {
			return domain.ErrPromoAlreadyUsed
		}
		records = append(records, redemption{order: redeemed.OrderID.String(), customer: redeemed.CustomerID.String()})
	}
	for _, released := range promo.PullReleases() {
		records = slices.DeleteFunc(records, func(red redemption) bool { return red.order == released.String() })
	}
	next.Version++
	r.s.codes[next.Code] = next
	r.s.redemptions[next.Code] = records
	promo.AdvanceVersion()
	r.s.events = append(r.s.events, promo.PullEvents()...)
	return nil
}

type offerPriceRepository struct{ s *Store }

func (r offerPriceRepository) FindBySKU(_ context.Context, sku domain.SKU) (*domain.OfferPrice, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.prices[sku.String()]
	if !ok {
		return nil, domain.ErrOfferPriceNotFound
	}
	return domain.RehydrateOfferPrice(snap)
}

func (r offerPriceRepository) Save(_ context.Context, price *domain.OfferPrice) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := price.Snapshot()
	current, exists := r.s.prices[next.SKU]
	if stale(exists, current.Version, next.Version) {
		return kernel.ErrConcurrentModification
	}
	next.Version++
	r.s.prices[next.SKU] = next
	price.AdvanceVersion()
	r.s.events = append(r.s.events, price.PullEvents()...)
	return nil
}

type categoryIndex struct{ s *Store }

func (c categoryIndex) Save(_ context.Context, productID string, path []string, _ time.Time) error {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.s.categories[productID] = slices.Clone(path)
	return nil
}

func (c categoryIndex) Paths(_ context.Context, productIDs []string) (map[string][]string, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	return c.s.paths(productIDs), nil
}

func (s *Store) paths(productIDs []string) map[string][]string {
	out := make(map[string][]string, len(productIDs))
	for _, id := range productIDs {
		if path, ok := s.categories[id]; ok {
			out[id] = slices.Clone(path)
		}
	}
	return out
}

func (s *Store) usage(code, customer string) int {
	count := 0
	for _, red := range s.redemptions[code] {
		if red.customer == customer {
			count++
		}
	}
	return count
}

func clonePromotion(snap domain.PromotionSnapshot) domain.PromotionSnapshot {
	out := snap
	out.SKUs = slices.Clone(snap.SKUs)
	out.Sellers = slices.Clone(snap.Sellers)
	out.Categories = slices.Clone(snap.Categories)
	return out
}

type ReadModel struct {
	s *Store
}

func NewReadModel(store *Store) *ReadModel {
	return &ReadModel{s: store}
}

func (m *ReadModel) Prices(_ context.Context, skus []string) ([]*domain.OfferPrice, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	out := make([]*domain.OfferPrice, 0, len(skus))
	for _, sku := range skus {
		snap, ok := m.s.prices[sku]
		if !ok {
			continue
		}
		price, err := domain.RehydrateOfferPrice(snap)
		if err != nil {
			return nil, err
		}
		out = append(out, price)
	}
	return out, nil
}

func (m *ReadModel) CategoryPaths(_ context.Context, productIDs []string) (map[string][]string, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	return m.s.paths(productIDs), nil
}

func (m *ReadModel) RunningPromotions(_ context.Context, at time.Time) ([]*domain.Promotion, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	var out []*domain.Promotion
	for _, snap := range m.s.promotions {
		promotion, err := domain.RehydratePromotion(clonePromotion(snap))
		if err != nil {
			return nil, err
		}
		if promotion.IsRunning(at) {
			out = append(out, promotion)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID().String() < out[j].ID().String() })
	return out, nil
}

func (m *ReadModel) FindPromoCode(ctx context.Context, code domain.Code) (*domain.PromoCode, error) {
	return promoCodeRepository(*m).FindByCode(ctx, code)
}

func (m *ReadModel) PromoCodeUsage(ctx context.Context, code domain.Code, customer kernel.UserID) (int, error) {
	return promoCodeRepository(*m).CustomerUsage(ctx, code, customer)
}

func (m *ReadModel) Promotion(_ context.Context, promotionID string) (query.PromotionView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	snap, ok := m.s.promotions[promotionID]
	if !ok {
		return query.PromotionView{}, domain.ErrPromotionNotFound
	}
	return query.NewPromotionView(snap), nil
}

func (m *ReadModel) Promotions(_ context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[query.PromotionView], error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	var rows []query.PromotionView
	for _, snap := range m.s.promotions {
		if status != "" && snap.Status != status {
			continue
		}
		if after != nil && !snap.CreatedAt.Before(after.At) && (!snap.CreatedAt.Equal(after.At) || snap.ID >= after.ID) {
			continue
		}
		rows = append(rows, query.NewPromotionView(snap))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	if len(rows) > limit+1 {
		rows = rows[:limit+1]
	}
	return pagination.Build(rows, limit, func(v query.PromotionView) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (m *ReadModel) PromoCode(_ context.Context, code string) (query.PromoCodeView, error) {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	snap, ok := m.s.codes[code]
	if !ok {
		return query.PromoCodeView{}, domain.ErrPromoCodeNotFound
	}
	return query.NewPromoCodeView(snap), nil
}
