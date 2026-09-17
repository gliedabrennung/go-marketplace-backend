package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	now     = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	sellerA = kernel.NewSellerID()
	sellerB = kernel.NewSellerID()
)

func money(amount int64) kernel.Money { return kernel.MustMoney(amount, kernel.KZT) }

func address() domain.Address {
	return domain.Address{Recipient: "Айгуль", Phone: "+7 (701) 123-45-67", City: "Алматы", Line: "пр. Абая, 1", PostalCode: "050000"}
}

func item(sku string, seller kernel.SellerID, quantity int, unit, final int64) domain.Item {
	return domain.Item{
		SKU: sku, ProductID: "product-" + sku, SellerID: seller, Title: "Товар " + sku, Quantity: quantity,
		UnitPrice: money(unit), Base: money(unit * int64(quantity)), Final: money(final),
	}
}

func spec() domain.PlaceSpec {
	return domain.PlaceSpec{
		ID: domain.NewOrderID(), BuyerID: kernel.NewUserID(), Address: address(), DeliveryMethod: "standard",
		PromoCode: "SALE", Currency: kernel.KZT,
		Items:    []domain.Item{item("A", sellerA, 2, 1000, 1800), item("B", sellerA, 1, 500, 500), item("C", sellerB, 1, 3000, 2700)},
		Shipping: []domain.ShippingCost{{SellerID: sellerA, Cost: money(700)}, {SellerID: sellerB, Cost: money(0)}},
	}
}

func placed(t *testing.T) *domain.Order {
	t.Helper()
	order, err := domain.Place(spec(), now)
	require.NoError(t, err)
	return order
}

func names(events []kernel.DomainEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.EventName())
	}
	return out
}

func TestPlaceOrderComputesTotals(t *testing.T) {
	order := placed(t)
	assert.Equal(t, domain.StatusCreated, order.Status())
	assert.Equal(t, int64(5500), order.Subtotal().Amount())
	assert.Equal(t, int64(500), order.Discount().Amount())
	assert.Equal(t, int64(700), order.Shipping().Amount())
	assert.Equal(t, int64(5700), order.Total().Amount())
	assert.Equal(t, "+77011234567", order.Address().Phone)
	assert.Equal(t, "KZ", order.Address().Country)
	assert.Equal(t, "standard", order.DeliveryMethod())
	assert.Equal(t, "SALE", order.PromoCode())
	assert.Len(t, order.Items(), 3)
	assert.Equal(t, []kernel.SellerID{sellerA, sellerB}, order.SellerIDs())

	parts := order.Parts()
	require.Len(t, parts, 2)
	assert.Equal(t, int64(2500), parts[0].Subtotal.Amount())
	assert.Equal(t, int64(200), parts[0].Discount.Amount())
	assert.Equal(t, int64(3000), parts[0].Total.Amount())
	assert.Equal(t, int64(2700), parts[1].Total.Amount())

	history := order.History()
	require.Len(t, history, 1)
	assert.Equal(t, domain.ActorBuyer, history[0].Actor.Kind)
	assert.Equal(t, []string{"ordering.order_created.v1"}, names(order.PullEvents()))
	assert.Equal(t, now, order.CreatedAt())
}

