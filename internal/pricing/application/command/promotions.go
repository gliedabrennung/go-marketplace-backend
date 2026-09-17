package command

import (
	"context"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

var ErrInvalidPromotionStatus = kernel.Validation("PRICING_INVALID_PROMOTION_STATUS", "status must be active, paused or ended")

type PromotionInput struct {
	Name      string
	Discount  DiscountInput
	Target    TargetInput
	Priority  int
	Exclusive bool
	StartsAt  time.Time
	EndsAt    time.Time
}

func (b Base) promotionSpec(input PromotionInput) (domain.PromotionSpec, error) {
	scope, err := target(input.Target)
	if err != nil {
		return domain.PromotionSpec{}, err
	}
	return domain.PromotionSpec{
		Name: input.Name, Discount: b.discount(input.Discount), Target: scope,
		Priority: input.Priority, Exclusive: input.Exclusive, StartsAt: input.StartsAt, EndsAt: input.EndsAt,
	}, nil
}

type CreatePromotion struct {
	Actor     auth.Principal
	Promotion PromotionInput
}

type CreatePromotionResult struct {
	PromotionID string
}

type CreatePromotionHandler struct {
	base Base
}

func NewCreatePromotionHandler(base Base) *CreatePromotionHandler {
	return &CreatePromotionHandler{base: base}
}

func (h *CreatePromotionHandler) Handle(ctx context.Context, cmd CreatePromotion) (CreatePromotionResult, error) {
	if err := requireManager(cmd.Actor); err != nil {
		return CreatePromotionResult{}, err
	}
	spec, err := h.base.promotionSpec(cmd.Promotion)
	if err != nil {
		return CreatePromotionResult{}, err
	}
	promotion, err := domain.CreatePromotion(domain.NewPromotionID(), spec, h.base.clock.Now())
	if err != nil {
		return CreatePromotionResult{}, err
	}
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		return repos.Promotions().Save(ctx, promotion)
	})
	if err != nil {
		return CreatePromotionResult{}, err
	}
	return CreatePromotionResult{PromotionID: promotion.ID().String()}, nil
}

type UpdatePromotion struct {
	Actor       auth.Principal
	PromotionID string
	Promotion   PromotionInput
}

type UpdatePromotionHandler struct {
	base Base
}

func NewUpdatePromotionHandler(base Base) *UpdatePromotionHandler {
	return &UpdatePromotionHandler{base: base}
}

func (h *UpdatePromotionHandler) Handle(ctx context.Context, cmd UpdatePromotion) (struct{}, error) {
	spec, err := h.base.promotionSpec(cmd.Promotion)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.promotionAction(ctx, cmd.Actor, cmd.PromotionID, func(p *domain.Promotion, now time.Time) error {
		return p.Update(spec, now)
	})
}

type SetPromotionStatus struct {
	Actor       auth.Principal
	PromotionID string
	Status      string
}

type SetPromotionStatusHandler struct {
	base Base
}

func NewSetPromotionStatusHandler(base Base) *SetPromotionStatusHandler {
	return &SetPromotionStatusHandler{base: base}
}

func (h *SetPromotionStatusHandler) Handle(ctx context.Context, cmd SetPromotionStatus) (struct{}, error) {
	var op promotionOperation
	switch domain.PromotionStatus(cmd.Status) {
	case domain.PromotionActive:
		op = (*domain.Promotion).Activate
	case domain.PromotionPaused:
		op = (*domain.Promotion).Pause
	case domain.PromotionEnded:
		op = (*domain.Promotion).End
	default:
		return struct{}{}, ErrInvalidPromotionStatus.WithDetail("%q", cmd.Status)
	}
	return struct{}{}, h.base.promotionAction(ctx, cmd.Actor, cmd.PromotionID, op)
}
