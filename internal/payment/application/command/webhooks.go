package command

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type HandleWebhook struct {
	Provider string
	Headers  map[string]string
	Body     []byte
}

type HandleWebhookResult struct {
	Duplicate bool
	Applied   bool
	Type      string
}

type HandleWebhookHandler struct {
	base Base
}

func NewHandleWebhookHandler(base Base) *HandleWebhookHandler {
	return &HandleWebhookHandler{base: base}
}

type pendingMethod struct {
	buyer kernel.UserID
	token string
	label string
}

type webhookOutcome struct {
	result   HandleWebhookResult
	method   *pendingMethod
	lateVoid *domain.Payment
}

func (h *HandleWebhookHandler) Handle(ctx context.Context, cmd HandleWebhook) (HandleWebhookResult, error) {
	provider, err := h.base.providers.Get(cmd.Provider)
	if err != nil {
		return HandleWebhookResult{}, err
	}
	now := h.base.clock.Now()
	event, err := provider.Verify(cmd.Headers, cmd.Body, now)
	if err != nil {
		return HandleWebhookResult{}, err
	}
	var outcome webhookOutcome
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		outcome, err = process(ctx, repos, provider.Name(), event, now)
		return err
	})
	if err != nil {
		return HandleWebhookResult{}, err
	}
	if outcome.lateVoid != nil {
		key := "late-void:" + outcome.lateVoid.ID().String()
		if err := provider.Cancel(ctx, outcome.lateVoid.ProviderPaymentID(), key); err != nil {
			return HandleWebhookResult{}, err
		}
	}
	if outcome.method != nil {
		if err := h.remember(ctx, provider.Name(), *outcome.method, now); err != nil {
			return HandleWebhookResult{}, err
		}
	}
	return outcome.result, nil
}

func process(ctx context.Context, repos application.Repositories, provider string, event application.WebhookEvent, now time.Time) (webhookOutcome, error) {
	payment, err := repos.Payments().FindByProviderID(ctx, provider, event.ProviderPaymentID)
	if err != nil {
		return webhookOutcome{}, err
	}
	applied, err := apply(payment, event, now)
	switch {
	case event.Type == application.WebhookAuthorized && errors.Is(err, domain.ErrPaymentTerminal):
		return webhookOutcome{lateVoid: payment}, nil
	case ignorable(err):
		applied = false
	case err != nil:
		return webhookOutcome{}, err
	}
	fresh, err := repos.Webhooks().Record(ctx, provider, event.ID, event.Type, now)
	if err != nil || !fresh {
		return webhookOutcome{result: HandleWebhookResult{Duplicate: !fresh}}, err
	}
	outcome := webhookOutcome{result: HandleWebhookResult{Applied: applied, Type: event.Type}}
	if !applied {
		return outcome, nil
	}
	if event.Type == application.WebhookAuthorized && payment.SaveMethod() && event.MethodToken != "" {
		outcome.method = &pendingMethod{buyer: payment.BuyerID(), token: event.MethodToken, label: event.MethodLabel}
	}
	return outcome, repos.Payments().Save(ctx, payment)
}

func ignorable(err error) bool {
	return errors.Is(err, domain.ErrPaymentTerminal) || errors.Is(err, &domain.TransitionError{}) ||
		errors.Is(err, domain.ErrAmountMismatch) || errors.Is(err, domain.ErrRefundNotFound) ||
		errors.Is(err, errUnsupportedEvent)
}

var errUnsupportedEvent = errors.New("unsupported webhook event")

func apply(payment *domain.Payment, event application.WebhookEvent, now time.Time) (bool, error) {
	switch event.Type {
	case application.WebhookAuthorized:
		if event.Currency != "" && !strings.EqualFold(event.Currency, string(payment.Amount().Currency())) {
			return false, domain.ErrAmountMismatch
		}
		amount, err := kernel.NewMoney(event.Amount, payment.Amount().Currency())
		if err != nil {
			return false, domain.ErrAmountMismatch
		}
		return true, payment.Authorize(amount, now)
	case application.WebhookFailed:
		return true, payment.Fail(event.Reason, now)
	case application.WebhookRefundSucceeded, application.WebhookRefundFailed:
		refundID, err := domain.ParseRefundID(event.RefundReference)
		if err != nil {
			return false, domain.ErrRefundNotFound
		}
		if event.Type == application.WebhookRefundFailed {
			return true, payment.FailRefund(refundID, event.Reason, now)
		}
		return true, payment.CompleteRefund(refundID, event.ProviderRefundID, now)
	default:
		return false, errUnsupportedEvent
	}
}