func TestPlaceOrderValidation(t *testing.T) {
	cases := map[string]struct {
		mutate func(s *domain.PlaceSpec)
		err    error
	}{
		"id":             {func(s *domain.PlaceSpec) { s.ID = domain.OrderID{} }, kernel.ErrInvalidID},
		"empty":          {func(s *domain.PlaceSpec) { s.Items = nil }, domain.ErrEmptyOrder},
		"too many":       {func(s *domain.PlaceSpec) { s.Items = make([]domain.Item, domain.MaxItems+1) }, domain.ErrTooManyItems},
		"address":        {func(s *domain.PlaceSpec) { s.Address.City = "" }, domain.ErrInvalidAddress},
		"phone":          {func(s *domain.PlaceSpec) { s.Address.Phone = "12" }, domain.ErrInvalidAddress},
		"country":        {func(s *domain.PlaceSpec) { s.Address.Country = "KAZ" }, domain.ErrInvalidAddress},
		"postal":         {func(s *domain.PlaceSpec) { s.Address.PostalCode = strings.Repeat("1", 21) }, domain.ErrInvalidAddress},
		"duplicate sku":  {func(s *domain.PlaceSpec) { s.Items[1].SKU = "A" }, domain.ErrInvalidItem},
		"quantity":       {func(s *domain.PlaceSpec) { s.Items[0].Quantity = 0 }, domain.ErrInvalidItem},
		"base mismatch":  {func(s *domain.PlaceSpec) { s.Items[0].Base = money(1) }, domain.ErrInvalidItem},
		"final too high": {func(s *domain.PlaceSpec) { s.Items[0].Final = money(999999) }, domain.ErrInvalidItem},
		"currency": {func(s *domain.PlaceSpec) {
			s.Items[0].Final = kernel.MustMoney(1, kernel.Currency("USD"))
		}, domain.ErrCurrencyMismatch},
		"missing shipping": {func(s *domain.PlaceSpec) { s.Shipping = s.Shipping[:1] }, domain.ErrInvalidShipping},
		"foreign shipping": {func(s *domain.PlaceSpec) { s.Shipping[1].SellerID = kernel.NewSellerID() }, domain.ErrInvalidShipping},
		"double shipping": {func(s *domain.PlaceSpec) {
			s.Shipping[1] = domain.ShippingCost{SellerID: sellerA, Cost: money(5)}
		}, domain.ErrInvalidShipping},
		"shipping currency": {func(s *domain.PlaceSpec) {
			s.Shipping[1].Cost = kernel.MustMoney(1, kernel.Currency("USD"))
		}, domain.ErrCurrencyMismatch},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := spec()
			tc.mutate(&s)
			_, err := domain.Place(s, now)
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestOrderPaymentFlow(t *testing.T) {
	order := placed(t)
	order.PullEvents()
	require.ErrorIs(t, order.AwaitPayment(" ", now), domain.ErrInvalidReference)
	require.ErrorIs(t, order.MarkPaid(money(5700), now), &domain.TransitionError{})

	require.NoError(t, order.AwaitPayment("pay-1", now))
	require.NoError(t, order.AwaitPayment("pay-1", now))
	require.NoError(t, order.AwaitPayment("pay-2", now.Add(time.Minute)))
	assert.Equal(t, "pay-2", order.PaymentID())
	assert.Equal(t, []string{"ordering.order_awaiting_payment.v1", "ordering.order_awaiting_payment.v1"}, names(order.PullEvents()))

	require.ErrorIs(t, order.MarkPaid(money(1), now), domain.ErrPaidAmountMismatch)
	require.NoError(t, order.MarkPaid(money(5700), now.Add(2*time.Minute)))
	require.NoError(t, order.MarkPaid(money(5700), now))
	assert.Equal(t, domain.StatusPaid, order.Status())
	assert.Equal(t, now.Add(2*time.Minute), order.UpdatedAt())
	assert.Equal(t, []string{"ordering.order_paid.v1"}, names(order.PullEvents()))
	require.ErrorIs(t, order.AwaitPayment("pay-3", now), &domain.TransitionError{})
	require.ErrorIs(t, order.Fail("late", now), &domain.TransitionError{})

	require.NoError(t, order.Cancel(domain.Actor{Kind: domain.ActorBuyer, ID: order.BuyerID().String()}, "передумал", now))
	events := order.PullEvents()
	require.Len(t, events, 1)
	cancelled, ok := events[0].(domain.OrderCancelled)
	require.True(t, ok)
	assert.True(t, cancelled.RefundRequired)
	assert.Equal(t, order.ID().String(), cancelled.AggregateID())
	assert.Len(t, order.History(), 4)
}

func paidOrder(t *testing.T) *domain.Order {
	t.Helper()
	order := placed(t)
	require.NoError(t, order.AwaitPayment("pay-1", now))
	require.NoError(t, order.MarkPaid(money(5700), now))
	order.PullEvents()
	return order
}

func TestOrderFulfilmentFlow(t *testing.T) {
	order := paidOrder(t)
	seller := domain.Actor{Kind: domain.ActorSeller, ID: "seller-1"}

	require.ErrorIs(t, order.MarkDelivered(seller, now), &domain.TransitionError{})
	require.False(t, order.ReadyToComplete(time.Hour, now))

	require.NoError(t, order.MarkShipped(seller, now))
	require.NoError(t, order.MarkShipped(seller, now))
	assert.Equal(t, domain.StatusShipped, order.Status())
	events := names(order.PullEvents())
	assert.Equal(t, []string{"ordering.order_shipped.v1"}, events)

	require.NoError(t, order.MarkDelivered(seller, now.Add(time.Hour)))
	require.NoError(t, order.MarkDelivered(seller, now.Add(time.Hour)))
	assert.Equal(t, domain.StatusDelivered, order.Status())
	assert.Equal(t, now.Add(time.Hour), order.DeliveredAt())
	assert.Equal(t, []string{"ordering.order_delivered.v1"}, names(order.PullEvents()))

	require.False(t, order.ReadyToComplete(24*time.Hour, now.Add(time.Hour)))
	require.True(t, order.ReadyToComplete(24*time.Hour, now.Add(25*time.Hour)))

	require.NoError(t, order.Complete(now.Add(25*time.Hour)))
	require.NoError(t, order.Complete(now.Add(25*time.Hour)))
	assert.Equal(t, domain.StatusCompleted, order.Status())
	completed, ok := order.PullEvents()[0].(domain.OrderCompleted)
	require.True(t, ok)
	assert.Equal(t, order.ID(), completed.Order.OrderID)
	require.ErrorIs(t, order.MarkShipped(seller, now), &domain.TransitionError{})
}

func TestOrderFulfilmentDirectFromInFulfilment(t *testing.T) {
	order := paidOrder(t)
	system := domain.Actor{Kind: domain.ActorSystem}
	require.NoError(t, order.MarkShipped(system, now))
	require.NoError(t, order.MarkDelivered(system, now))
	require.NoError(t, order.Complete(now))
}

func TestOrderCancellationRules(t *testing.T) {
	order := placed(t)
	buyer := domain.Actor{Kind: domain.ActorBuyer, ID: order.BuyerID().String()}
	require.ErrorIs(t, order.Cancel(buyer, " ", now), domain.ErrInvalidReason)
	require.ErrorIs(t, order.Cancel(domain.Actor{Kind: domain.ActorBuyer, ID: "someone"}, "x", now), domain.ErrCancellationDenied)
	require.ErrorIs(t, order.Cancel(domain.Actor{Kind: domain.ActorSupport}, "x", now), kernel.ErrInvalidID)
	require.ErrorIs(t, order.Cancel(domain.Actor{Kind: "robot", ID: "1"}, "x", now), kernel.ErrInvalidID)
	require.NoError(t, order.Cancel(buyer, "не нужен", now))
	require.NoError(t, order.Cancel(buyer, "не нужен", now))
	cancelled := order.PullEvents()[1].(domain.OrderCancelled)
	assert.False(t, cancelled.RefundRequired)
	require.ErrorIs(t, order.Fail("x", now), &domain.TransitionError{})

	failed := placed(t)
	require.NoError(t, failed.Fail("payment timeout", now))
	require.NoError(t, failed.Fail("payment timeout", now))
	require.ErrorIs(t, failed.Cancel(domain.Actor{Kind: domain.ActorSupport, ID: "agent"}, "x", now), domain.ErrCancellationDenied)
	require.ErrorIs(t, failed.Cancel(buyerOf(failed), "x", now), domain.ErrCancellationDenied)
	assert.Equal(t, []string{"ordering.order_created.v1", "ordering.order_failed.v1"}, names(failed.PullEvents()))
}

func buyerOf(order *domain.Order) domain.Actor {
	return domain.Actor{Kind: domain.ActorBuyer, ID: order.BuyerID().String()}
}

func TestStatusMachine(t *testing.T) {
	status, known := domain.ParseStatus("returning")
	assert.True(t, known)
	assert.Equal(t, domain.StatusReturning, status)
	_, known = domain.ParseStatus("completed")
	assert.True(t, known)
	_, known = domain.ParseStatus("lost")
	assert.False(t, known)

	require.NoError(t, domain.StatusShipped.CanTransitionTo(domain.StatusReturning))
	err := domain.StatusCompleted.CanTransitionTo(domain.StatusCancelled)
	var transition *domain.TransitionError
	require.True(t, errors.As(err, &transition))
	assert.Equal(t, kernel.KindConflict, transition.Kind())
	assert.Equal(t, "ORDER_INVALID_TRANSITION", transition.Code())
	assert.Contains(t, transition.Message(), "completed")

	assert.True(t, domain.StatusReturned.IsPaid())
	assert.False(t, domain.StatusAwaitingPayment.IsPaid())
	assert.True(t, domain.StatusCancelled.VisibleToSeller())
	assert.False(t, domain.StatusCreated.VisibleToSeller())
	assert.Equal(t, domain.ActorSystem, domain.System().Kind)
}

func TestOrderSnapshotRoundTrip(t *testing.T) {
	order := placed(t)
	require.NoError(t, order.AwaitPayment("pay-1", now))
	order.AdvanceVersion()
	restored, err := domain.RehydrateOrder(order.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, order.Snapshot(), restored.Snapshot())
	assert.Equal(t, 1, restored.Version())

	for name, mutate := range map[string]func(s *domain.OrderSnapshot){
		"id":       func(s *domain.OrderSnapshot) { s.ID = "x" },
		"buyer":    func(s *domain.OrderSnapshot) { s.BuyerID = "x" },
		"currency": func(s *domain.OrderSnapshot) { s.Currency = "x" },
		"status":   func(s *domain.OrderSnapshot) { s.Status = "lost" },
		"item":     func(s *domain.OrderSnapshot) { s.Items[0].SellerID = "x" },
		"part":     func(s *domain.OrderSnapshot) { s.Parts[0].SellerID = "x" },
	} {
		snap := order.Snapshot()
		mutate(&snap)
		_, err := domain.RehydrateOrder(snap)
		require.Error(t, err, name)
	}
}
