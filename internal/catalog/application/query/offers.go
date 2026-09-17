package query

import (
	"context"
	"errors"
	"sort"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type ListProductOffers struct {
	ProductID string
}

type ListProductOffersHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
}

func NewListProductOffersHandler(reader ReadModel, sellers application.SellerDirectory) *ListProductOffersHandler {
	return &ListProductOffersHandler{reader: reader, sellers: sellers}
}

func (h *ListProductOffersHandler) Handle(ctx context.Context, q ListProductOffers) ([]OfferView, error) {
	if _, err := domain.ParseProductID(q.ProductID); err != nil {
		return nil, domain.ErrProductNotFound
	}
	product, err := h.reader.Product(ctx, q.ProductID)
	if err != nil {
		return nil, err
	}
	if product.Status != string(domain.ProductStatusPublished) {
		return nil, domain.ErrProductNotFound
	}
	offers, err := h.reader.ProductOffers(ctx, q.ProductID, string(domain.OfferActive))
	if err != nil {
		return nil, err
	}
	canSell := map[string]bool{}
	out := make([]OfferView, 0, len(offers))
	for _, offer := range offers {
		allowed, seen := canSell[offer.SellerID]
		if !seen {
			info, err := h.sellers.Seller(ctx, offer.SellerID)
			if err != nil && !errors.Is(err, sellerapi.ErrSellerNotFound) {
				return nil, err
			}
			allowed = err == nil && info.CanSell
			canSell[offer.SellerID] = allowed
		}
		if allowed {
			out = append(out, offer)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Price < out[j].Price })
	return out, nil
}

type ListSellerOffers struct {
	Actor    auth.Principal
	SellerID string
	Status   string
	Limit    int
	Cursor   string
}

type ListSellerOffersHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
}

func NewListSellerOffersHandler(reader ReadModel, sellers application.SellerDirectory) *ListSellerOffersHandler {
	return &ListSellerOffersHandler{reader: reader, sellers: sellers}
}

func (h *ListSellerOffersHandler) Handle(ctx context.Context, q ListSellerOffers) (pagination.Page[OfferView], error) {
	if err := requireMember(ctx, h.sellers, q.Actor, q.SellerID); err != nil {
		return pagination.Page[OfferView]{}, err
	}
	if err := validStatus(q.Status, domain.OfferActive, domain.OfferPaused, domain.OfferArchived); err != nil {
		return pagination.Page[OfferView]{}, err
	}
	after, err := keyset(q.Cursor)
	if err != nil {
		return pagination.Page[OfferView]{}, err
	}
	return h.reader.SellerOffers(ctx, q.SellerID, q.Status, pagination.NormalizeLimit(q.Limit), after)
}
