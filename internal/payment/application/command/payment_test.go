package command_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/infrastructure/psp"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
)

var (
	ctx     = context.Background()
	errDown = errors.New("provider down")
)

type fakeProvider struct {
	mu           sync.Mutex
	name         string
	intents      map[string]application.IntentRequest
	calls        []string
	failCreate   error
	failCapture  error
	failRefund   error
	failCancel   error
	transactions []application.ProviderTransaction
}

func newProvider() *fakeProvider {
	return &fakeProvider{name: "sandbox", intents: map[string]application.IntentRequest{}}
}

func (p *fakeProvider) record(call string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, call)
}

func (p *fakeProvider) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

func (p *fakeProvider) Name() string { return p.name }

func (p *fakeProvider) CreateIntent(_ context.Context, request application.IntentRequest) (application.Intent, error) {
	p.record("create:" + request.PaymentID)
	if p.failCreate != nil {
		return application.Intent{}, p.failCreate
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	id := "pi_" + request.PaymentID
	p.intents[id] = request
	return application.Intent{ProviderPaymentID: id, RedirectURL: "https://psp.example/pay/" + id}, nil
}

func (p *fakeProvider) Capture(_ context.Context, id string, amount kernel.Money, key string) error {
	p.record(fmt.Sprintf("capture:%s:%d:%s", id, amount.Amount(), key))
	return p.failCapture
}

func (p *fakeProvider) Cancel(_ context.Context, id, key string) error {
	p.record("cancel:" + id + ":" + key)
	return p.failCancel
}

func (p *fakeProvider) Refund(_ context.Context, id string, amount kernel.Money, key string) (string, error) {
	p.record(fmt.Sprintf("refund:%s:%d:%s", id, amount.Amount(), key))
	if p.failRefund != nil {
		return "", p.failRefund
	}
	return "re_" + key, nil
}

func (p *fakeProvider) Transactions(context.Context, time.Time) ([]application.ProviderTransaction, error) {
	return p.transactions, nil
}

func (p *fakeProvider) Verify(headers map[string]string, body []byte, _ time.Time) (application.WebhookEvent, error) {
	if headers["signature"] != "valid" {
		return application.WebhookEvent{}, application.ErrInvalidSignature
	}
	var event application.WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return application.WebhookEvent{}, application.ErrInvalidWebhook
	}
	return event, nil
}

type env struct {
	store    *memory.Store
	reads    *memory.ReadModel
	clock    *clock.Manual
	provider *fakeProvider

	create    *command.CreatePaymentHandler
	capture   *command.CapturePaymentHandler
	cancel    *command.CancelPaymentHandler
	refund    *command.RefundPaymentHandler
	webhook   *command.HandleWebhookHandler
	remove    *command.RemoveSavedMethodHandler
	reconcile *command.ReconcileHandler

	get            *query.GetPaymentHandler
	methods        *query.ListSavedMethodsHandler
	reconciliation *query.GetReconciliationHandler
}

func newEnv() *env {
	store := memory.NewStore()
	e := &env{
		store: store, reads: memory.NewReadModel(store), provider: newProvider(),
		clock: clock.NewManual(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)),
	}
	base := command.NewBase(memory.NewUnitOfWork(store), e.clock, psp.NewRegistry(e.provider))
	e.create = command.NewCreatePaymentHandler(base)
	e.capture = command.NewCapturePaymentHandler(base)
	e.cancel = command.NewCancelPaymentHandler(base)
	e.refund = command.NewRefundPaymentHandler(base)
	e.webhook = command.NewHandleWebhookHandler(base)
	e.remove = command.NewRemoveSavedMethodHandler(base)
	e.reconcile = command.NewReconcileHandler(base, memory.NewReconciliations(store))
	e.get = query.NewGetPaymentHandler(e.reads)
	e.methods = query.NewListSavedMethodsHandler(e.reads)
	e.reconciliation = query.NewGetReconciliationHandler(e.reads)
	return e
}

type order struct {
	payment string
	buyer   auth.Principal
}

