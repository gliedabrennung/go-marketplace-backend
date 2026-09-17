package query

import (
	"context"
	"slices"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

var ErrUnknownStatus = kernel.Validation("PRICING_UNKNOWN_STATUS", "status filter is unknown")

type PromotionView struct {
	ID          string
	Name        string
	Kind        string
	BasisPoints int
	Amount      int64
	Currency    string
	BuyQuantity int
	FreeUnits   int
	SKUs        []string
	Sellers     []string
	Categories  []string
	Priority    int
	Exclusive   bool
	Status      string
	StartsAt    time.Time
	EndsAt      time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PromoCodeView struct {
	Code             string
	Kind             string
	BasisPoints      int
	Amount           int64
	Currency         string
	MinCartAmount    int64
	TotalLimit       int
	PerCustomerLimit int
	Used             int
	Status           string
	StartsAt         time.Time
	EndsAt           time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func NewPromotionView(s domain.PromotionSnapshot) PromotionView {
	return PromotionView{
		ID: s.ID, Name: s.Name, Kind: s.Kind, BasisPoints: s.BasisPoints, Amount: s.Amount, Currency: s.Currency,
		BuyQuantity: s.BuyQuantity, FreeUnits: s.FreeUnits, SKUs: slices.Clone(s.SKUs), Sellers: slices.Clone(s.Sellers),
		Categories: slices.Clone(s.Categories), Priority: s.Priority, Exclusive: s.Exclusive, Status: s.Status,
		StartsAt: s.StartsAt, EndsAt: s.EndsAt, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func NewPromoCodeView(s domain.PromoCodeSnapshot) PromoCodeView {
	return PromoCodeView{
		Code: s.Code, Kind: s.Kind, BasisPoints: s.BasisPoints, Amount: s.Amount, Currency: s.Currency,
		MinCartAmount: s.MinCartAmount, TotalLimit: s.TotalLimit, PerCustomerLimit: s.PerCustomerLimit, Used: s.Used,
		Status: s.Status, StartsAt: s.StartsAt, EndsAt: s.EndsAt, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

type AdminReadModel interface {
	Promotion(ctx context.Context, promotionID string) (PromotionView, error)
	Promotions(ctx context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[PromotionView], error)
	PromoCode(ctx context.Context, code string) (PromoCodeView, error)
}

type GetPromotion struct {
	Actor       auth.Principal
	PromotionID string
}

type GetPromotionHandler struct {
	reader AdminReadModel
}

func NewGetPromotionHandler(reader AdminReadModel) *GetPromotionHandler {
	return &GetPromotionHandler{reader: reader}
}

func (h *GetPromotionHandler) Handle(ctx context.Context, q GetPromotion) (PromotionView, error) {
	if err := identity.Authorize(q.Actor, identity.PermPromotionsManage); err != nil {
		return PromotionView{}, err
	}
	if _, err := domain.ParsePromotionID(q.PromotionID); err != nil {
		return PromotionView{}, domain.ErrPromotionNotFound
	}
	return h.reader.Promotion(ctx, q.PromotionID)
}

type ListPromotions struct {
	Actor  auth.Principal
	Status string
	Limit  int
	Cursor string
}

type ListPromotionsHandler struct {
	reader AdminReadModel
}

func NewListPromotionsHandler(reader AdminReadModel) *ListPromotionsHandler {
	return &ListPromotionsHandler{reader: reader}
}

func (h *ListPromotionsHandler) Handle(ctx context.Context, q ListPromotions) (pagination.Page[PromotionView], error) {
	if err := identity.Authorize(q.Actor, identity.PermPromotionsManage); err != nil {
		return pagination.Page[PromotionView]{}, err
	}
	known := []domain.PromotionStatus{domain.PromotionDraft, domain.PromotionActive, domain.PromotionPaused, domain.PromotionEnded}
	if q.Status != "" && !slices.Contains(known, domain.PromotionStatus(q.Status)) {
		return pagination.Page[PromotionView]{}, ErrUnknownStatus.WithDetail("%q", q.Status)
	}
	keyset, ok, err := pagination.DecodeKeyset(q.Cursor)
	if err != nil {
		return pagination.Page[PromotionView]{}, err
	}
	var after *pagination.Keyset
	if ok {
		after = &keyset
	}
	return h.reader.Promotions(ctx, q.Status, pagination.NormalizeLimit(q.Limit), after)
}

type GetPromoCode struct {
	Actor auth.Principal
	Code  string
}

type GetPromoCodeHandler struct {
	reader AdminReadModel
}

func NewGetPromoCodeHandler(reader AdminReadModel) *GetPromoCodeHandler {
	return &GetPromoCodeHandler{reader: reader}
}

func (h *GetPromoCodeHandler) Handle(ctx context.Context, q GetPromoCode) (PromoCodeView, error) {
	if err := identity.Authorize(q.Actor, identity.PermPromotionsManage); err != nil {
		return PromoCodeView{}, err
	}
	code, err := domain.NewCode(q.Code)
	if err != nil {
		return PromoCodeView{}, domain.ErrPromoCodeNotFound
	}
	return h.reader.PromoCode(ctx, code.String())
}
