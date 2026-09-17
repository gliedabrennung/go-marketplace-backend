package command_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cartapi "github.com/gliedabrennung/go-marketplace-backend/internal/cart/api"
	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	inventoryapi "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/infrastructure/memory"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	pricingapi "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
	shippingapi "github.com/gliedabrennung/go-marketplace-backend/internal/shipping/api"
)

var ctx = context.Background()

type env struct {
	store     *memory.Store
	reads     *memory.ReadModel
	clock     *clock.Manual
	carts     *carts
	catalog   catalog
	pricing   *pricing
	inventory *inventory
	payments  *payments
	metrics   *metrics
	team      membership

	place      *command.PlaceOrderHandler
	authorized *command.PaymentAuthorizedHandler
	expire     *command.ExpireCheckoutsHandler
	compensate *command.RunCompensationsHandler
	stalled    *command.ResumeStalledHandler
	cancel     *command.CancelOrderHandler
	retry      *command.RetryPaymentHandler
	resume     *command.ResumeSagaHandler
	ship       *command.MarkOrderShippedHandler
	deliver    *command.MarkOrderDeliveredHandler
	complete   *command.CompleteDeliveredOrdersHandler

	get     *query.GetOrderHandler
	list    *query.ListOrdersHandler
	sellers *query.ListSellerOrdersHandler
	sagas   *query.ListSagasHandler
}

var (
	sellerA = kernel.NewSellerID().String()
	sellerB = kernel.NewSellerID().String()
)

func newEnv() *env {
	store := memory.NewStore()
	e := &env{
		store: store, reads: memory.NewReadModel(store), clock: clock.NewManual(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)),
		carts:   &carts{checkout: map[string]cartapi.Checkout{}, removed: map[string][]string{}},
		catalog: catalog{}, pricing: &pricing{prices: map[string]pricingapi.PricedLine{}, redeemed: map[string]bool{}},
		payments: &payments{infos: map[string]paymentapi.Info{}, refunds: map[string]string{}},
		metrics:  &metrics{counts: map[string]int{}}, team: membership{},
	}
	e.inventory = &inventory{
		stock: map[string]int{}, reservations: map[string]string{}, lines: map[string][]inventoryapi.ReserveLine{},
		ttl: 20 * time.Minute, now: e.clock.Now,
	}
	policy := application.DefaultPolicy()
	policy.CompensationAttempts = 3
	policy.ReturnURL = "https://shop.example/checkout/result?source=psp"
	base := command.NewBase(command.Dependencies{
		UoW: memory.NewUnitOfWork(store), Clock: e.clock, Policy: policy, Carts: e.carts, Offers: e.catalog, Pricing: e.pricing,
		Tariffs: tariffs{fee: 500, freeFrom: 10000}, Inventory: e.inventory, Payments: e.payments, Metrics: e.metrics,
	})
	compensator := command.NewCompensator(base)
	e.place = command.NewPlaceOrderHandler(base, compensator)
	e.authorized = command.NewPaymentAuthorizedHandler(base, compensator)
	e.expire = command.NewExpireCheckoutsHandler(base, compensator)
	e.compensate = command.NewRunCompensationsHandler(base, compensator)
	e.stalled = command.NewResumeStalledHandler(base, compensator)
	e.cancel = command.NewCancelOrderHandler(base, compensator)
	e.retry = command.NewRetryPaymentHandler(base, e.place)
	e.resume = command.NewResumeSagaHandler(base, compensator)
	e.ship = command.NewMarkOrderShippedHandler(base, e.team)
	e.deliver = command.NewMarkOrderDeliveredHandler(base, e.team)
	e.complete = command.NewCompleteDeliveredOrdersHandler(base)
	e.get = query.NewGetOrderHandler(e.reads, e.payments)
	e.list = query.NewListOrdersHandler(e.reads)
	e.sellers = query.NewListSellerOrdersHandler(e.reads, e.team)
	e.sagas = query.NewListSagasHandler(e.reads)
	return e
}