func buyer() auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer"}}
}

func (e *env) pay(t *testing.T, amount int64, save bool, method string, who auth.Principal) (order, command.CreatePaymentResult) {
	t.Helper()
	o := order{payment: domain.NewPaymentID().String(), buyer: who}
	result, err := e.create.Handle(ctx, command.CreatePayment{
		PaymentID: o.payment, OrderID: domain.NewOrderID().String(), BuyerID: who.UserID, Amount: amount,
		Currency: "KZT", ReturnURL: "https://shop.example/return", SaveMethod: save, MethodID: method,
	})
	require.NoError(t, err)
	return o, result
}

func (e *env) send(t *testing.T, event application.WebhookEvent) (command.HandleWebhookResult, error) {
	t.Helper()
	if event.ID == "" {
		event.ID = "evt_" + kernel.NewUserID().String()
	}
	body, err := json.Marshal(event)
	require.NoError(t, err)
	return e.webhook.Handle(ctx, command.HandleWebhook{Provider: "sandbox", Headers: map[string]string{"signature": "valid"}, Body: body})
}

func (e *env) authorize(t *testing.T, o order, amount int64, token string) {
	t.Helper()
	result, err := e.send(t, application.WebhookEvent{
		Type: application.WebhookAuthorized, ProviderPaymentID: "pi_" + o.payment, Amount: amount, Currency: "KZT",
		MethodToken: token, MethodLabel: "Visa 4242",
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
}

func (e *env) status(t *testing.T, o order) query.PaymentView {
	t.Helper()
	view, err := e.get.Handle(ctx, query.GetPayment{Actor: o.buyer, PaymentID: o.payment})
	require.NoError(t, err)
	return view
}

func eventNames(events []kernel.DomainEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.EventName())
	}
	return out
}

func TestCheckoutPaymentLifecycle(t *testing.T) {
	e := newEnv()
	o, created := e.pay(t, 25000, true, "", buyer())
	assert.Equal(t, "pending", created.Status)
	assert.Equal(t, "https://psp.example/pay/pi_"+o.payment, created.RedirectURL)

	again, err := e.create.Handle(ctx, command.CreatePayment{
		PaymentID: o.payment, OrderID: domain.NewOrderID().String(), BuyerID: o.buyer.UserID, Amount: 25000, Currency: "KZT",
	})
	require.NoError(t, err)
	assert.Equal(t, created, again)
	assert.Len(t, e.provider.Calls(), 1)

	e.authorize(t, o, 25000, "tok_1")
	_, err = e.capture.Handle(ctx, command.CapturePayment{PaymentID: o.payment})
	require.NoError(t, err)
	_, err = e.capture.Handle(ctx, command.CapturePayment{PaymentID: o.payment})
	require.NoError(t, err)

	refunded, err := e.refund.Handle(ctx, command.RefundPayment{
		PaymentID: o.payment, RefundID: domain.NewRefundID().String(), Amount: 5000, Reason: "damaged",
	})
	require.NoError(t, err)
	assert.Equal(t, "succeeded", refunded.Status)
	full := domain.NewRefundID().String()
	rest, err := e.refund.Handle(ctx, command.RefundPayment{PaymentID: o.payment, RefundID: full, Reason: "cancelled"})
	require.NoError(t, err)
	assert.Equal(t, int64(20000), rest.Amount)
	repeat, err := e.refund.Handle(ctx, command.RefundPayment{PaymentID: o.payment, RefundID: full})
	require.NoError(t, err)
	assert.Equal(t, rest, repeat)

	view := e.status(t, o)
	assert.Equal(t, "refunded", view.Status)
	assert.Equal(t, int64(25000), view.Refunded)
	assert.Len(t, view.Refunds, 2)

	assert.Equal(t, []string{
		"payment.pending.v1", "payment.authorized.v1", "payment.captured.v1",
		"payment.refund_requested.v1", "payment.refund_completed.v1",
		"payment.refund_requested.v1", "payment.refund_completed.v1",
	}, eventNames(e.store.Events()))

	methods, err := e.methods.Handle(ctx, query.ListSavedMethods{Actor: o.buyer})
	require.NoError(t, err)
	require.Len(t, methods, 1)
	assert.Equal(t, "Visa 4242", methods[0].Label)
}

