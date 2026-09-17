package command

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
)

type ApplyPerformance struct {
	SellerID          string
	Orders            int
	CancelledBySeller int
	LateShipments     int
	Reviews           int
	ReviewScoreSum    int
}

type ApplyPerformanceResult struct {
	Score       int
	Provisional bool
	Suspended   bool
}

type ApplyPerformanceHandler struct {
	unit   Unit
	policy domain.RatingPolicy
}

func NewApplyPerformanceHandler(unit Unit, policy domain.RatingPolicy) *ApplyPerformanceHandler {
	return &ApplyPerformanceHandler{unit: unit, policy: policy}
}

func (h *ApplyPerformanceHandler) Handle(ctx context.Context, cmd ApplyPerformance) (ApplyPerformanceResult, error) {
	id, err := parseSellerID(cmd.SellerID)
	if err != nil {
		return ApplyPerformanceResult{}, err
	}
	metrics := domain.PerformanceMetrics{
		Orders:            cmd.Orders,
		CancelledBySeller: cmd.CancelledBySeller,
		LateShipments:     cmd.LateShipments,
		Reviews:           cmd.Reviews,
		ReviewScoreSum:    cmd.ReviewScoreSum,
	}
	now := h.unit.clock.Now()

	var result ApplyPerformanceResult
	err = h.unit.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		seller, err := repos.Sellers().FindByID(ctx, id)
		if err != nil {
			return err
		}
		wasActive := seller.Status() == domain.StatusActive
		rating, err := seller.ApplyPerformance(metrics, h.policy, now)
		if err != nil {
			return err
		}
		result = ApplyPerformanceResult{
			Score:       rating.Score(),
			Provisional: rating.Provisional(),
			Suspended:   wasActive && seller.Status() == domain.StatusSuspended,
		}
		return repos.Sellers().Save(ctx, seller)
	})
	return result, err
}
