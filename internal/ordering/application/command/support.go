package command

import (
	"context"
	"log/slog"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
)

type Dependencies struct {
	UoW       application.UnitOfWork
	Clock     application.Clock
	Policy    application.Policy
	Carts     application.Carts
	Offers    application.Offers
	Pricing   application.Pricing
	Tariffs   application.Tariffs
	Inventory application.Inventory
	Payments  application.Payments
	Metrics   application.Metrics
	Logger    *slog.Logger
}

type Base struct {
	Dependencies
}

func NewBase(d Dependencies) Base {
	if d.Metrics == nil {
		d.Metrics = application.NopMetrics{}
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	return Base{Dependencies: d}
}

func (b Base) saga(ctx context.Context, id domain.OrderID) (*domain.CheckoutSaga, error) {
	var saga *domain.CheckoutSaga
	err := b.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		var err error
		saga, err = repos.Sagas().FindByOrder(ctx, id)
		return err
	})
	return saga, err
}

func (b Base) mutateSaga(ctx context.Context, id domain.OrderID, change func(saga *domain.CheckoutSaga) error) (*domain.CheckoutSaga, error) {
	var saga *domain.CheckoutSaga
	err := b.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		found, err := repos.Sagas().FindByOrder(ctx, id)
		if err != nil {
			return err
		}
		if err := change(found); err != nil {
			return err
		}
		saga = found
		return repos.Sagas().Save(ctx, found)
	})
	return saga, err
}

func (b Base) mutateBoth(ctx context.Context, id domain.OrderID, change func(order *domain.Order, saga *domain.CheckoutSaga) error) error {
	return b.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		order, err := repos.Orders().FindByID(ctx, id)
		if err != nil {
			return err
		}
		saga, err := repos.Sagas().FindByOrder(ctx, id)
		if err != nil {
			return err
		}
		if err := change(order, saga); err != nil {
			return err
		}
		if err := repos.Orders().Save(ctx, order); err != nil {
			return err
		}
		return repos.Sagas().Save(ctx, saga)
	})
}