func TestSavedMethodReuseAndRemoval(t *testing.T) {
	e := newEnv()
	who := buyer()
	first, _ := e.pay(t, 1000, true, "", who)
	e.authorize(t, first, 1000, "tok_saved")
	e.authorize(t, first, 1000, "tok_saved")

	methods, err := e.methods.Handle(ctx, query.ListSavedMethods{Actor: who})
	require.NoError(t, err)
	require.Len(t, methods, 1)

	e.pay(t, 2000, false, methods[0].ID, who)
	e.provider.mu.Lock()
	var tokens []string
	for _, intent := range e.provider.intents {
		tokens = append(tokens, intent.MethodToken)
	}
	e.provider.mu.Unlock()
	assert.Contains(t, tokens, "tok_saved")

	stranger := buyer()
	_, err = e.create.Handle(ctx, command.CreatePayment{
		PaymentID: domain.NewPaymentID().String(), OrderID: domain.NewOrderID().String(), BuyerID: stranger.UserID,
		Amount: 10, Currency: "KZT", MethodID: methods[0].ID,
	})
	require.ErrorIs(t, err, domain.ErrMethodNotFound)
	_, err = e.remove.Handle(ctx, command.RemoveSavedMethod{Actor: stranger, MethodID: methods[0].ID})
	require.ErrorIs(t, err, domain.ErrMethodNotFound)
	_, err = e.remove.Handle(ctx, command.RemoveSavedMethod{Actor: who, MethodID: "bogus"})
	require.ErrorIs(t, err, domain.ErrMethodNotFound)
	_, err = e.remove.Handle(ctx, command.RemoveSavedMethod{Actor: auth.Principal{}, MethodID: methods[0].ID})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)

	_, err = e.remove.Handle(ctx, command.RemoveSavedMethod{Actor: who, MethodID: methods[0].ID})
	require.NoError(t, err)
	methods, err = e.methods.Handle(ctx, query.ListSavedMethods{Actor: who})
	require.NoError(t, err)
	assert.Empty(t, methods)
	_, err = e.methods.Handle(ctx, query.ListSavedMethods{Actor: auth.Principal{}})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}

func TestCreatePaymentValidationAndProviderFailure(t *testing.T) {
	e := newEnv()
	who := buyer()
	valid := command.CreatePayment{
		PaymentID: domain.NewPaymentID().String(), OrderID: domain.NewOrderID().String(), BuyerID: who.UserID,
		Amount: 100, Currency: "KZT",
	}
	broken := []func(c *command.CreatePayment){
		func(c *command.CreatePayment) { c.PaymentID = "x" },
		func(c *command.CreatePayment) { c.OrderID = "x" },
		func(c *command.CreatePayment) { c.BuyerID = "x" },
		func(c *command.CreatePayment) { c.Currency = "kz" },
		func(c *command.CreatePayment) { c.Amount = 0 },
		func(c *command.CreatePayment) { c.MethodID = "x" },
		func(c *command.CreatePayment) { c.MethodID = domain.NewMethodID().String() },
	}
	for i, mutate := range broken {
		cmd := valid
		mutate(&cmd)
		_, err := e.create.Handle(ctx, cmd)
		require.Error(t, err, i)
	}

	e.provider.failCreate = errDown
	_, err := e.create.Handle(ctx, valid)
	require.ErrorIs(t, err, errDown)
	view, err := e.get.Handle(ctx, query.GetPayment{Actor: who, PaymentID: valid.PaymentID})
	require.NoError(t, err)
	assert.Equal(t, "failed", view.Status)
	assert.Contains(t, view.FailureReason, "provider down")

	e.provider.failCreate = nil
	result, err := e.create.Handle(ctx, valid)
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Status)
}

