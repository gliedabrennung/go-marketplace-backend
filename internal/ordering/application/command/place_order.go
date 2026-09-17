package command

import (
	"context"
	"errors"
	"net/url"
	"strings"

	cartapi "github.com/gliedabrennung/go-marketplace-backend/internal/cart/api"
	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	inventoryapi "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	pricingapi "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	shippingapi "github.com/gliedabrennung/go-marketplace-backend/internal/shipping/api"
)

type PlaceOrder struct {
	Actor          auth.Principal
	Address        domain.Address
	DeliveryMethod string
	MethodID       string
	SaveMethod     bool
	ExpectedTotal  int64
}

type PlaceOrderResult struct {
	OrderID    string
	Status     string
	PaymentID  string
	PaymentURL string
	Total      int64
	Currency   string
}

type PlaceOrderHandler struct {
	base        Base
	compensator *Compensator
}

func NewPlaceOrderHandler(base Base, compensator *Compensator) *PlaceOrderHandler {
	return &PlaceOrderHandler{base: base, compensator: compensator}
}

type draft struct {
	spec       domain.PlaceSpec
	lines      []inventoryapi.ReserveLine
	skus       []string
	promoBasis int64
}

func (h *PlaceOrderHandler) Handle(ctx context.Context, cmd PlaceOrder) (PlaceOrderResult, error) {
	buyer, err := kernel.ParseUserID(cmd.Actor.UserID)
	if err != nil {
		return PlaceOrderResult{}, auth.ErrUnauthenticated
	}
	if _, err := domain.NewAddress(cmd.Address); err != nil {
		return PlaceOrderResult{}, err
	}
	plan, err := h.draft(ctx, buyer, cmd)
	if err != nil {
		return PlaceOrderResult{}, err
	}
	now := h.base.Clock.Now()
	order, err := domain.Place(plan.spec, now)
	if err != nil {
		return PlaceOrderResult{}, err
	}
	if cmd.ExpectedTotal > 0 && cmd.ExpectedTotal != order.Total().Amount() {
		return PlaceOrderResult{}, totalChanged(order.Total())
	}
	if err := h.start(ctx, order, plan); err != nil {
		return PlaceOrderResult{}, err
	}
	paymentID, paymentURL, err := h.pay(ctx, order, plan, cmd)
	if err != nil {
		return PlaceOrderResult{}, err
	}
	if err := h.base.Carts.RemoveOrdered(ctx, buyer.String(), plan.skus); err != nil {
		h.base.Logger.WarnContext(ctx, "ordered items were not removed from cart", "order_id", order.ID().String(), "err", err)
	}
	h.base.Metrics.OrderPlaced(string(domain.StatusAwaitingPayment), string(order.Total().Currency()), order.Total().Amount())
	return PlaceOrderResult{
		OrderID: order.ID().String(), Status: string(domain.StatusAwaitingPayment), PaymentID: paymentID,
		PaymentURL: paymentURL, Total: order.Total().Amount(), Currency: string(order.Total().Currency()),
	}, nil
}

func totalChanged(total kernel.Money) error {
	return application.ErrTotalChanged.WithDetail("current total %s", total)
}

