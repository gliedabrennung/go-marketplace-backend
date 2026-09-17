//go:build integration

package payment_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

var paymentTables = []string{
	"payment.refunds", "payment.payments", "payment.saved_methods", "payment.webhook_events",
	"payment.reconciliation_reports", "platform.outbox", "platform.idempotency_keys",
}

func implementations() map[string]func(t *testing.T) application.Repositories {
	return map[string]func(t *testing.T) application.Repositories{
		"memory": func(*testing.T) application.Repositories { return memory.NewStore() },
		"postgres": func(t *testing.T) application.Repositories {
			pool := testdb.Pool(t)
			testdb.Truncate(t, pool, paymentTables...)
			return postgres.NewRepositories(pool, postgres.NewOutboxWriter())
		},
	}
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func money(amount int64) kernel.Money {
	return kernel.MustMoney(amount, kernel.KZT)
}

func TestPaymentRepositoryContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			_, err := repos.Payments().FindByID(ctx, domain.NewPaymentID())
			require.ErrorIs(t, err, domain.ErrPaymentNotFound)
			_, err = repos.Payments().FindByProviderID(ctx, "sandbox", "pi_missing")
			require.ErrorIs(t, err, domain.ErrPaymentNotFound)

			at := now()
			payment, err := domain.InitiatePayment(domain.PaymentRequest{
				ID: domain.NewPaymentID(), OrderID: domain.NewOrderID(), BuyerID: kernel.NewUserID(),
				Provider: "sandbox", Amount: money(5000), SaveMethod: true,
			}, at)
			require.NoError(t, err)
			require.NoError(t, repos.Payments().Save(ctx, payment))
			_, err = repos.Payments().FindByProviderID(ctx, "sandbox", "")
			require.ErrorIs(t, err, domain.ErrPaymentNotFound)

			require.NoError(t, payment.AttachIntent("pi_contract_"+name, "https://psp/pay", at))
			require.NoError(t, payment.Authorize(money(5000), at))
			require.NoError(t, payment.Capture(money(5000), at))
			refund := domain.NewRefundID()
			_, err = payment.RequestRefund(refund, money(2000), "damaged", at)
			require.NoError(t, err)
			require.NoError(t, repos.Payments().Save(ctx, payment))

			reloaded, err := repos.Payments().FindByProviderID(ctx, "sandbox", "pi_contract_"+name)
			require.NoError(t, err)
			assert.Equal(t, payment.Snapshot(), reloaded.Snapshot())

			require.NoError(t, reloaded.CompleteRefund(refund, "re_1", at.Add(time.Second)))
			require.NoError(t, repos.Payments().Save(ctx, reloaded))
			again, err := repos.Payments().FindByID(ctx, payment.ID())
			require.NoError(t, err)
			assert.Equal(t, reloaded.Snapshot(), again.Snapshot())
			assert.Equal(t, int64(2000), again.Refunded().Amount())

			require.ErrorIs(t, repos.Payments().Save(ctx, payment), kernel.ErrConcurrentModification)
		})
	}
}

func TestMethodAndWebhookContract(t *testing.T) {
	ctx := context.Background()
	for name, factory := range implementations() {
		t.Run(name, func(t *testing.T) {
			repos := factory(t)
			buyer := kernel.NewUserID()
			at := now()
			method, err := domain.SaveMethod(domain.NewMethodID(), buyer, "sandbox", "tok_"+name, "Visa 4242", at)
			require.NoError(t, err)
			require.NoError(t, repos.Methods().Save(ctx, method))

			byToken, err := repos.Methods().FindByToken(ctx, "sandbox", "tok_"+name)
			require.NoError(t, err)
			assert.Equal(t, method.Snapshot(), byToken.Snapshot())
			_, err = repos.Methods().FindByToken(ctx, "sandbox", "tok_other")
			require.ErrorIs(t, err, domain.ErrMethodNotFound)
			_, err = repos.Methods().FindByID(ctx, domain.NewMethodID())
			require.ErrorIs(t, err, domain.ErrMethodNotFound)

			require.NoError(t, byToken.Remove(buyer, at))
			require.NoError(t, repos.Methods().Save(ctx, byToken))
			removed, err := repos.Methods().FindByID(ctx, method.ID())
			require.NoError(t, err)
			assert.True(t, removed.IsRemoved())
			require.ErrorIs(t, repos.Methods().Save(ctx, method), kernel.ErrConcurrentModification)

			fresh, err := repos.Webhooks().Record(ctx, "sandbox", "evt_1", "payment.authorized", at)
			require.NoError(t, err)
			assert.True(t, fresh)
			fresh, err = repos.Webhooks().Record(ctx, "sandbox", "evt_1", "payment.authorized", at)
			require.NoError(t, err)
			assert.False(t, fresh)
			fresh, err = repos.Webhooks().Record(ctx, "other", "evt_1", "payment.authorized", at)
			require.NoError(t, err)
			assert.True(t, fresh)
		})
	}
}