func TestCaptureAndCancel(t *testing.T) {
	e := newEnv()
	o, _ := e.pay(t, 900, false, "", buyer())

	_, err := e.capture.Handle(ctx, command.CapturePayment{PaymentID: o.payment})
	require.ErrorIs(t, err, &domain.TransitionError{})
	_, err = e.capture.Handle(ctx, command.CapturePayment{PaymentID: "bogus"})
	require.ErrorIs(t, err, domain.ErrPaymentNotFound)

	e.authorize(t, o, 900, "")
	e.provider.failCapture = errDown
	_, err = e.capture.Handle(ctx, command.CapturePayment{PaymentID: o.payment, Amount: 900})
	require.ErrorIs(t, err, errDown)
	assert.Equal(t, "authorized", e.status(t, o).Status)

	e.provider.failCapture = nil
	_, err = e.capture.Handle(ctx, command.CapturePayment{PaymentID: o.payment, Amount: 1000})
	require.ErrorIs(t, err, domain.ErrCaptureExceedsHold)

	_, err = e.cancel.Handle(ctx, command.CancelPayment{PaymentID: o.payment, Reason: "stock"})
	require.NoError(t, err)
	_, err = e.cancel.Handle(ctx, command.CancelPayment{PaymentID: o.payment, Reason: "stock"})
	require.NoError(t, err)
	assert.Equal(t, "cancelled", e.status(t, o).Status)
	assert.Contains(t, e.provider.Calls(), "cancel:pi_"+o.payment+":cancel:"+o.payment)

	pending, _ := e.pay(t, 500, false, "", buyer())
	e.provider.failCancel = errDown
	_, err = e.cancel.Handle(ctx, command.CancelPayment{PaymentID: pending.payment})
	require.ErrorIs(t, err, errDown)
	assert.Equal(t, "pending", e.status(t, pending).Status)

	captured, _ := e.pay(t, 500, false, "", buyer())
	e.authorize(t, captured, 500, "")
	_, err = e.capture.Handle(ctx, command.CapturePayment{PaymentID: captured.payment})
	require.NoError(t, err)
	_, err = e.cancel.Handle(ctx, command.CancelPayment{PaymentID: captured.payment})
	require.ErrorIs(t, err, &domain.TransitionError{})
}

func TestRefundFailures(t *testing.T) {
	e := newEnv()
	o, _ := e.pay(t, 1000, false, "", buyer())
	_, err := e.refund.Handle(ctx, command.RefundPayment{PaymentID: o.payment, RefundID: domain.NewRefundID().String()})
	require.ErrorIs(t, err, &domain.TransitionError{})
	_, err = e.refund.Handle(ctx, command.RefundPayment{PaymentID: o.payment, RefundID: "x"})
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	e.authorize(t, o, 1000, "")
	_, err = e.capture.Handle(ctx, command.CapturePayment{PaymentID: o.payment})
	require.NoError(t, err)

	e.provider.failRefund = application.ErrProviderRejected
	failed := domain.NewRefundID().String()
	_, err = e.refund.Handle(ctx, command.RefundPayment{PaymentID: o.payment, RefundID: failed, Amount: 400})
	require.ErrorIs(t, err, application.ErrProviderRejected)
	view := e.status(t, o)
	require.Len(t, view.Refunds, 1)
	assert.Equal(t, "failed", view.Refunds[0].Status)

	e.provider.failRefund = nil
	_, err = e.refund.Handle(ctx, command.RefundPayment{PaymentID: o.payment, RefundID: domain.NewRefundID().String(), Amount: 1001})
	require.ErrorIs(t, err, domain.ErrRefundExceedsCharge)
	_, err = e.refund.Handle(ctx, command.RefundPayment{PaymentID: o.payment, RefundID: domain.NewRefundID().String()})
	require.NoError(t, err)
	assert.Equal(t, "refunded", e.status(t, o).Status)
}