func (e *env) offer(sku, seller string, price int64, stock int) {
	e.catalog[sku] = catalogapi.OfferSummary{
		OfferID: sku, ProductID: "product-" + sku, SellerID: seller, Title: "Товар " + sku, Status: "active",
		PriceAmount: price, Currency: "KZT", ProductPublished: true,
	}
	e.pricing.prices[sku] = pricingapi.PricedLine{SKU: sku, SellerID: seller, ProductID: "product-" + sku, UnitPrice: price}
	e.inventory.stock[sku] = stock
}

func buyer() auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer"}}
}

func support() auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"support_agent"}}
}

func (e *env) fill(who auth.Principal, promo string, items ...cartapi.Item) {
	e.carts.checkout[who.UserID] = cartapi.Checkout{CartID: "cart", Currency: "KZT", PromoCode: promo, Items: items}
}

func address() domain.Address {
	return domain.Address{Recipient: "Айгуль Сапарова", Phone: "+77011234567", City: "Алматы", Line: "пр. Абая, 1"}
}

func (e *env) order(t *testing.T, who auth.Principal, promo string) command.PlaceOrderResult {
	t.Helper()
	e.offer("A", sellerA, 3000, 5)
	e.offer("B", sellerB, 12000, 5)
	e.fill(who, promo, cartapi.Item{SKU: "A", Quantity: 2}, cartapi.Item{SKU: "B", Quantity: 1})
	result, err := e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address(), SaveMethod: true})
	require.NoError(t, err)
	return result
}

func (e *env) saga(t *testing.T, orderID string) domain.SagaSnapshot {
	t.Helper()
	id, err := domain.ParseOrderID(orderID)
	require.NoError(t, err)
	saga, err := e.store.Sagas().FindByOrder(ctx, id)
	require.NoError(t, err)
	return saga.Snapshot()
}

func (e *env) view(t *testing.T, who auth.Principal, orderID string) query.OrderView {
	t.Helper()
	view, err := e.get.Handle(ctx, query.GetOrder{Actor: who, OrderID: orderID})
	require.NoError(t, err)
	return view
}

func (e *env) authorize(t *testing.T, result command.PlaceOrderResult) {
	t.Helper()
	e.payments.set(result.PaymentID, func(info *paymentapi.Info) {
		if info.Status == paymentapi.StatusPending {
			info.Status, info.Authorized = paymentapi.StatusAuthorized, info.Amount
		}
	})
	_, err := e.authorized.Handle(ctx, command.PaymentAuthorized{OrderID: result.OrderID, PaymentID: result.PaymentID})
	require.NoError(t, err)
}

func eventNames(events []kernel.DomainEvent) []string {
	out := []string{}
	for _, event := range events {
		out = append(out, event.EventName())
	}
	return out
}

