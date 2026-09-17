package memory

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

func (s *Store) CategoryTree(_ context.Context) ([]query.CategoryNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]query.CategoryNode, 0, len(s.categories))
	for _, c := range s.categories {
		out = append(out, query.NewCategoryNode(c))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) Category(_ context.Context, categoryID string) (query.CategoryView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chain, err := s.chain(categoryID)
	if err != nil {
		return query.CategoryView{}, err
	}
	return query.NewCategoryView(chain), nil
}

func (s *Store) Product(_ context.Context, productID string) (query.ProductView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.products[productID]
	if !ok {
		return query.ProductView{}, domain.ErrProductNotFound
	}
	chain, err := s.chain(snap.CategoryID)
	if err != nil {
		return query.ProductView{}, err
	}
	return query.NewProductView(cloneProduct(snap), query.NewCategoryView(chain)), nil
}

func (s *Store) ProductVariants(_ context.Context, productID string, publishedOnly bool) (*query.VariantView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.groups {
		if !slices.ContainsFunc(g.Members, func(m domain.VariantMemberSnapshot) bool { return m.ProductID == productID }) {
			continue
		}
		return query.NewVariantView(cloneGroup(g), func(id string) bool {
			return !publishedOnly || s.products[id].Status == string(domain.ProductStatusPublished)
		}), nil
	}
	return nil, nil
}

func (s *Store) ProductOffers(_ context.Context, productID, status string) ([]query.OfferView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []query.OfferView{}
	for _, o := range s.offers {
		if o.ProductID == productID && (status == "" || o.Status == status) {
			out = append(out, query.NewOfferView(o))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) SellerProducts(_ context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[query.ProductSummary], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []query.ProductSummary
	for _, p := range s.products {
		if p.SellerID == sellerID && (status == "" || p.Status == status) {
			rows = append(rows, query.NewProductSummary(p))
		}
	}
	return newestFirst(rows, limit, after, func(v query.ProductSummary) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (s *Store) ModerationQueue(_ context.Context, limit int, after *pagination.Keyset) (pagination.Page[query.ProductSummary], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []query.ProductSummary
	for _, p := range s.products {
		if p.Status == string(domain.ProductStatusOnModeration) {
			rows = append(rows, query.NewProductSummary(p))
		}
	}
	keyOf := func(v query.ProductSummary) pagination.Keyset { return pagination.Keyset{At: v.UpdatedAt, ID: v.ID} }
	sort.Slice(rows, func(i, j int) bool { return less(keyOf(rows[i]), keyOf(rows[j])) })
	if after != nil {
		rows = slices.DeleteFunc(rows, func(v query.ProductSummary) bool { return !less(*after, keyOf(v)) })
	}
	return pagination.Build(truncate(rows, limit), limit, keyOf), nil
}

func (s *Store) SellerOffers(_ context.Context, sellerID, status string, limit int, after *pagination.Keyset) (pagination.Page[query.OfferView], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []query.OfferView
	for _, o := range s.offers {
		if o.SellerID == sellerID && (status == "" || o.Status == status) {
			rows = append(rows, query.NewOfferView(o))
		}
	}
	return newestFirst(rows, limit, after, func(v query.OfferView) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (s *Store) ImportJob(_ context.Context, jobID string) (query.ImportJobView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.imports[jobID]
	if !ok {
		return query.ImportJobView{}, domain.ErrImportJobNotFound
	}
	return query.NewImportJobView(cloneImport(snap), true), nil
}

func (s *Store) SellerImports(_ context.Context, sellerID string, limit int, after *pagination.Keyset) (pagination.Page[query.ImportJobView], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []query.ImportJobView
	for _, j := range s.imports {
		if j.SellerID == sellerID {
			rows = append(rows, query.NewImportJobView(j, false))
		}
	}
	return newestFirst(rows, limit, after, func(v query.ImportJobView) pagination.Keyset {
		return pagination.Keyset{At: v.CreatedAt, ID: v.ID}
	}), nil
}

func (s *Store) StaleImportIDs(_ context.Context, before time.Time, limit int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var jobs []domain.ImportJobSnapshot
	for _, j := range s.imports {
		if j.Status == string(domain.ImportProcessing) && j.UpdatedAt.Before(before) {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].UpdatedAt.Before(jobs[j].UpdatedAt) })
	ids := make([]string, 0, len(jobs))
	for _, j := range truncate(jobs, limit-1) {
		ids = append(ids, j.ID)
	}
	return ids, nil
}

func less(a, b pagination.Keyset) bool {
	if a.At.Equal(b.At) {
		return a.ID < b.ID
	}
	return a.At.Before(b.At)
}

func truncate[T any](rows []T, limit int) []T {
	if len(rows) > limit+1 {
		return rows[:limit+1]
	}
	return rows
}

func newestFirst[T any](rows []T, limit int, after *pagination.Keyset, keyOf func(T) pagination.Keyset) pagination.Page[T] {
	sort.Slice(rows, func(i, j int) bool { return less(keyOf(rows[j]), keyOf(rows[i])) })
	if after != nil {
		rows = slices.DeleteFunc(rows, func(v T) bool { return !less(keyOf(v), *after) })
	}
	return pagination.Build(truncate(rows, limit), limit, keyOf)
}
