package query

import (
	"context"
	"errors"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/inventory/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type StockView struct {
	SKU       string
	SellerID  string
	Available int
	Reserved  int
	UpdatedAt time.Time
}

type MovementView struct {
	SKU         string
	Delta       int
	Reason      string
	ReferenceID string
	OccurredAt  time.Time
}

type ReservationLineView struct {
	SKU      string
	Quantity int
	Status   string
}

type ReservationView struct {
	ReservationID string
	OrderID       string
	Status        string
	ExpiresAt     time.Time
	Lines         []ReservationLineView
}

type ReadModel interface {
	Stock(ctx context.Context, sku string) (StockView, error)
	SellerStock(ctx context.Context, sellerID string, limit int, after *pagination.Keyset) (pagination.Page[StockView], error)
	Movements(ctx context.Context, sku string, limit int) ([]MovementView, error)
	Reservation(ctx context.Context, reservationID string) (ReservationView, error)
	Available(ctx context.Context, skus []string) (map[string]int, error)
}

func member(ctx context.Context, sellers application.SellerDirectory, p auth.Principal, sellerID string) error {
	if p.UserID == "" {
		return auth.ErrUnauthenticated
	}
	if _, err := kernel.ParseSellerID(sellerID); err != nil {
		return sellerapi.ErrSellerNotFound
	}
	_, ok, err := sellers.MemberRole(ctx, sellerID, p.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return sellerapi.ErrSellerNotFound
	}
	return nil
}

type GetStock struct {
	Actor auth.Principal
	SKU   string
}

type GetStockHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
}

func NewGetStockHandler(reader ReadModel, sellers application.SellerDirectory) *GetStockHandler {
	return &GetStockHandler{reader: reader, sellers: sellers}
}

func (h *GetStockHandler) Handle(ctx context.Context, q GetStock) (StockView, error) {
	if _, err := domain.NewSKU(q.SKU); err != nil {
		return StockView{}, domain.ErrStockNotFound
	}
	view, err := h.reader.Stock(ctx, q.SKU)
	if err != nil {
		return StockView{}, err
	}
	if err := member(ctx, h.sellers, q.Actor, view.SellerID); err != nil {
		if errors.Is(err, sellerapi.ErrSellerNotFound) {
			return StockView{}, domain.ErrStockNotFound
		}
		return StockView{}, err
	}
	return view, nil
}

type ListSellerStock struct {
	Actor    auth.Principal
	SellerID string
	Limit    int
	Cursor   string
}

type ListSellerStockHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
}

func NewListSellerStockHandler(reader ReadModel, sellers application.SellerDirectory) *ListSellerStockHandler {
	return &ListSellerStockHandler{reader: reader, sellers: sellers}
}

func (h *ListSellerStockHandler) Handle(ctx context.Context, q ListSellerStock) (pagination.Page[StockView], error) {
	if err := member(ctx, h.sellers, q.Actor, q.SellerID); err != nil {
		return pagination.Page[StockView]{}, err
	}
	keyset, ok, err := pagination.DecodeKeyset(q.Cursor)
	if err != nil {
		return pagination.Page[StockView]{}, err
	}
	var after *pagination.Keyset
	if ok {
		after = &keyset
	}
	return h.reader.SellerStock(ctx, q.SellerID, pagination.NormalizeLimit(q.Limit), after)
}

type ListMovements struct {
	Actor auth.Principal
	SKU   string
	Limit int
}

type ListMovementsHandler struct {
	reader  ReadModel
	sellers application.SellerDirectory
}

func NewListMovementsHandler(reader ReadModel, sellers application.SellerDirectory) *ListMovementsHandler {
	return &ListMovementsHandler{reader: reader, sellers: sellers}
}

func (h *ListMovementsHandler) Handle(ctx context.Context, q ListMovements) ([]MovementView, error) {
	if _, err := domain.NewSKU(q.SKU); err != nil {
		return nil, domain.ErrStockNotFound
	}
	view, err := h.reader.Stock(ctx, q.SKU)
	if err != nil {
		return nil, err
	}
	if err := member(ctx, h.sellers, q.Actor, view.SellerID); err != nil {
		if errors.Is(err, sellerapi.ErrSellerNotFound) {
			return nil, domain.ErrStockNotFound
		}
		return nil, err
	}
	return h.reader.Movements(ctx, q.SKU, pagination.NormalizeLimit(q.Limit))
}

type GetReservation struct {
	ReservationID string
}

type GetReservationHandler struct {
	reader ReadModel
}

func NewGetReservationHandler(reader ReadModel) *GetReservationHandler {
	return &GetReservationHandler{reader: reader}
}

func (h *GetReservationHandler) Handle(ctx context.Context, q GetReservation) (ReservationView, error) {
	if _, err := domain.ParseReservationID(q.ReservationID); err != nil {
		return ReservationView{}, domain.ErrReservationNotFound
	}
	return h.reader.Reservation(ctx, q.ReservationID)
}
