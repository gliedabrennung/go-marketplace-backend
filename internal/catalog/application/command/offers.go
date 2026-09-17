package command

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

var ErrInvalidOfferStatus = kernel.Validation("CATALOG_INVALID_OFFER_STATUS", "status must be active, paused or archived")

func (b Base) currency(raw string) string {
	if raw == "" {
		return string(b.policy.DefaultCurrency)
	}
	return raw
}

type offerOperation func(o *domain.Offer, seller kernel.SellerID) error

func (b Base) offerAction(ctx context.Context, p auth.Principal, rawOfferID string, requireSelling bool, op offerOperation) error {
	offerID, err := domain.ParseOfferID(rawOfferID)
	if err != nil {
		return domain.ErrOfferNotFound
	}
	var sellerID kernel.SellerID
	err = b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		offer, err := repos.Offers().FindByID(ctx, offerID)
		if err != nil {
			return err
		}
		sellerID = offer.SellerID()
		return nil
	})
	if err != nil {
		return err
	}
	seller, err := b.requireMember(ctx, p, sellerID.String())
	if errors.Is(err, sellerapi.ErrSellerNotFound) {
		return domain.ErrOfferNotFound
	}
	if err != nil {
		return err
	}
	if requireSelling {
		if err := b.requireCanSell(ctx, seller); err != nil {
			return err
		}
	}
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		offer, err := repos.Offers().FindByID(ctx, offerID)
		if err != nil {
			return err
		}
		if err := op(offer, seller); err != nil {
			return err
		}
		return repos.Offers().Save(ctx, offer)
	})
}

type CreateOffer struct {
	Actor          auth.Principal
	SellerID       string
	ProductID      string
	SellerSKU      string
	Price          int64
	Currency       string
	Condition      string
	ProcessingDays int
}

type CreateOfferResult struct {
	OfferID string
}

type CreateOfferHandler struct {
	base Base
}

func NewCreateOfferHandler(base Base) *CreateOfferHandler {
	return &CreateOfferHandler{base: base}
}

func (h *CreateOfferHandler) Handle(ctx context.Context, cmd CreateOffer) (CreateOfferResult, error) {
	seller, err := h.base.requireSellingMember(ctx, cmd.Actor, cmd.SellerID)
	if err != nil {
		return CreateOfferResult{}, err
	}
	productID, err := parseProductID(cmd.ProductID)
	if err != nil {
		return CreateOfferResult{}, err
	}
	sku, err := domain.NewSellerSKU(cmd.SellerSKU)
	if err != nil {
		return CreateOfferResult{}, err
	}
	terms, err := domain.NewOfferTerms(cmd.Price, h.base.currency(cmd.Currency), cmd.Condition, cmd.ProcessingDays)
	if err != nil {
		return CreateOfferResult{}, err
	}
	var created *domain.Offer
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		product, err := repos.Products().FindByID(ctx, productID)
		if err != nil {
			return err
		}
		offer, err := domain.CreateOffer(domain.NewOfferID(), product, seller, sku, terms, h.base.clock.Now())
		if err != nil {
			return err
		}
		created = offer
		return repos.Offers().Save(ctx, offer)
	})
	if err != nil {
		return CreateOfferResult{}, err
	}
	return CreateOfferResult{OfferID: created.ID().String()}, nil
}

type UpdateOfferTerms struct {
	Actor          auth.Principal
	OfferID        string
	Price          int64
	Currency       string
	Condition      string
	ProcessingDays int
}

type UpdateOfferTermsHandler struct {
	base Base
}

func NewUpdateOfferTermsHandler(base Base) *UpdateOfferTermsHandler {
	return &UpdateOfferTermsHandler{base: base}
}

func (h *UpdateOfferTermsHandler) Handle(ctx context.Context, cmd UpdateOfferTerms) (struct{}, error) {
	terms, err := domain.NewOfferTerms(cmd.Price, h.base.currency(cmd.Currency), cmd.Condition, cmd.ProcessingDays)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.offerAction(ctx, cmd.Actor, cmd.OfferID, false, func(o *domain.Offer, seller kernel.SellerID) error {
		return o.ChangeTerms(seller, terms, h.base.clock.Now())
	})
}

type SetOfferStatus struct {
	Actor   auth.Principal
	OfferID string
	Status  string
}

type SetOfferStatusHandler struct {
	base Base
}

func NewSetOfferStatusHandler(base Base) *SetOfferStatusHandler {
	return &SetOfferStatusHandler{base: base}
}

func (h *SetOfferStatusHandler) Handle(ctx context.Context, cmd SetOfferStatus) (struct{}, error) {
	now := h.base.clock.Now()
	var op offerOperation
	switch domain.OfferStatus(cmd.Status) {
	case domain.OfferActive:
		op = func(o *domain.Offer, seller kernel.SellerID) error { return o.Activate(seller, now) }
	case domain.OfferPaused:
		op = func(o *domain.Offer, seller kernel.SellerID) error { return o.Pause(seller, now) }
	case domain.OfferArchived:
		op = func(o *domain.Offer, seller kernel.SellerID) error { return o.Archive(seller, now) }
	default:
		return struct{}{}, ErrInvalidOfferStatus.WithDetail("%q", cmd.Status)
	}
	return struct{}{}, h.base.offerAction(ctx, cmd.Actor, cmd.OfferID, domain.OfferStatus(cmd.Status) == domain.OfferActive, op)
}
