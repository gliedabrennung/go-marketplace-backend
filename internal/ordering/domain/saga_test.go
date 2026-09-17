package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/ordering/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var errBoom = errors.New("boom")

func saga(t *testing.T, promo string) *domain.CheckoutSaga {
	t.Helper()
	s, err := domain.StartSaga(domain.SagaSpec{
		OrderID: domain.NewOrderID(), BuyerID: kernel.NewUserID(), ReservationID: "res-1", PaymentID: "pay-1",
		PromoCode: promo, Amount: money(5000), Deadline: now.Add(20 * time.Minute),
	}, now)
	require.NoError(t, err)
	return s
}

func TestSagaHappyPath(t *testing.T) {
	_, err := domain.StartSaga(domain.SagaSpec{}, now)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.StartSaga(domain.SagaSpec{OrderID: domain.NewOrderID(), BuyerID: kernel.NewUserID()}, now)
	require.ErrorIs(t, err, domain.ErrInvalidReference)

	s := saga(t, "SALE")
	assert.Equal(t, domain.StepStockReserved, s.Step())
	require.NoError(t, s.PromoRedeemed(now))
	require.ErrorIs(t, s.AwaitPayment("", now), domain.ErrInvalidReference)
	require.NoError(t, s.AwaitPayment("pay-1", now))
	assert.False(t, s.Expired(now.Add(10*time.Minute)))
	assert.True(t, s.Expired(now.Add(21*time.Minute)))

	require.ErrorIs(t, s.ChangePayment(" ", now), domain.ErrInvalidReference)
	require.NoError(t, s.ChangePayment("pay-2", now))
	ok, err := s.BeginCommit("pay-1", now)
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = s.BeginCommit("pay-2", now)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, s.Committing())
	assert.False(t, s.Expired(now.Add(time.Hour)))
	ok, _ = s.BeginCommit("pay-2", now)
	assert.True(t, ok)
	require.ErrorIs(t, s.ChangePayment("pay-3", now), domain.ErrSagaNotRunning)

	require.NoError(t, s.StockCommitted(now))
	require.NoError(t, s.PaymentCaptured(now))
	assert.True(t, s.Captured())
	s.RecordError(errBoom, now)
	assert.Equal(t, "boom", s.LastError())
	require.NoError(t, s.Complete(now))
	require.NoError(t, s.Complete(now))
	assert.Equal(t, domain.SagaCompleted, s.Status())
	assert.Empty(t, s.LastError())
	require.ErrorIs(t, s.StockCommitted(now), domain.ErrSagaNotRunning)
	require.ErrorIs(t, s.Resume(now), domain.ErrSagaNotManual)
	ok, _ = s.BeginCommit("pay-2", now)
	assert.False(t, ok)
	assert.Nil(t, s.Pending())

	require.ErrorIs(t, s.BeginCompensation("cancel", "", now), domain.ErrInvalidReference)
	require.NoError(t, s.BeginCompensation("cancelled by buyer", "refund-1", now))
	require.NoError(t, s.BeginCompensation("again", "refund-2", now))
	assert.Equal(t, "refund-1", s.RefundID())
	assert.Equal(t, "cancelled by buyer", s.Reason())
	assert.Equal(t, []domain.Step{
		domain.CompensationRefund, domain.CompensationStock, domain.CompensationPromo, domain.CompensationCloseOrder,
	}, s.Pending())
}

func TestSagaCompensationAndManualIntervention(t *testing.T) {
	s := saga(t, "")
	ok, err := s.BeginCommit("pay-1", now)
	require.NoError(t, err)
	assert.False(t, ok)
	require.NoError(t, s.BeginCompensation("payment timeout", "refund-1", now))
	assert.Equal(t, []domain.Step{domain.CompensationCancel, domain.CompensationStock, domain.CompensationCloseOrder}, s.Pending())
	require.ErrorIs(t, s.Complete(now), domain.ErrSagaNotRunning)

	s.Compensated(domain.CompensationCancel, now)
	s.Compensated(domain.CompensationCancel, now)
	for attempt := 1; attempt < 3; attempt++ {
		s.CompensationFailed(errBoom, 3, now)
		assert.Equal(t, domain.SagaCompensating, s.Status())
	}
	s.CompensationFailed(errBoom, 3, now)
	assert.Equal(t, domain.SagaManual, s.Status())
	assert.Equal(t, 3, s.Attempts())
	assert.Nil(t, s.Pending())

	require.NoError(t, s.Resume(now))
	assert.Equal(t, 0, s.Attempts())
	s.Compensated(domain.CompensationStock, now)
	s.Compensated(domain.CompensationCloseOrder, now)
	assert.Equal(t, domain.SagaCompensated, s.Status())
	require.NoError(t, s.BeginCompensation("again", "refund", now))
	assert.Equal(t, domain.SagaCompensated, s.Status())
	assert.False(t, s.Expired(now.Add(time.Hour)))
}

func TestSagaSnapshotRoundTrip(t *testing.T) {
	s := saga(t, "SALE")
	require.NoError(t, s.AwaitPayment("pay-1", now))
	require.NoError(t, s.BeginCompensation("timeout", "refund-1", now))
	s.Compensated(domain.CompensationCancel, now)
	s.AdvanceVersion()

	restored, err := domain.RehydrateSaga(s.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, s.Snapshot(), restored.Snapshot())
	assert.Equal(t, s.OrderID(), restored.OrderID())
	assert.Equal(t, s.BuyerID(), restored.BuyerID())
	assert.Equal(t, "res-1", restored.ReservationID())
	assert.Equal(t, "pay-1", restored.PaymentID())
	assert.Equal(t, "SALE", restored.PromoCode())
	assert.Equal(t, int64(5000), restored.Amount().Amount())
	assert.Equal(t, now.Add(20*time.Minute), restored.Deadline())
	assert.Equal(t, 1, restored.Version())

	for name, mutate := range map[string]func(s *domain.SagaSnapshot){
		"id":       func(s *domain.SagaSnapshot) { s.OrderID = "x" },
		"buyer":    func(s *domain.SagaSnapshot) { s.BuyerID = "x" },
		"currency": func(s *domain.SagaSnapshot) { s.Currency = "x" },
		"status":   func(s *domain.SagaSnapshot) { s.Status = "lost" },
	} {
		snap := s.Snapshot()
		mutate(&snap)
		_, err := domain.RehydrateSaga(snap)
		require.Error(t, err, name)
	}
}