func TestCheckoutHappyPath(t *testing.T) {
	e := newEnv()
	who := buyer()
	result := e.order(t, who, "SALE10")

	assert.Equal(t, "awaiting_payment", result.Status)
	assert.Equal(t, int64(17900), result.Total)
	assert.Contains(t, result.PaymentURL, "order_id="+result.OrderID)
	assert.Contains(t, result.PaymentURL, "source=psp")
	assert.ElementsMatch(t, []string{"A", "B"}, e.carts.removed[who.UserID])
	assert.True(t, e.pricing.redeemed["SALE10/"+result.OrderID])
	saga := e.saga(t, result.OrderID)
	assert.Equal(t, "awaiting_payment", saga.Step)
	assert.Equal(t, "held", e.inventory.status(saga.ReservationID))
	assert.Equal(t, 3, e.inventory.stock["A"])

	view := e.view(t, who, result.OrderID)
	require.NotNil(t, view.Payment)
	assert.Equal(t, "pending", view.Payment.Status)
	assert.NotEmpty(t, view.Payment.RedirectURL)
	assert.Equal(t, e.clock.Now().Add(20*time.Minute), view.Payment.PayBefore)
	assert.True(t, view.Cancellable)
	assert.Len(t, view.Parts, 2)

	e.authorize(t, result)
	e.authorize(t, result)
	saga = e.saga(t, result.OrderID)
	assert.Equal(t, "completed", saga.Status)
	assert.Equal(t, "committed", e.inventory.status(saga.ReservationID))
	assert.Equal(t, "captured", e.payments.status(result.PaymentID))

	view = e.view(t, who, result.OrderID)
	assert.Equal(t, "paid", view.Status)
	assert.Empty(t, view.Payment.RedirectURL)
	assert.Len(t, view.History, 3)
	assert.Equal(t, []string{
		"ordering.order_created.v1", "ordering.order_awaiting_payment.v1", "ordering.order_paid.v1",
	}, eventNames(e.store.Events()))
	assert.Equal(t, 1, e.metrics.get("placed:awaiting_payment:KZT"))
	assert.Equal(t, 1, e.metrics.get("step:completed:success"))

	_, err := e.get.Handle(ctx, query.GetOrder{Actor: buyer(), OrderID: result.OrderID})
	require.ErrorIs(t, err, domain.ErrOrderNotFound)
	_, err = e.get.Handle(ctx, query.GetOrder{Actor: support(), OrderID: result.OrderID})
	require.NoError(t, err)
	_, err = e.get.Handle(ctx, query.GetOrder{Actor: who, OrderID: "bogus"})
	require.ErrorIs(t, err, domain.ErrOrderNotFound)
	_, err = e.get.Handle(ctx, query.GetOrder{OrderID: result.OrderID})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}

func TestPlaceOrderRejections(t *testing.T) {
	e := newEnv()
	who := buyer()
	_, err := e.place.Handle(ctx, command.PlaceOrder{Actor: auth.Principal{}, Address: address()})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: domain.Address{}})
	require.ErrorIs(t, err, domain.ErrInvalidAddress)
	_, err = e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address()})
	require.ErrorIs(t, err, cartapi.ErrCartEmpty)

	e.offer("A", sellerA, 3000, 1)
	e.fill(who, "", cartapi.Item{SKU: "A", Quantity: 2})
	_, err = e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address(), ExpectedTotal: 1})
	require.ErrorIs(t, err, application.ErrTotalChanged)
	_, err = e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address()})
	require.ErrorIs(t, err, inventoryapi.ErrInsufficientStock)
	_, err = e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address(), DeliveryMethod: "drone"})
	require.Error(t, err)

	archived := e.catalog["A"]
	archived.Status = "archived"
	e.catalog["A"] = archived
	_, err = e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address()})
	require.ErrorIs(t, err, application.ErrItemsUnavailable)

	e.offer("C", sellerA, 100, 5)
	delete(e.pricing.prices, "C")
	e.fill(who, "", cartapi.Item{SKU: "C", Quantity: 1})
	_, err = e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address()})
	require.ErrorIs(t, err, application.ErrItemsUnavailable)
	assert.Empty(t, e.store.Events())
}

func TestPromoAndPaymentFailuresCompensate(t *testing.T) {
	e := newEnv()
	who := buyer()
	e.pricing.failRedeem = errRejected
	e.offer("A", sellerA, 3000, 5)
	e.offer("B", sellerB, 12000, 5)
	e.fill(who, "SALE10", cartapi.Item{SKU: "A", Quantity: 1})
	_, err := e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address()})
	require.ErrorIs(t, err, errRejected)
	assert.Equal(t, 5, e.inventory.stock["A"])
	page, err := e.list.Handle(ctx, query.ListOrders{Actor: who})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "failed", page.Items[0].Status)
	saga := e.saga(t, page.Items[0].ID)
	assert.Equal(t, "compensated", saga.Status)
	assert.ElementsMatch(t, []string{"cancel_payment", "return_stock", "release_promo", "close_order"}, saga.Compensated)

	e.pricing.failRedeem = nil
	e.payments.failCreate = errRejected
	_, err = e.place.Handle(ctx, command.PlaceOrder{Actor: who, Address: address()})
	require.ErrorIs(t, err, errRejected)
	assert.Equal(t, 5, e.inventory.stock["A"])
	assert.Empty(t, e.pricing.redeemed)
	assert.Empty(t, e.carts.removed[who.UserID])
}

