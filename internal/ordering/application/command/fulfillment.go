package command

import (
	"context"
	"errors"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

var ErrNotOrderSeller = domain.ErrOrderNotFound

func sellerActor(ctx context.Context, sellers application.SellerMembership, principal auth.Principal, order *domain.Order) (domain.Actor, error) {
	if principal.UserID == "" {
		return domain.Actor{}, auth.ErrUnauthenticated
	}
	if identity.Can(principal, identity.PermOrdersSupport) {
		return domain.Actor{Kind: domain.ActorSupport, ID: principal.UserID}, nil
	}
	for _, seller := range order.SellerIDs() {
		_, member, err := sellers.MemberRole(ctx, seller.String(), principal.UserID)
		if err != nil {
			return domain.Actor{}, err
		}
		if member {
			return domain.Actor{Kind: domain.ActorSeller, ID: principal.UserID}, nil
		}
	}
	return domain.Actor{}, ErrNotOrderSeller
}

type MarkOrderShipped struct {
	Actor   auth.Principal
	OrderID string
}

type MarkOrderShippedHandler struct {
	base    Base
	sellers application.SellerMembership
}

func NewMarkOrderShippedHandler(base Base, sellers application.SellerMembership) *MarkOrderShippedHandler {
	return &MarkOrderShippedHandler{base: base, sellers: sellers}
}

func (h *MarkOrderShippedHandler) Handle(ctx context.Context, cmd MarkOrderShipped) (struct{}, error) {
	id, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return struct{}{}, domain.ErrOrderNotFound
	}
	return struct{}{}, h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		order, err := repos.Orders().FindByID(ctx, id)
		if err != nil {
			return err
		}
		actor, err := sellerActor(ctx, h.sellers, cmd.Actor, order)
		if err != nil {
			return err
		}
		if err := order.MarkShipped(actor, h.base.Clock.Now()); err != nil {
			return err
		}
		return repos.Orders().Save(ctx, order)
	})
}

type MarkOrderDelivered struct {
	Actor   auth.Principal
	OrderID string
}

type MarkOrderDeliveredHandler struct {
	base    Base
	sellers application.SellerMembership
}

func NewMarkOrderDeliveredHandler(base Base, sellers application.SellerMembership) *MarkOrderDeliveredHandler {
	return &MarkOrderDeliveredHandler{base: base, sellers: sellers}
}

func (h *MarkOrderDeliveredHandler) Handle(ctx context.Context, cmd MarkOrderDelivered) (struct{}, error) {
	id, err := domain.ParseOrderID(cmd.OrderID)
	if err != nil {
		return struct{}{}, domain.ErrOrderNotFound
	}
	return struct{}{}, h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		order, err := repos.Orders().FindByID(ctx, id)
		if err != nil {
			return err
		}
		actor, err := sellerActor(ctx, h.sellers, cmd.Actor, order)
		if err != nil {
			return err
		}
		if err := order.MarkDelivered(actor, h.base.Clock.Now()); err != nil {
			return err
		}
		return repos.Orders().Save(ctx, order)
	})
}

type CompleteDeliveredOrders struct{}

type CompleteDeliveredOrdersHandler struct {
	base Base
}

func NewCompleteDeliveredOrdersHandler(base Base) *CompleteDeliveredOrdersHandler {
	return &CompleteDeliveredOrdersHandler{base: base}
}

func (h *CompleteDeliveredOrdersHandler) Handle(ctx context.Context, _ CompleteDeliveredOrders) (int, error) {
	now := h.base.Clock.Now()
	var ids []domain.OrderID
	err := h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		var err error
		ids, err = repos.Orders().DeliveredBefore(ctx, now.Add(-h.base.Policy.ReturnWindow), h.base.Policy.BatchSize)
		return err
	})
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, id := range ids {
		err := h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
			order, err := repos.Orders().FindByID(ctx, id)
			if err != nil {
				return err
			}
			if !order.ReadyToComplete(h.base.Policy.ReturnWindow, h.base.Clock.Now()) {
				return nil
			}
			if err := order.Complete(h.base.Clock.Now()); err != nil {
				return err
			}
			return repos.Orders().Save(ctx, order)
		})
		errs = append(errs, err)
	}
	return len(ids), errors.Join(errs...)
}