func TestWebhookHandling(t *testing.T) {
	e := newEnv()
	o, _ := e.pay(t, 700, false, "", buyer())

	_, err := e.webhook.Handle(ctx, command.HandleWebhook{Provider: "sandbox", Body: []byte(`{}`)})
	require.ErrorIs(t, err, application.ErrInvalidSignature)
	_, err = e.webhook.Handle(ctx, command.HandleWebhook{Provider: "stripe"})
	require.ErrorIs(t, err, application.ErrUnknownProvider)
	_, err = e.send(t, application.WebhookEvent{Type: application.WebhookAuthorized, ProviderPaymentID: "pi_unknown", Amount: 700})
	require.ErrorIs(t, err, domain.ErrPaymentNotFound)

	mismatch, err := e.send(t, application.WebhookEvent{
		Type: application.WebhookAuthorized, ProviderPaymentID: "pi_" + o.payment, Amount: 700, Currency: "USD",
	})
	require.NoError(t, err)
	assert.False(t, mismatch.Applied)
	unknown, err := e.send(t, application.WebhookEvent{Type: "payment.disputed", ProviderPaymentID: "pi_" + o.payment})
	require.NoError(t, err)
	assert.False(t, unknown.Applied)

	event := application.WebhookEvent{
		ID: "evt_failed", Type: application.WebhookFailed, ProviderPaymentID: "pi_" + o.payment, Reason: "declined",
	}
	first, err := e.send(t, event)
	require.NoError(t, err)
	assert.Equal(t, command.HandleWebhookResult{Applied: true, Type: application.WebhookFailed}, first)
	second, err := e.send(t, event)
	require.NoError(t, err)
	assert.Equal(t, command.HandleWebhookResult{Duplicate: true}, second)
	assert.Equal(t, "failed", e.status(t, o).Status)

	late, err := e.send(t, application.WebhookEvent{
		Type: application.WebhookAuthorized, ProviderPaymentID: "pi_" + o.payment, Amount: 700, Currency: "KZT",
	})
	require.NoError(t, err)
	assert.False(t, late.Applied)
	assert.Contains(t, e.provider.Calls(), "cancel:pi_"+o.payment+":late-void:"+o.payment)

	e.provider.failCancel = errDown
	_, err = e.send(t, application.WebhookEvent{
		Type: application.WebhookAuthorized, ProviderPaymentID: "pi_" + o.payment, Amount: 700, Currency: "KZT",
	})
	require.ErrorIs(t, err, errDown)
}

func TestRefundWebhooks(t *testing.T) {
	e := newEnv()
	o, _ := e.pay(t, 1000, false, "", buyer())
	e.authorize(t, o, 1000, "")
	_, err := e.capture.Handle(ctx, command.CapturePayment{PaymentID: o.payment})
	require.NoError(t, err)

	id, err := domain.ParsePaymentID(o.payment)
	require.NoError(t, err)
	payment, err := e.store.Payments().FindByID(ctx, id)
	require.NoError(t, err)
	pendingOK, pendingFail := domain.NewRefundID(), domain.NewRefundID()
	_, err = payment.RequestRefund(pendingOK, kernel.MustMoney(600, kernel.KZT), "a", e.clock.Now())
	require.NoError(t, err)
	_, err = payment.RequestRefund(pendingFail, kernel.MustMoney(400, kernel.KZT), "b", e.clock.Now())
	require.NoError(t, err)
	require.NoError(t, e.store.Payments().Save(ctx, payment))

	ok, err := e.send(t, application.WebhookEvent{
		Type: application.WebhookRefundSucceeded, ProviderPaymentID: "pi_" + o.payment,
		RefundReference: pendingOK.String(), ProviderRefundID: "re_1",
	})
	require.NoError(t, err)
	assert.True(t, ok.Applied)
	failed, err := e.send(t, application.WebhookEvent{
		Type: application.WebhookRefundFailed, ProviderPaymentID: "pi_" + o.payment,
		RefundReference: pendingFail.String(), Reason: "insufficient funds",
	})
	require.NoError(t, err)
	assert.True(t, failed.Applied)
	stray, err := e.send(t, application.WebhookEvent{
		Type: application.WebhookRefundSucceeded, ProviderPaymentID: "pi_" + o.payment, RefundReference: "bogus",
	})
	require.NoError(t, err)
	assert.False(t, stray.Applied)

	view := e.status(t, o)
	assert.Equal(t, "captured", view.Status)
	assert.Equal(t, int64(600), view.Refunded)
}