func TestPaymentTimeoutCompensation(t *testing.T) {
	e := newEnv()
	who := buyer()
	result := e.order(t, who, "SALE10")

	processed, err := e.expire.Handle(ctx, command.ExpireCheckouts{})
	require.NoError(t, err)
	assert.Zero(t, processed)

	e.clock.Advance(21 * time.Minute)
	processed, err = e.expire.Handle(ctx, command.ExpireCheckouts{})
	require.NoError(t, err)
	assert.Equal(t, 1, processed)

	saga := e.saga(t, result.OrderID)
	assert.Equal(t, "compensated", saga.Status)
	assert.Equal(t, "cancelled", e.payments.status(result.PaymentID))
	assert.Equal(t, "released", e.inventory.status(saga.ReservationID))
	assert.Empty(t, e.pricing.redeemed)
	assert.Equal(t, "failed", e.view(t, who, result.OrderID).Status)

	e.payments.set(result.PaymentID, func(info *paymentapi.Info) { info.Status = paymentapi.StatusAuthorized })
	_, err = e.authorized.Handle(ctx, command.PaymentAuthorized{OrderID: result.OrderID, PaymentID: result.PaymentID})
	require.NoError(t, err)
	assert.Equal(t, "compensated", e.saga(t, result.OrderID).Status)
	_, err = e.authorized.Handle(ctx, command.PaymentAuthorized{OrderID: "bogus", PaymentID: result.PaymentID})
	require.NoError(t, err)
	_, err = e.authorized.Handle(ctx, command.PaymentAuthorized{OrderID: domain.NewOrderID().String(), PaymentID: "x"})
	require.NoError(t, err)
}

func TestCommitAndCaptureFailures(t *testing.T) {
	e := newEnv()
	who := buyer()

	lost := e.order(t, who, "")
	e.inventory.reservations[e.saga(t, lost.OrderID).ReservationID] = "expired"
	e.authorize(t, lost)
	assert.Equal(t, "compensated", e.saga(t, lost.OrderID).Status)
	assert.Equal(t, "cancelled", e.payments.status(lost.PaymentID))
	assert.Equal(t, "failed", e.view(t, who, lost.OrderID).Status)

	rejected := e.order(t, who, "")
	e.payments.failCapture = errRejected
	e.authorize(t, rejected)
	saga := e.saga(t, rejected.OrderID)
	assert.Equal(t, "compensated", saga.Status)
	assert.Equal(t, "restored", e.inventory.status(saga.ReservationID))
	assert.Equal(t, "cancelled", e.payments.status(rejected.PaymentID))
	e.payments.failCapture = nil

	flaky := e.order(t, who, "")
	e.inventory.failCommit = errTransient
	e.payments.set(flaky.PaymentID, func(info *paymentapi.Info) { info.Status = paymentapi.StatusAuthorized })
	_, err := e.authorized.Handle(ctx, command.PaymentAuthorized{OrderID: flaky.OrderID, PaymentID: flaky.PaymentID})
	require.ErrorIs(t, err, errTransient)
	saga = e.saga(t, flaky.OrderID)
	assert.Equal(t, "committing_stock", saga.Step)
	assert.Equal(t, "connection reset", saga.LastError)
	assert.False(t, e.view(t, who, flaky.OrderID).Cancellable)
	_, err = e.cancel.Handle(ctx, command.CancelOrder{Actor: who, OrderID: flaky.OrderID, Reason: "долго"})
	require.ErrorIs(t, err, domain.ErrCancellationDenied)

	e.clock.Advance(time.Hour)
	_, err = e.expire.Handle(ctx, command.ExpireCheckouts{})
	require.NoError(t, err)
	e.inventory.failCommit = nil
	resumed, err := e.stalled.Handle(ctx, command.ResumeStalled{})
	require.NoError(t, err)
	assert.Equal(t, 1, resumed)
	assert.Equal(t, "completed", e.saga(t, flaky.OrderID).Status)
	assert.Equal(t, "paid", e.view(t, who, flaky.OrderID).Status)

	e.payments.failCapture = errTransient
	stuck := e.order(t, who, "")
	e.payments.set(stuck.PaymentID, func(info *paymentapi.Info) { info.Status = paymentapi.StatusAuthorized })
	_, err = e.authorized.Handle(ctx, command.PaymentAuthorized{OrderID: stuck.OrderID, PaymentID: stuck.PaymentID})
	require.ErrorIs(t, err, errTransient)
	assert.Equal(t, "stock_committed", e.saga(t, stuck.OrderID).Step)
}