func (h *HandleWebhookHandler) remember(ctx context.Context, provider string, pending pendingMethod, now time.Time) error {
	return h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		_, err := repos.Methods().FindByToken(ctx, provider, pending.token)
		if err == nil {
			return nil
		}
		if !errors.Is(err, domain.ErrMethodNotFound) {
			return err
		}
		method, err := domain.SaveMethod(domain.NewMethodID(), pending.buyer, provider, pending.token, pending.label, now)
		if err != nil {
			return err
		}
		return repos.Methods().Save(ctx, method)
	})
}

type RemoveSavedMethod struct {
	Actor    auth.Principal
	MethodID string
}

type RemoveSavedMethodHandler struct {
	base Base
}

func NewRemoveSavedMethodHandler(base Base) *RemoveSavedMethodHandler {
	return &RemoveSavedMethodHandler{base: base}
}

func (h *RemoveSavedMethodHandler) Handle(ctx context.Context, cmd RemoveSavedMethod) (struct{}, error) {
	buyer, err := kernel.ParseUserID(cmd.Actor.UserID)
	if err != nil {
		return struct{}{}, auth.ErrUnauthenticated
	}
	id, err := domain.ParseMethodID(cmd.MethodID)
	if err != nil {
		return struct{}{}, domain.ErrMethodNotFound
	}
	return struct{}{}, h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		method, err := repos.Methods().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if method.BuyerID() != buyer {
			return domain.ErrMethodNotFound
		}
		if err := method.Remove(buyer, h.base.clock.Now()); err != nil {
			return err
		}
		return repos.Methods().Save(ctx, method)
	})
}

type Reconcile struct {
	Day time.Time
}

type ReconcileHandler struct {
	base    Base
	reports application.Reconciliations
}

func NewReconcileHandler(base Base, reports application.Reconciliations) *ReconcileHandler {
	return &ReconcileHandler{base: base, reports: reports}
}

func (h *ReconcileHandler) Handle(ctx context.Context, cmd Reconcile) (application.ReconciliationReport, error) {
	provider := h.base.providers.Default()
	day := time.Date(cmd.Day.Year(), cmd.Day.Month(), cmd.Day.Day(), 0, 0, 0, 0, time.UTC)
	remote, err := provider.Transactions(ctx, day)
	if err != nil {
		return application.ReconciliationReport{}, err
	}
	ledger, err := h.reports.Ledger(ctx, provider.Name(), day)
	if err != nil {
		return application.ReconciliationReport{}, err
	}
	report := application.ReconciliationReport{
		Day: day, Provider: provider.Name(), CreatedAt: h.base.clock.Now(), Mismatches: compare(ledger, remote),
	}
	report.Checked = max(len(ledger), len(remote))
	if err := h.reports.Save(ctx, report); err != nil {
		return application.ReconciliationReport{}, err
	}
	return report, nil
}

func compare(ledger []application.LedgerEntry, remote []application.ProviderTransaction) []application.Mismatch {
	byProvider := make(map[string]application.ProviderTransaction, len(remote))
	for _, tx := range remote {
		byProvider[tx.ProviderPaymentID] = tx
	}
	mismatches := []application.Mismatch{}
	seen := map[string]bool{}
	for _, entry := range ledger {
		seen[entry.ProviderPaymentID] = true
		tx, ok := byProvider[entry.ProviderPaymentID]
		if !ok {
			mismatches = append(mismatches, application.Mismatch{
				PaymentID: entry.PaymentID, ProviderPaymentID: entry.ProviderPaymentID, Field: "presence",
				Internal: entry.Status, Provider: "missing",
			})
			continue
		}
		mismatches = appendDiff(mismatches, entry, "captured", entry.Captured, tx.Captured)
		mismatches = appendDiff(mismatches, entry, "refunded", entry.Refunded, tx.Refunded)
	}
	for _, tx := range remote {
		if !seen[tx.ProviderPaymentID] && (tx.Captured > 0 || tx.Refunded > 0) {
			mismatches = append(mismatches, application.Mismatch{
				ProviderPaymentID: tx.ProviderPaymentID, Field: "presence", Internal: "missing", Provider: tx.Status,
			})
		}
	}
	return mismatches
}

func appendDiff(out []application.Mismatch, entry application.LedgerEntry, field string, internal, provider int64) []application.Mismatch {
	if internal == provider {
		return out
	}
	return append(out, application.Mismatch{
		PaymentID: entry.PaymentID, ProviderPaymentID: entry.ProviderPaymentID, Field: field,
		Internal: formatAmount(internal), Provider: formatAmount(provider),
	})
}

func formatAmount(value int64) string {
	return strconv.FormatInt(value, 10)
}