func (h *PlaceOrderHandler) draft(ctx context.Context, buyer kernel.UserID, cmd PlaceOrder) (draft, error) {
	checkout, err := h.base.Carts.Checkout(ctx, buyer.String())
	if err != nil {
		return draft{}, err
	}
	plan := draft{skus: make([]string, 0, len(checkout.Items))}
	request := pricingapi.QuoteRequest{PromoCode: checkout.PromoCode, CustomerID: buyer.String()}
	for _, item := range checkout.Items {
		plan.skus = append(plan.skus, item.SKU)
		request.Lines = append(request.Lines, pricingapi.QuoteLine{SKU: item.SKU, Quantity: item.Quantity})
		plan.lines = append(plan.lines, inventoryapi.ReserveLine{SKU: item.SKU, Quantity: item.Quantity})
	}
	offers, err := h.base.Offers.Offers(ctx, plan.skus)
	if err != nil {
		return draft{}, err
	}
	if missing := unavailable(checkout, offers); len(missing) > 0 {
		return draft{}, application.ErrItemsUnavailable.WithDetail("skus: %s", strings.Join(missing, ", "))
	}
	quote, err := h.base.Pricing.Quote(ctx, request)
	if pricingapi.IsUnpricedOffer(err) {
		return draft{}, application.ErrItemsUnavailable.WithDetail("%v", err)
	}
	if err != nil {
		return draft{}, err
	}
	currency, err := kernel.NewCurrency(quote.Currency)
	if err != nil {
		return draft{}, err
	}
	plan.spec = domain.PlaceSpec{
		ID: domain.NewOrderID(), BuyerID: buyer, Address: cmd.Address, DeliveryMethod: cmd.DeliveryMethod,
		PromoCode: quote.PromoCode, Currency: currency,
	}
	if plan.spec.DeliveryMethod == "" {
		plan.spec.DeliveryMethod = shippingapi.MethodStandard
	}
	if err := h.price(ctx, &plan, quote, offers, currency); err != nil {
		return draft{}, err
	}
	return plan, nil
}

func unavailable(checkout cartapi.Checkout, offers map[string]catalogapi.OfferSummary) []string {
	var missing []string
	for _, item := range checkout.Items {
		offer, found := offers[item.SKU]
		if !found || offer.Status != "active" || !offer.ProductPublished {
			missing = append(missing, item.SKU)
		}
	}
	return missing
}

func (h *PlaceOrderHandler) price(ctx context.Context, plan *draft, quote pricingapi.Quote, offers map[string]catalogapi.OfferSummary, currency kernel.Currency) error {
	money := func(amount int64) (kernel.Money, error) { return kernel.NewMoney(amount, currency) }
	totals := map[string]int64{}
	sellers := []string{}
	for _, line := range quote.Lines {
		seller, err := kernel.ParseSellerID(line.SellerID)
		if err != nil {
			return err
		}
		item := domain.Item{
			SKU: line.SKU, ProductID: line.ProductID, CategoryID: offers[line.SKU].CategoryID, SellerID: seller,
			Title: offers[line.SKU].Title, Quantity: line.Quantity,
		}
		if item.UnitPrice, err = money(line.UnitPrice); err != nil {
			return err
		}
		if item.Base, err = money(line.Base); err != nil {
			return err
		}
		if item.Final, err = money(line.Final); err != nil {
			return err
		}
		plan.spec.Items = append(plan.spec.Items, item)
		if _, seen := totals[line.SellerID]; !seen {
			sellers = append(sellers, line.SellerID)
		}
		totals[line.SellerID] += line.Final
		plan.promoBasis += line.Final
		for _, discount := range line.Discounts {
			if discount.PromoCode != "" {
				plan.promoBasis += discount.Amount
			}
		}
	}
	request := shippingapi.TariffRequest{Method: plan.spec.DeliveryMethod, Currency: quote.Currency}
	for _, seller := range sellers {
		request.Parcels = append(request.Parcels, shippingapi.Parcel{SellerID: seller, Subtotal: totals[seller]})
	}
	tariff, err := h.base.Tariffs.Quote(ctx, request)
	if err != nil {
		return err
	}
	for _, parcel := range tariff.Parcels {
		seller, err := kernel.ParseSellerID(parcel.SellerID)
		if err != nil {
			return err
		}
		cost, err := money(parcel.Cost)
		if err != nil {
			return err
		}
		plan.spec.Shipping = append(plan.spec.Shipping, domain.ShippingCost{SellerID: seller, Cost: cost})
	}
	return nil
}