func TestCancellationAndRefund(t *testing.T) {
	e := newEnv()
	who := buyer()

	pending := e.order(t, who, "SALE10")
	_, err := e.cancel.Handle(ctx, command.CancelOrder{Actor: buyer(), OrderID: pending.OrderID, Reason: "x"})
	require.ErrorIs(t, err, domain.ErrOrderNotFound)
	_, err = e.cancel.Handle(ctx, command.CancelOrder{Actor: who, OrderID: "bogus", Reason: "x"})
	require.ErrorIs(t, err, domain.ErrOrderNotFound)
	_, err = e.cancel.Handle(ctx, command.CancelOrder{Actor: auth.Principal{}, OrderID: pending.OrderID, Reason: "x"})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.cancel.Handle(ctx, command.CancelOrder{Actor: who, OrderID: pending.OrderID, Reason: "передумал"})
	require.NoError(t, err)
	view := e.view(t, who, pending.OrderID)
	assert.Equal(t, "cancelled", view.Status)
	assert.Equal(t, "cancelled", e.payments.status(pending.PaymentID))
	assert.Equal(t, "compensated", e.saga(t, pending.OrderID).Status)

	paid := e.order(t, who, "")
	e.authorize(t, paid)
	_, err = e.cancel.Handle(ctx, command.CancelOrder{Actor: support(), OrderID: paid.OrderID, Reason: "нет в наличии"})
	require.NoError(t, err)
	saga := e.saga(t, paid.OrderID)
	assert.Equal(t, "compensated", saga.Status)
	assert.Equal(t, "refunded", e.payments.status(paid.PaymentID))
	assert.Equal(t, saga.RefundID, e.payments.refunds[paid.PaymentID])
	assert.Equal(t, "restored", e.inventory.status(saga.ReservationID))
	history := e.view(t, who, paid.OrderID).History
	assert.Equal(t, "support", history[len(history)-1].ActorKind)

	_, err = e.cancel.Handle(ctx, command.CancelOrder{Actor: who, OrderID: paid.OrderID, Reason: "ещё раз"})
	require.NoError(t, err)
}

