package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var now = time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)

func money(t *testing.T, amount int64) kernel.Money {
	t.Helper()
	value, err := kernel.NewMoney(amount, kernel.KZT)
	require.NoError(t, err)
	return value
}

func names(events []kernel.DomainEvent) []string {
	out := make([]string, len(events))
	for i, event := range events {
		out[i] = event.EventName()
	}
	return out
}

func initiated(t *testing.T, amount int64) *domain.Payment {
	t.Helper()
	payment, err := domain.InitiatePayment(domain.PaymentRequest{
		ID: domain.NewPaymentID(), OrderID: domain.NewOrderID(), BuyerID: kernel.NewUserID(),
		Provider: "sandbox", Amount: money(t, amount), SaveMethod: true,
	}, now)
	require.NoError(t, err)
	return payment
}

func captured(t *testing.T, amount int64) *domain.Payment {
	t.Helper()
	payment := initiated(t, amount)
	require.NoError(t, payment.AttachIntent("pi_1", "https://psp/pay/pi_1", now))
	require.NoError(t, payment.Authorize(money(t, amount), now))
	require.NoError(t, payment.Capture(money(t, amount), now))
	payment.PullEvents()
	return payment
}

func TestInitiatePaymentValidation(t *testing.T) {
	base := domain.PaymentRequest{
		ID: domain.NewPaymentID(), OrderID: domain.NewOrderID(), BuyerID: kernel.NewUserID(),
		Provider: "sandbox", Amount: money(t, 1000),
	}

	broken := base
	broken.ID = domain.PaymentID{}
	_, err := domain.InitiatePayment(broken, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	broken = base
	broken.Provider = " "
	_, err = domain.InitiatePayment(broken, now)
	require.ErrorIs(t, err, domain.ErrInvalidProvider)

	broken = base
	broken.Amount = money(t, 0)
	_, err = domain.InitiatePayment(broken, now)
	require.ErrorIs(t, err, domain.ErrInvalidAmount)

	withMethod := base
	withMethod.MethodID, withMethod.SaveMethod = domain.NewMethodID(), true
	payment, err := domain.InitiatePayment(withMethod, now)
	require.NoError(t, err)
	assert.False(t, payment.SaveMethod())
	assert.Equal(t, domain.StatusCreated, payment.Status())
}

func TestPaymentHappyPath(t *testing.T) {
	payment := initiated(t, 50000)
	assert.True(t, payment.SaveMethod())

	require.ErrorIs(t, payment.AttachIntent("", "https://psp", now), domain.ErrInvalidIntent)
	require.NoError(t, payment.AttachIntent("pi_1", "https://psp/pay/pi_1", now))
	require.NoError(t, payment.AttachIntent("pi_1", "https://psp/pay/pi_1", now))
	assert.Equal(t, domain.StatusPending, payment.Status())
	assert.Equal(t, "pi_1", payment.ProviderPaymentID())
	assert.Equal(t, "https://psp/pay/pi_1", payment.RedirectURL())

	require.ErrorIs(t, payment.Authorize(money(t, 49000), now), domain.ErrAmountMismatch)
	require.NoError(t, payment.Authorize(money(t, 50000), now))
	require.NoError(t, payment.Authorize(money(t, 50000), now))
	assert.Equal(t, money(t, 50000), payment.Authorized())

	require.ErrorIs(t, payment.Capture(money(t, 60000), now), domain.ErrCaptureExceedsHold)
	require.ErrorIs(t, payment.Capture(money(t, 0), now), domain.ErrInvalidAmount)
	require.NoError(t, payment.Capture(money(t, 50000), now))
	require.NoError(t, payment.Capture(money(t, 50000), now))
	assert.Equal(t, domain.StatusCaptured, payment.Status())

	assert.Equal(t, []string{"payment.pending.v1", "payment.authorized.v1", "payment.captured.v1"}, names(payment.PullEvents()))
}

func TestPaymentFailureAndCancellation(t *testing.T) {
	failed := initiated(t, 1000)
	require.NoError(t, failed.AttachIntent("pi_2", "https://psp/pay/pi_2", now))
	require.NoError(t, failed.Fail("card declined", now))
	require.NoError(t, failed.Fail("again", now))
	assert.Equal(t, "card declined", failed.FailureReason())
	assert.True(t, failed.Status().IsTerminal())
	require.ErrorIs(t, failed.Authorize(money(t, 1000), now), domain.ErrPaymentTerminal)
	require.ErrorIs(t, failed.Cancel("late", now), domain.ErrPaymentTerminal)

	authorized := initiated(t, 1000)
	require.NoError(t, authorized.AttachIntent("pi_3", "https://psp/pay/pi_3", now))
	require.NoError(t, authorized.Authorize(money(t, 1000), now))
	require.NoError(t, authorized.Cancel("order cancelled", now))
	require.NoError(t, authorized.Cancel("order cancelled", now))
	assert.Equal(t, domain.StatusCancelled, authorized.Status())

	pending := initiated(t, 1000)
	var transition *domain.TransitionError
	require.ErrorAs(t, pending.Capture(money(t, 1000), now), &transition)
	assert.Equal(t, "PAYMENT_INVALID_TRANSITION", transition.Code())
	assert.Equal(t, kernel.KindConflict, transition.Kind())
	assert.Contains(t, transition.Message(), "created")
	require.ErrorIs(t, pending.Capture(money(t, 1000), now), &domain.TransitionError{})
}

func TestRefunds(t *testing.T) {
	payment := captured(t, 10000)
	first := domain.NewRefundID()

	_, err := payment.RequestRefund(domain.RefundID{}, money(t, 1000), "x", now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = payment.RequestRefund(first, money(t, 0), "x", now)
	require.ErrorIs(t, err, domain.ErrInvalidAmount)
	_, err = payment.RequestRefund(first, money(t, 10001), "x", now)
	require.ErrorIs(t, err, domain.ErrRefundExceedsCharge)

	refund, err := payment.RequestRefund(first, money(t, 4000), "damaged", now)
	require.NoError(t, err)
	assert.Equal(t, domain.RefundPending, refund.Status())
	again, err := payment.RequestRefund(first, money(t, 4000), "damaged", now)
	require.NoError(t, err)
	assert.Equal(t, refund.ID(), again.ID())
	assert.Equal(t, money(t, 6000), payment.RefundableAmount())

	second := domain.NewRefundID()
	_, err = payment.RequestRefund(second, money(t, 7000), "x", now)
	require.ErrorIs(t, err, domain.ErrRefundExceedsCharge)

	require.ErrorIs(t, payment.CompleteRefund(domain.NewRefundID(), "re_x", now), domain.ErrRefundNotFound)
	require.NoError(t, payment.CompleteRefund(first, "re_1", now))
	require.NoError(t, payment.CompleteRefund(first, "re_1", now))
	assert.Equal(t, money(t, 4000), payment.Refunded())
	assert.Equal(t, domain.StatusCaptured, payment.Status())

	_, err = payment.RequestRefund(second, money(t, 6000), "rest", now)
	require.NoError(t, err)
	require.NoError(t, payment.FailRefund(second, "provider error", now))
	require.NoError(t, payment.FailRefund(second, "provider error", now))
	failedRefund, err := payment.Refund(second)
	require.NoError(t, err)
	assert.Equal(t, domain.RefundFailed, failedRefund.Status())
	assert.Equal(t, money(t, 6000), payment.RefundableAmount())

	third := domain.NewRefundID()
	_, err = payment.RequestRefund(third, money(t, 6000), "rest", now)
	require.NoError(t, err)
	require.NoError(t, payment.CompleteRefund(third, "re_3", now))
	assert.Equal(t, domain.StatusRefunded, payment.Status())
	assert.True(t, payment.RefundableAmount().IsZero())
	_, err = payment.RequestRefund(domain.NewRefundID(), money(t, 1), "x", now)
	require.Error(t, err)

	assert.Contains(t, names(payment.PullEvents()), "payment.refund_completed.v1")
	assert.Len(t, payment.Refunds(), 3)

	_, err = payment.Refund(domain.NewRefundID())
	require.ErrorIs(t, err, domain.ErrRefundNotFound)

	notCaptured := initiated(t, 100)
	_, err = notCaptured.RequestRefund(domain.NewRefundID(), money(t, 100), "x", now)
	require.ErrorIs(t, err, &domain.TransitionError{})
}

func TestPaymentSnapshotRoundTrip(t *testing.T) {
	payment := captured(t, 10000)
	_, err := payment.RequestRefund(domain.NewRefundID(), money(t, 1000), "x", now)
	require.NoError(t, err)
	payment.AdvanceVersion()

	snap := payment.Snapshot()
	restored, err := domain.RehydratePayment(snap)
	require.NoError(t, err)
	assert.Equal(t, snap, restored.Snapshot())
	assert.Equal(t, payment.BuyerID(), restored.BuyerID())
	assert.Equal(t, payment.OrderID(), restored.OrderID())
	assert.Equal(t, "sandbox", restored.Provider())
	assert.Equal(t, 1, restored.Version())

	withMethod, err := domain.InitiatePayment(domain.PaymentRequest{
		ID: domain.NewPaymentID(), OrderID: domain.NewOrderID(), BuyerID: kernel.NewUserID(),
		Provider: "sandbox", Amount: money(t, 1000), MethodID: domain.NewMethodID(),
	}, now)
	require.NoError(t, err)
	restoredMethod, err := domain.RehydratePayment(withMethod.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, withMethod.MethodID(), restoredMethod.MethodID())

	for name, mutate := range map[string]func(*domain.PaymentSnapshot){
		"id":       func(s *domain.PaymentSnapshot) { s.ID = "bad" },
		"order":    func(s *domain.PaymentSnapshot) { s.OrderID = "bad" },
		"buyer":    func(s *domain.PaymentSnapshot) { s.BuyerID = "bad" },
		"currency": func(s *domain.PaymentSnapshot) { s.Currency = "bad" },
		"status":   func(s *domain.PaymentSnapshot) { s.Status = "unknown" },
		"method":   func(s *domain.PaymentSnapshot) { s.MethodID = "bad" },
		"refund":   func(s *domain.PaymentSnapshot) { s.Refunds = []domain.RefundSnapshot{{ID: "bad"}} },
	} {
		broken := payment.Snapshot()
		broken.Refunds = append([]domain.RefundSnapshot(nil), broken.Refunds...)
		mutate(&broken)
		_, err := domain.RehydratePayment(broken)
		require.Error(t, err, name)
	}
}

func TestSavedMethod(t *testing.T) {
	buyer := kernel.NewUserID()
	_, err := domain.SaveMethod(domain.MethodID{}, buyer, "sandbox", "tok", "Visa 4242", now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.SaveMethod(domain.NewMethodID(), buyer, "sandbox", " ", "Visa 4242", now)
	require.ErrorIs(t, err, domain.ErrInvalidMethod)

	method, err := domain.SaveMethod(domain.NewMethodID(), buyer, "sandbox", "tok_1", "Visa •••• 4242", now)
	require.NoError(t, err)
	require.NoError(t, method.Usable(buyer, "sandbox"))
	require.ErrorIs(t, method.Usable(kernel.NewUserID(), "sandbox"), domain.ErrMethodNotFound)
	require.ErrorIs(t, method.Usable(buyer, "other"), domain.ErrInvalidMethod)

	require.ErrorIs(t, method.Remove(kernel.NewUserID(), now), domain.ErrNotMethodOwner)
	require.NoError(t, method.Remove(buyer, now))
	require.NoError(t, method.Remove(buyer, now))
	assert.True(t, method.IsRemoved())
	require.ErrorIs(t, method.Usable(buyer, "sandbox"), domain.ErrMethodNotFound)

	method.AdvanceVersion()
	restored, err := domain.RehydrateSavedMethod(method.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, method.Snapshot(), restored.Snapshot())
	assert.Equal(t, "tok_1", restored.Token())
	assert.Equal(t, "Visa •••• 4242", restored.Label())
	assert.Equal(t, "sandbox", restored.Provider())
	assert.Equal(t, buyer, restored.BuyerID())

	broken := method.Snapshot()
	broken.ID = "bad"
	_, err = domain.RehydrateSavedMethod(broken)
	require.Error(t, err)
	broken = method.Snapshot()
	broken.BuyerID = "bad"
	_, err = domain.RehydrateSavedMethod(broken)
	require.Error(t, err)
}

func TestEventContract(t *testing.T) {
	payment := domain.NewPaymentID()
	events := []kernel.DomainEvent{
		domain.PaymentPending{PaymentID: payment, At: now},
		domain.PaymentAuthorized{PaymentID: payment, At: now},
		domain.PaymentFailed{PaymentID: payment, At: now},
		domain.PaymentCaptured{PaymentID: payment, At: now},
		domain.PaymentCancelled{PaymentID: payment, At: now},
		domain.RefundRequested{PaymentID: payment, At: now},
		domain.RefundCompleted{PaymentID: payment, At: now},
	}
	for _, event := range events {
		assert.Equal(t, payment.String(), event.AggregateID())
		assert.Equal(t, now, event.OccurredAt())
		assert.Contains(t, event.EventName(), "payment.")
	}
}
