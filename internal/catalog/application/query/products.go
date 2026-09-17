package query

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type GetProduct struct {
	Actor     auth.Principal
	ProductID string
}

type GetProductHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
	signer  URLSigner
}

func NewGetProductHandler(reader ReadModel, sellers application.SellerDirectory, signer URLSigner) *GetProductHandler {
	return &GetProductHandler{reader: reader, sellers: sellers, signer: signer}
}

func (h *GetProductHandler) Handle(ctx context.Context, q GetProduct) (ProductView, error) {
	if _, err := domain.ParseProductID(q.ProductID); err != nil {
		return ProductView{}, domain.ErrProductNotFound
	}
	view, err := h.reader.Product(ctx, q.ProductID)
	if err != nil {
		return ProductView{}, err
	}
	privileged := identity.Can(q.Actor, identity.PermCatalogModerate)
	if !privileged {
		if privileged, err = isMember(ctx, h.sellers, q.Actor, view.SellerID); err != nil {
			return ProductView{}, err
		}
	}
	if !privileged && view.Status != string(domain.ProductStatusPublished) {
		return ProductView{}, domain.ErrProductNotFound
	}
	if view.Images, err = h.sign(ctx, view, privileged); err != nil {
		return ProductView{}, err
	}
	if view.Variants, err = h.reader.ProductVariants(ctx, view.ID, !privileged); err != nil {
		return ProductView{}, err
	}
	return view, nil
}

func (h *GetProductHandler) sign(ctx context.Context, view ProductView, privileged bool) ([]ImageView, error) {
	out := make([]ImageView, 0, len(view.Images))
	for _, img := range view.Images {
		processed := img.Status == string(domain.ImageProcessed)
		if !processed && !privileged {
			continue
		}
		urls := map[string]*string{}
		if processed {
			urls[domain.ImageVariantSmall] = &img.SmallURL
			urls[domain.ImageVariantLarge] = &img.LargeURL
		}
		if privileged && img.Status != string(domain.ImageAwaitingUpload) {
			urls[domain.ImageVariantOriginal] = &img.OriginalURL
		}
		for variant, target := range urls {
			key, ok := imageKey(view.ID, img.ID, variant)
			if !ok {
				continue
			}
			url, err := h.signer.SignDownload(ctx, key)
			if err != nil {
				return nil, err
			}
			*target = url
		}
		out = append(out, img)
	}
	return out, nil
}

type ListSellerProducts struct {
	Actor    auth.Principal
	SellerID string
	Status   string
	Limit    int
	Cursor   string
}

type ListSellerProductsHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
}

func NewListSellerProductsHandler(reader ReadModel, sellers application.SellerDirectory) *ListSellerProductsHandler {
	return &ListSellerProductsHandler{reader: reader, sellers: sellers}
}

func (h *ListSellerProductsHandler) Handle(ctx context.Context, q ListSellerProducts) (pagination.Page[ProductSummary], error) {
	if err := requireMember(ctx, h.sellers, q.Actor, q.SellerID); err != nil {
		return pagination.Page[ProductSummary]{}, err
	}
	if err := validStatus(q.Status, domain.ProductStatusDraft, domain.ProductStatusOnModeration,
		domain.ProductStatusPublished, domain.ProductStatusRejected); err != nil {
		return pagination.Page[ProductSummary]{}, err
	}
	after, err := keyset(q.Cursor)
	if err != nil {
		return pagination.Page[ProductSummary]{}, err
	}
	return h.reader.SellerProducts(ctx, q.SellerID, q.Status, pagination.NormalizeLimit(q.Limit), after)
}

type ListModerationQueue struct {
	Actor  auth.Principal
	Limit  int
	Cursor string
}

type ListModerationQueueHandler struct {
	reader ReadModel
}

func NewListModerationQueueHandler(reader ReadModel) *ListModerationQueueHandler {
	return &ListModerationQueueHandler{reader: reader}
}

func (h *ListModerationQueueHandler) Handle(ctx context.Context, q ListModerationQueue) (pagination.Page[ProductSummary], error) {
	if err := identity.Authorize(q.Actor, identity.PermCatalogModerate); err != nil {
		return pagination.Page[ProductSummary]{}, err
	}
	after, err := keyset(q.Cursor)
	if err != nil {
		return pagination.Page[ProductSummary]{}, err
	}
	return h.reader.ModerationQueue(ctx, pagination.NormalizeLimit(q.Limit), after)
}