func TestCompensationRetriesAndManualIntervention(t *testing.T) {
	e := newEnv()
	who := buyer()
	result := e.order(t, who, "")
	e.inventory.failRelease = errTransient

	_, err := e.cancel.Handle(ctx, command.CancelOrder{Actor: who, OrderID: result.OrderID, Reason: "передумал"})
	require.NoError(t, err)
	saga := e.saga(t, result.OrderID)
	assert.Equal(t, "compensating", saga.Status)
	assert.Equal(t, 1, saga.Attempts)

	processed, err := e.compensate.Handle(ctx, command.RunCompensations{})
	require.NoError(t, err)
	assert.Zero(t, processed)
	for range 2 {
		e.clock.Advance(time.Hour)
		_, err = e.compensate.Handle(ctx, command.RunCompensations{})
		require.ErrorIs(t, err, errTransient)
	}
	assert.Equal(t, "manual", e.saga(t, result.OrderID).Status)
	assert.Equal(t, 3, e.metrics.get("compensation:return_stock:error"))

	page, err := e.sagas.Handle(ctx, query.ListSagas{Actor: support(), Status: "manual"})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "connection reset", page.Items[0].LastError)
	_, err = e.sagas.Handle(ctx, query.ListSagas{Actor: who})
	require.Error(t, err)
	_, err = e.sagas.Handle(ctx, query.ListSagas{Actor: support(), Status: "lost"})
	require.ErrorIs(t, err, query.ErrUnknownStatus)
	_, err = e.sagas.Handle(ctx, query.ListSagas{Actor: support(), Cursor: "%%"})
	require.ErrorIs(t, err, pagination.ErrInvalidCursor)

	_, err = e.resume.Handle(ctx, command.ResumeSaga{Actor: who, OrderID: result.OrderID})
	require.Error(t, err)
	_, err = e.resume.Handle(ctx, command.ResumeSaga{Actor: support(), OrderID: "bogus"})
	require.ErrorIs(t, err, domain.ErrSagaNotFound)
	e.inventory.failRelease = nil
	_, err = e.resume.Handle(ctx, command.ResumeSaga{Actor: support(), OrderID: result.OrderID})
	require.NoError(t, err)
	assert.Equal(t, "compensated", e.saga(t, result.OrderID).Status)
	_, err = e.resume.Handle(ctx, command.ResumeSaga{Actor: support(), OrderID: result.OrderID})
	require.ErrorIs(t, err, domain.ErrSagaNotManual)
}

func TestRetryPayment(t *testing.T) {
	e := newEnv()
	who := buyer()
	result := e.order(t, who, "")

	same, err := e.retry.Handle(ctx, command.RetryPayment{Actor: who, PaymentID: result.PaymentID})
	require.NoError(t, err)
	assert.Equal(t, result.PaymentID, same.PaymentID)
	_, err = e.retry.Handle(ctx, command.RetryPayment{Actor: buyer(), PaymentID: result.PaymentID})
	require.ErrorIs(t, err, paymentapi.ErrPaymentNotFound)

	e.payments.set(result.PaymentID, func(info *paymentapi.Info) { info.Status = paymentapi.StatusFailed })
	retried, err := e.retry.Handle(ctx, command.RetryPayment{Actor: who, PaymentID: result.PaymentID})
	require.NoError(t, err)
	assert.NotEqual(t, result.PaymentID, retried.PaymentID)
	assert.Equal(t, result.OrderID, retried.OrderID)
	assert.Equal(t, retried.PaymentID, e.view(t, who, result.OrderID).Payment.PaymentID)

	e.payments.set(result.PaymentID, func(info *paymentapi.Info) { info.Status = paymentapi.StatusAuthorized })
	_, err = e.authorized.Handle(ctx, command.PaymentAuthorized{OrderID: result.OrderID, PaymentID: result.PaymentID})
	require.NoError(t, err)
	assert.Equal(t, "cancelled", e.payments.status(result.PaymentID))
	assert.Equal(t, "awaiting_payment", e.saga(t, result.OrderID).Step)

	e.authorize(t, command.PlaceOrderResult{OrderID: result.OrderID, PaymentID: retried.PaymentID})
	_, err = e.retry.Handle(ctx, command.RetryPayment{Actor: who, PaymentID: retried.PaymentID})
	require.ErrorIs(t, err, application.ErrRetryNotAllowed)

	other := e.order(t, who, "")
	e.payments.set(other.PaymentID, func(info *paymentapi.Info) { info.Status = paymentapi.StatusAuthorized })
	_, err = e.retry.Handle(ctx, command.RetryPayment{Actor: who, PaymentID: other.PaymentID})
	require.ErrorIs(t, err, application.ErrRetryNotAllowed)
	e.payments.set(other.PaymentID, func(info *paymentapi.Info) { info.Status = paymentapi.StatusFailed })
	e.payments.failCreate = errRejected
	_, err = e.retry.Handle(ctx, command.RetryPayment{Actor: who, PaymentID: other.PaymentID})
	require.ErrorIs(t, err, errRejected)
}

