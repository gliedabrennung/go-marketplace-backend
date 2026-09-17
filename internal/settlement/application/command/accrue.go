package command

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Base struct {
	uow    application.UnitOfWork
	clock  application.Clock
	rates  application.CommissionRates
	policy application.Policy
}

func NewBase(uow application.UnitOfWork, clock application.Clock, rates application.CommissionRates, policy application.Policy) Base {
	return Base{uow: uow, clock: clock, rates: rates, policy: policy}
}

type AccrueItem struct {
	SellerID   string
	CategoryID string
	Amount     int64
}

type AccrueSettlement struct {
	OrderID  string
	Currency string
	Items    []AccrueItem
}

type AccrueSettlementHandler struct {
	base Base
}

func NewAccrueSettlementHandler(base Base) *AccrueSettlementHandler {
	return &AccrueSettlementHandler{base: base}
}

type sellerTotals struct {
	seller     kernel.SellerID
	gross      int64
	commission int64
}

func (h *AccrueSettlementHandler) Handle(ctx context.Context, cmd AccrueSettlement) (int, error) {
	currency, err := kernel.NewCurrency(cmd.Currency)
	if err != nil {
		return 0, err
	}
	bySeller := map[string]*sellerTotals{}
	order := []string{}
	for _, item := range cmd.Items {
		seller, err := kernel.ParseSellerID(item.SellerID)
		if err != nil {
			return 0, err
		}
		totals, ok := bySeller[item.SellerID]
		if !ok {
			totals = &sellerTotals{seller: seller}
			bySeller[item.SellerID] = totals
			order = append(order, item.SellerID)
		}
		rate, err := h.base.rates.CommissionRate(ctx, item.SellerID, item.CategoryID)
		if err != nil {
			rate = h.base.policy.DefaultCommissionRate
		}
		bp := kernel.MustBasisPoints(rate)
		totals.gross += item.Amount
		totals.commission += kernel.MustMoney(item.Amount, currency).ApplyPercent(bp).Amount()
	}

	accrued := 0
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		for _, sellerID := range order {
			totals := bySeller[sellerID]
			existing, err := repos.Entries().FindByOrderAndSeller(ctx, cmd.OrderID, sellerID)
			if err != nil && !errors.Is(err, domain.ErrEntryNotFound) {
				return err
			}
			if existing != nil {
				continue
			}
			entry, err := domain.Accrue(domain.NewEntryID(), cmd.OrderID, totals.seller,
				kernel.MustMoney(totals.gross, currency), kernel.MustMoney(totals.commission, currency), h.base.clock.Now())
			if err != nil {
				return err
			}
			if err := repos.Entries().Save(ctx, entry); err != nil {
				if errors.Is(err, domain.ErrAlreadyAccrued) {
					continue
				}
				return err
			}
			accrued++
		}
		return nil
	})
	return accrued, err
}