func TestPaymentVisibility(t *testing.T) {
	e := newEnv()
	o, _ := e.pay(t, 100, false, "", buyer())

	_, err := e.get.Handle(ctx, query.GetPayment{Actor: buyer(), PaymentID: o.payment})
	require.ErrorIs(t, err, domain.ErrPaymentNotFound)
	_, err = e.get.Handle(ctx, query.GetPayment{Actor: auth.Principal{}, PaymentID: o.payment})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.get.Handle(ctx, query.GetPayment{Actor: o.buyer, PaymentID: "bogus"})
	require.ErrorIs(t, err, domain.ErrPaymentNotFound)
	_, err = e.get.Handle(ctx, query.GetPayment{Actor: o.buyer, PaymentID: domain.NewPaymentID().String()})
	require.ErrorIs(t, err, domain.ErrPaymentNotFound)

	support := auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"support_agent"}}
	view, err := e.get.Handle(ctx, query.GetPayment{Actor: support, PaymentID: o.payment})
	require.NoError(t, err)
	assert.Equal(t, o.payment, view.ID)

	orders, err := e.reads.OrderPayments(ctx, view.OrderID)
	require.NoError(t, err)
	assert.Len(t, orders, 1)
}

func TestReconciliation(t *testing.T) {
	e := newEnv()
	matched, _ := e.pay(t, 1000, false, "", buyer())
	e.authorize(t, matched, 1000, "")
	_, err := e.capture.Handle(ctx, command.CapturePayment{PaymentID: matched.payment})
	require.NoError(t, err)
	drifted, _ := e.pay(t, 500, false, "", buyer())
	e.authorize(t, drifted, 500, "")
	_, err = e.capture.Handle(ctx, command.CapturePayment{PaymentID: drifted.payment})
	require.NoError(t, err)
	lost, _ := e.pay(t, 300, false, "", buyer())

	e.provider.transactions = []application.ProviderTransaction{
		{ProviderPaymentID: "pi_" + matched.payment, Status: "captured", Captured: 1000, Currency: "KZT"},
		{ProviderPaymentID: "pi_" + drifted.payment, Status: "refunded", Captured: 500, Refunded: 500, Currency: "KZT"},
		{ProviderPaymentID: "pi_foreign", Status: "captured", Captured: 42, Currency: "KZT"},
	}
	report, err := e.reconcile.Handle(ctx, command.Reconcile{Day: e.clock.Now().Add(3 * time.Hour)})
	require.NoError(t, err)
	assert.Equal(t, 3, report.Checked)
	fields := map[string]string{}
	for _, m := range report.Mismatches {
		fields[m.ProviderPaymentID] = m.Field
	}
	assert.Equal(t, map[string]string{
		"pi_" + drifted.payment: "refunded", "pi_" + lost.payment: "presence", "pi_foreign": "presence",
	}, fields)

	admin := auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"platform_admin"}}
	view, err := e.reconciliation.Handle(ctx, query.GetReconciliation{Actor: admin, Provider: "sandbox", Day: report.Day})
	require.NoError(t, err)
	assert.Len(t, view.Mismatches, 3)
	_, err = e.reconciliation.Handle(ctx, query.GetReconciliation{Actor: buyer(), Provider: "sandbox", Day: report.Day})
	require.Error(t, err)
	_, err = e.reconciliation.Handle(ctx, query.GetReconciliation{Actor: admin, Provider: "sandbox", Day: report.Day.AddDate(0, 0, -1)})
	require.ErrorIs(t, err, query.ErrReportNotFound)
}