func TestOrderListings(t *testing.T) {
	e := newEnv()
	who := buyer()
	first := e.order(t, who, "")
	e.authorize(t, first)
	e.clock.Advance(time.Minute)
	second := e.order(t, who, "")

	page, err := e.list.Handle(ctx, query.ListOrders{Actor: who, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, second.OrderID, page.Items[0].ID)
	assert.Equal(t, 3, page.Items[0].ItemsCount)
	require.True(t, page.HasMore)
	page, err = e.list.Handle(ctx, query.ListOrders{Actor: who, Limit: 1, Cursor: page.NextCursor})
	require.NoError(t, err)
	assert.Equal(t, first.OrderID, page.Items[0].ID)
	page, err = e.list.Handle(ctx, query.ListOrders{Actor: who, Status: "paid"})
	require.NoError(t, err)
	assert.Len(t, page.Items, 1)
	_, err = e.list.Handle(ctx, query.ListOrders{Actor: who, Status: "lost"})
	require.ErrorIs(t, err, query.ErrUnknownStatus)
	_, err = e.list.Handle(ctx, query.ListOrders{Actor: auth.Principal{}})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.list.Handle(ctx, query.ListOrders{Actor: who, Cursor: "%%"})
	require.ErrorIs(t, err, pagination.ErrInvalidCursor)

	owner := buyer()
	e.team[sellerA] = owner.UserID
	sellerPage, err := e.sellers.Handle(ctx, query.ListSellerOrders{Actor: owner, SellerID: sellerA})
	require.NoError(t, err)
	require.Len(t, sellerPage.Items, 1)
	assert.Equal(t, first.OrderID, sellerPage.Items[0].ID)
	require.Len(t, sellerPage.Items[0].Items, 1)
	assert.Equal(t, "A", sellerPage.Items[0].Items[0].SKU)
	assert.Equal(t, int64(6500), sellerPage.Items[0].Part.Total)

	hidden, err := e.sellers.Handle(ctx, query.ListSellerOrders{Actor: owner, SellerID: sellerA, Status: "awaiting_payment"})
	require.NoError(t, err)
	assert.Empty(t, hidden.Items)
	_, err = e.sellers.Handle(ctx, query.ListSellerOrders{Actor: who, SellerID: sellerA})
	require.ErrorIs(t, err, query.ErrNotMember)
	_, err = e.sellers.Handle(ctx, query.ListSellerOrders{Actor: owner, SellerID: "bogus"})
	require.ErrorIs(t, err, query.ErrNotMember)
	_, err = e.sellers.Handle(ctx, query.ListSellerOrders{SellerID: sellerA})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.sellers.Handle(ctx, query.ListSellerOrders{Actor: support(), SellerID: sellerA, Status: "lost"})
	require.ErrorIs(t, err, query.ErrUnknownStatus)
	assert.True(t, strings.HasPrefix(second.PaymentURL, "https://psp.example/pay/"))
}

type tariffs struct {
	fee, freeFrom int64
}

func (t tariffs) Quote(_ context.Context, request shippingapi.TariffRequest) (shippingapi.TariffQuote, error) {
	if request.Method != "" && request.Method != shippingapi.MethodStandard {
		return shippingapi.TariffQuote{}, shippingapi.ErrUnknownMethod
	}
	quote := shippingapi.TariffQuote{Method: shippingapi.MethodStandard, Currency: request.Currency}
	for _, parcel := range request.Parcels {
		cost := t.fee
		if parcel.Subtotal >= t.freeFrom {
			cost = 0
		}
		quote.Parcels = append(quote.Parcels, shippingapi.ParcelCost{SellerID: parcel.SellerID, Cost: cost})
		quote.Total += cost
	}
	return quote, nil
}
