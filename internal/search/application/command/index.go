package command

import (
	"context"

	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
)

type Base struct {
	index  application.Index
	clock  application.Clock
	policy application.Policy
}

func NewBase(index application.Index, clock application.Clock, policy application.Policy) Base {
	return Base{index: index, clock: clock, policy: policy}
}

type IndexProduct struct {
	Document catalogapi.ProductDocument
}

type IndexProductHandler struct {
	base Base
}

func NewIndexProductHandler(base Base) *IndexProductHandler {
	return &IndexProductHandler{base: base}
}

func (h *IndexProductHandler) Handle(ctx context.Context, cmd IndexProduct) (struct{}, error) {
	table, err := h.base.index.ActiveTable(ctx)
	if err != nil {
		return struct{}{}, err
	}
	now := h.base.clock.Now()
	document := application.NewDocument(cmd.Document)
	if err := h.base.index.SaveDocuments(ctx, table, []application.Document{document}, now); err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.index.RefreshProduct(ctx, document.ProductID, now)
}

type SetProductCover struct {
	ProductID string
	CoverKey  string
}

type SetProductCoverHandler struct {
	base Base
}

func NewSetProductCoverHandler(base Base) *SetProductCoverHandler {
	return &SetProductCoverHandler{base: base}
}

func (h *SetProductCoverHandler) Handle(ctx context.Context, cmd SetProductCover) (struct{}, error) {
	table, err := h.base.index.ActiveTable(ctx)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.index.SetCover(ctx, table, cmd.ProductID, cmd.CoverKey, h.base.clock.Now())
}

type IndexOffer struct {
	Offer application.OfferState
}

type IndexOfferHandler struct {
	base Base
}

func NewIndexOfferHandler(base Base) *IndexOfferHandler {
	return &IndexOfferHandler{base: base}
}

func (h *IndexOfferHandler) Handle(ctx context.Context, cmd IndexOffer) (struct{}, error) {
	if err := h.base.index.SaveOffer(ctx, cmd.Offer); err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.index.RefreshProduct(ctx, cmd.Offer.ProductID, h.base.clock.Now())
}

type IndexSeller struct {
	SellerID string
	CanSell  bool
}

type IndexSellerHandler struct {
	base Base
}

func NewIndexSellerHandler(base Base) *IndexSellerHandler {
	return &IndexSellerHandler{base: base}
}

func (h *IndexSellerHandler) Handle(ctx context.Context, cmd IndexSeller) (struct{}, error) {
	now := h.base.clock.Now()
	state := application.SellerState{SellerID: cmd.SellerID, CanSell: cmd.CanSell}
	if err := h.base.index.SaveSeller(ctx, state, now); err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.index.RefreshSeller(ctx, cmd.SellerID, now)
}

type IndexStock struct {
	SKU       string
	Available int
}

type IndexStockHandler struct {
	base Base
}

func NewIndexStockHandler(base Base) *IndexStockHandler {
	return &IndexStockHandler{base: base}
}

func (h *IndexStockHandler) Handle(ctx context.Context, cmd IndexStock) (struct{}, error) {
	now := h.base.clock.Now()
	productID, err := h.base.index.SaveStock(ctx, cmd.SKU, cmd.Available, now)
	if err != nil || productID == "" {
		return struct{}{}, err
	}
	return struct{}{}, h.base.index.RefreshProduct(ctx, productID, now)
}

type RefreshLexicon struct{}

type RefreshLexiconHandler struct {
	base Base
}

func NewRefreshLexiconHandler(base Base) *RefreshLexiconHandler {
	return &RefreshLexiconHandler{base: base}
}

func (h *RefreshLexiconHandler) Handle(ctx context.Context, _ RefreshLexicon) (int, error) {
	return h.base.index.RefreshLexicon(ctx, h.base.policy.LexiconMinWeight)
}