func (h *PlaceOrderHandler) start(ctx context.Context, order *domain.Order, plan draft) error {
	reservationID := domain.NewReference()
	reservation, err := h.base.Inventory.Reserve(ctx, reservationID, order.ID().String(), plan.lines)
	if err != nil {
		h.base.Metrics.SagaStep(string(domain.StepStockReserved), "rejected")
		return err
	}
	now := h.base.Clock.Now()
	deadline := now.Add(h.base.Policy.PaymentTimeout)
	if !reservation.ExpiresAt.IsZero() && reservation.ExpiresAt.Before(deadline) {
		deadline = reservation.ExpiresAt
	}
	saga, err := domain.StartSaga(domain.SagaSpec{
		OrderID: order.ID(), BuyerID: order.BuyerID(), ReservationID: reservationID, PaymentID: domain.NewReference(),
		PromoCode: order.PromoCode(), Amount: order.Total(), Deadline: deadline,
	}, now)
	if err == nil {
		err = h.base.UoW.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
			if err := repos.Orders().Save(ctx, order); err != nil {
				return err
			}
			return repos.Sagas().Save(ctx, saga)
		})
	}
	if err != nil {
		return errors.Join(err, h.base.Inventory.Release(ctx, reservationID))
	}
	h.base.Metrics.SagaStep(string(domain.StepStockReserved), "success")
	if order.PromoCode() == "" {
		return nil
	}
	if err := h.base.Pricing.Redeem(ctx, order.PromoCode(), order.ID().String(), order.BuyerID().String(),
		plan.promoBasis, string(order.Total().Currency())); err != nil {
		h.base.Metrics.SagaStep(string(domain.StepPromoRedeemed), "rejected")
		return errors.Join(err, h.compensator.Abort(ctx, order.ID(), "promo code redemption failed"))
	}
	_, err = h.base.mutateSaga(ctx, order.ID(), func(saga *domain.CheckoutSaga) error {
		return saga.PromoRedeemed(h.base.Clock.Now())
	})
	return err
}

func (h *PlaceOrderHandler) pay(ctx context.Context, order *domain.Order, plan draft, cmd PlaceOrder) (string, string, error) {
	saga, err := h.base.saga(ctx, order.ID())
	if err != nil {
		return "", "", err
	}
	created, err := h.base.Payments.Create(ctx, paymentapi.CreateRequest{
		PaymentID: saga.PaymentID(), OrderID: order.ID().String(), BuyerID: order.BuyerID().String(),
		Amount: order.Total().Amount(), Currency: string(order.Total().Currency()), ReturnURL: h.returnURL(order.ID()),
		SaveMethod: cmd.SaveMethod, MethodID: cmd.MethodID,
	})
	if err == nil && created.Status != paymentapi.StatusPending {
		err = application.ErrPaymentUnavailable.WithDetail("payment status %s", created.Status)
	}
	if err != nil {
		h.base.Metrics.SagaStep(string(domain.StepAwaitingPayment), "rejected")
		return "", "", errors.Join(err, h.compensator.Abort(ctx, order.ID(), "payment could not be initiated"))
	}
	err = h.base.mutateBoth(ctx, order.ID(), func(order *domain.Order, saga *domain.CheckoutSaga) error {
		now := h.base.Clock.Now()
		if err := order.AwaitPayment(created.PaymentID, now); err != nil {
			return err
		}
		return saga.AwaitPayment(created.PaymentID, now)
	})
	if err != nil {
		return "", "", err
	}
	h.base.Metrics.SagaStep(string(domain.StepAwaitingPayment), "success")
	return created.PaymentID, created.RedirectURL, nil
}

func (h *PlaceOrderHandler) returnURL(id domain.OrderID) string {
	target, err := url.Parse(h.base.Policy.ReturnURL)
	if err != nil {
		return h.base.Policy.ReturnURL
	}
	query := target.Query()
	query.Set("order_id", id.String())
	target.RawQuery = query.Encode()
	return target.String()
}
