package domain_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func legal(t *testing.T, form string) domain.LegalDetails {
	t.Helper()
	taxID := validBIN
	if form == "sole_proprietor" {
		taxID = validIIN
	}
	l, err := domain.NewLegalDetails(form, "ТОО Ромашка", taxID, "г. Алматы, пр. Абая 1")
	require.NoError(t, err)
	return l
}

func bank(t *testing.T, iban string) domain.BankAccount {
	t.Helper()
	b, err := domain.NewBankAccount(iban, "HSBKKZKX", "Halyk Bank", "ТОО Ромашка")
	require.NoError(t, err)
	return b
}

func doc(t *testing.T, kind string) domain.Document {
	t.Helper()
	d, err := domain.NewDocument(kind, "sellers/docs/"+kind+".pdf", at)
	require.NoError(t, err)
	return d
}

func draft(t *testing.T, form string) (*domain.Seller, kernel.UserID) {
	t.Helper()
	owner := kernel.NewUserID()
	s, err := domain.OpenApplication(kernel.NewSellerID(), owner, legal(t, form), at)
	require.NoError(t, err)
	return s, owner
}

func readyForReview(t *testing.T) (*domain.Seller, kernel.UserID) {
	t.Helper()
	s, owner := draft(t, "legal_entity")
	require.NoError(t, s.ChangeBankAccount(owner, bank(t, validIBAN), at))
	for _, kind := range []string{"registration_certificate", "bank_confirmation", "charter"} {
		require.NoError(t, s.AttachDocument(owner, doc(t, kind), at))
	}
	return s, owner
}

func active(t *testing.T) (*domain.Seller, kernel.UserID) {
	t.Helper()
	s, owner := readyForReview(t)
	require.NoError(t, s.SubmitForReview(owner, at))
	require.NoError(t, s.Approve(kernel.NewUserID(), at))
	s.PullEvents()
	return s, owner
}

func names(events []kernel.DomainEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.EventName()
	}
	return out
}

func TestOpenApplication(t *testing.T) {
	s, owner := draft(t, "legal_entity")
	assert.Equal(t, domain.StatusDraft, s.Status())
	assert.Equal(t, owner, s.OwnerID())
	role, ok := s.MemberRole(owner)
	assert.True(t, ok)
	assert.Equal(t, domain.MemberAdmin, role)
	assert.False(t, s.CanSell())
	assert.False(t, s.PayoutsAllowed())
	assert.Equal(t, validBIN, s.Legal().TaxID().String())
	assert.Equal(t, []string{"seller.application_opened.v1"}, names(s.PullEvents()))

	_, err := domain.OpenApplication(kernel.SellerID{}, owner, legal(t, "legal_entity"), at)
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = domain.OpenApplication(kernel.NewSellerID(), owner, domain.LegalDetails{}, at)
	require.ErrorIs(t, err, domain.ErrInvalidTaxID)
}

func TestSubmitForReview_RequiresCompleteApplication(t *testing.T) {
	s, owner := draft(t, "legal_entity")
	err := s.SubmitForReview(owner, at)
	require.ErrorIs(t, err, domain.ErrApplicationIncomplete)

	var fields []string
	var withFields interface {
		Fields() []kernel.FieldViolation
	}
	require.ErrorAs(t, err, &withFields)
	for _, f := range withFields.Fields() {
		fields = append(fields, f.Field)
	}
	assert.ElementsMatch(t, []string{"bank_account", "documents.registration_certificate", "documents.bank_confirmation", "documents.charter"}, fields)

	proprietor, ownerIP := draft(t, "sole_proprietor")
	require.NoError(t, proprietor.ChangeBankAccount(ownerIP, bank(t, validIBAN), at))
	require.NoError(t, proprietor.AttachDocument(ownerIP, doc(t, "registration_certificate"), at))
	require.NoError(t, proprietor.AttachDocument(ownerIP, doc(t, "bank_confirmation"), at))
	require.NoError(t, proprietor.SubmitForReview(ownerIP, at), "charter is not required for sole proprietor")
}

func TestSellerLifecycle_RejectThenApprove(t *testing.T) {
	s, owner := readyForReview(t)
	moderator := kernel.NewUserID()
	s.PullEvents()

	require.NoError(t, s.SubmitForReview(owner, at))
	assert.Equal(t, domain.StatusPendingReview, s.Status())
	require.ErrorIs(t, s.UpdateLegalDetails(owner, legal(t, "legal_entity"), at), domain.ErrApplicationLocked)
	require.ErrorIs(t, s.AttachDocument(owner, doc(t, "identity_document"), at), domain.ErrApplicationLocked)

	require.ErrorIs(t, s.Reject(moderator, " ", at), domain.ErrReasonRequired)
	require.NoError(t, s.Reject(moderator, "blurry certificate", at))
	assert.Equal(t, domain.StatusDraft, s.Status())
	assert.Equal(t, "blurry certificate", s.RejectionReason())

	require.NoError(t, s.UpdateLegalDetails(owner, legal(t, "legal_entity"), at.Add(time.Hour)))
	require.NoError(t, s.SubmitForReview(owner, at))
	assert.Empty(t, s.RejectionReason())
	require.NoError(t, s.Approve(moderator, at))

	assert.Equal(t, domain.StatusActive, s.Status())
	assert.True(t, s.CanSell())
	assert.True(t, s.BankVerified())
	assert.True(t, s.PayoutsAllowed())
	assert.Equal(t, []string{
		"seller.application_submitted.v1",
		"seller.application_rejected.v1",
		"seller.legal_details_updated.v1",
		"seller.application_submitted.v1",
		"seller.application_approved.v1",
	}, names(s.PullEvents()))
}

func TestSeller_InvalidTransitions(t *testing.T) {
	s, owner := draft(t, "legal_entity")
	moderator := kernel.NewUserID()
	require.ErrorIs(t, s.Approve(moderator, at), &domain.TransitionError{})
	require.ErrorIs(t, s.Reject(moderator, "x", at), &domain.TransitionError{})
	require.ErrorIs(t, s.Suspend(moderator, domain.SuspendedManually, "x", at), &domain.TransitionError{})
	require.ErrorIs(t, s.Reinstate(moderator, at), &domain.TransitionError{})
	require.ErrorIs(t, s.Terminate(moderator, "x", at), &domain.TransitionError{})

	act, _ := active(t)
	require.ErrorIs(t, act.SubmitForReview(owner, at), domain.ErrNotSellerAdmin)
	require.ErrorIs(t, act.Reinstate(moderator, at), &domain.TransitionError{})

	require.NoError(t, act.Suspend(moderator, domain.SuspendedManually, "fraud", at))
	require.ErrorIs(t, act.Approve(moderator, at), &domain.TransitionError{}, "approval must not bypass reinstatement")
	assert.Equal(t, domain.StatusSuspended, act.Status())

	pending, pendingOwner := readyForReview(t)
	require.NoError(t, pending.SubmitForReview(pendingOwner, at))
	require.ErrorIs(t, pending.Reinstate(moderator, at), &domain.TransitionError{}, "reinstatement must not bypass moderation")
	assert.Equal(t, domain.StatusPendingReview, pending.Status())
}

func TestBankAccountChangeBlocksPayouts(t *testing.T) {
	s, owner := active(t)
	moderator := kernel.NewUserID()

	require.NoError(t, s.ChangeBankAccount(owner, bank(t, validIBAN), at))
	assert.Empty(t, s.PullEvents(), "same bank account is a no-op")

	require.NoError(t, s.ChangeBankAccount(owner, bank(t, otherIBAN), at))
	assert.False(t, s.BankVerified())
	assert.False(t, s.PayoutsAllowed())
	assert.True(t, s.CanSell())
	events := s.PullEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "****************0100", events[0].(domain.SellerBankAccountChanged).MaskedIBAN)

	require.NoError(t, s.VerifyBankAccount(moderator, at))
	assert.True(t, s.PayoutsAllowed())
	require.ErrorIs(t, s.VerifyBankAccount(moderator, at), domain.ErrBankAlreadyVerified)

	fresh, _ := draft(t, "legal_entity")
	require.ErrorIs(t, fresh.VerifyBankAccount(moderator, at), domain.ErrBankAccountMissing)
	require.ErrorIs(t, fresh.ChangeBankAccount(fresh.OwnerID(), domain.BankAccount{}, at), domain.ErrInvalidIBAN)
}

func TestSuspendReinstateTerminate(t *testing.T) {
	s, owner := active(t)
	admin := kernel.NewUserID()

	require.ErrorIs(t, s.Suspend(admin, domain.SuspendedManually, "", at), domain.ErrReasonRequired)
	require.NoError(t, s.Suspend(admin, domain.SuspendedManually, "counterfeit goods", at))
	assert.Equal(t, domain.StatusSuspended, s.Status())
	assert.Equal(t, domain.SuspendedManually, s.SuspensionReason())
	assert.False(t, s.CanSell())
	assert.True(t, s.PayoutsAllowed())

	require.NoError(t, s.Reinstate(admin, at))
	assert.Equal(t, domain.StatusActive, s.Status())
	assert.Empty(t, s.SuspensionReason())

	require.ErrorIs(t, s.Terminate(admin, "", at), domain.ErrReasonRequired)
	require.NoError(t, s.Terminate(admin, "contract ended", at))
	assert.True(t, s.Status().IsTerminal())
	assert.False(t, s.PayoutsAllowed())

	require.ErrorIs(t, s.ChangeBankAccount(owner, bank(t, otherIBAN), at), domain.ErrSellerTerminated)
	require.ErrorIs(t, s.AddMember(owner, kernel.NewUserID(), domain.MemberOperator, at), domain.ErrSellerTerminated)
	require.ErrorIs(t, s.VerifyBankAccount(admin, at), domain.ErrSellerTerminated)
	require.ErrorIs(t, s.SetCommissionOverride(domain.NewCategoryID(), kernel.MustBasisPoints(500), admin, at), domain.ErrSellerTerminated)
	assert.Equal(t, []string{"seller.suspended.v1", "seller.reinstated.v1", "seller.terminated.v1"}, names(s.PullEvents()))
}

func TestMembers(t *testing.T) {
	s, owner := draft(t, "legal_entity")
	operator := kernel.NewUserID()
	secondAdmin := kernel.NewUserID()
	stranger := kernel.NewUserID()
	s.PullEvents()

	require.NoError(t, s.AddMember(owner, operator, domain.MemberOperator, at))
	require.NoError(t, s.AddMember(owner, secondAdmin, domain.MemberAdmin, at))
	require.ErrorIs(t, s.AddMember(owner, operator, domain.MemberAdmin, at), domain.ErrMemberExists)
	require.ErrorIs(t, s.AddMember(owner, kernel.NewUserID(), "boss", at), domain.ErrInvalidMemberRole)
	require.ErrorIs(t, s.AddMember(owner, kernel.UserID{}, domain.MemberOperator, at), kernel.ErrInvalidID)

	require.ErrorIs(t, s.AddMember(operator, stranger, domain.MemberOperator, at), domain.ErrNotSellerAdmin)
	require.ErrorIs(t, s.ChangeBankAccount(operator, bank(t, validIBAN), at), domain.ErrNotSellerAdmin)
	require.ErrorIs(t, s.UpdateLegalDetails(stranger, legal(t, "legal_entity"), at), domain.ErrNotSellerAdmin)
	require.ErrorIs(t, s.AttachDocument(stranger, doc(t, "charter"), at), domain.ErrNotSellerAdmin)

	require.NoError(t, s.ChangeBankAccount(secondAdmin, bank(t, validIBAN), at))
	require.ErrorIs(t, s.RemoveMember(secondAdmin, owner, at), domain.ErrCannotRemoveOwner)
	require.ErrorIs(t, s.RemoveMember(secondAdmin, stranger, at), domain.ErrMemberNotFound)
	require.ErrorIs(t, s.RemoveMember(operator, secondAdmin, at), domain.ErrNotSellerAdmin)
	require.NoError(t, s.RemoveMember(secondAdmin, operator, at))

	assert.False(t, s.IsMember(operator))
	assert.True(t, s.IsMember(secondAdmin))
	assert.Len(t, s.Members(), 2)
	assert.Equal(t, []string{
		"seller.member_added.v1", "seller.member_added.v1", "seller.bank_account_changed.v1", "seller.member_removed.v1",
	}, names(s.PullEvents()))
}

func TestMembersAndDocumentsLimits(t *testing.T) {
	s, owner := draft(t, "legal_entity")
	for range 49 {
		require.NoError(t, s.AddMember(owner, kernel.NewUserID(), domain.MemberOperator, at))
	}
	require.ErrorIs(t, s.AddMember(owner, kernel.NewUserID(), domain.MemberOperator, at), domain.ErrTooManyMembers)

	for i := range 20 {
		d, err := domain.NewDocument("identity_document", "docs/"+kernel.NewID[struct{}]().String()+string(rune('a'+i)), at)
		require.NoError(t, err)
		require.NoError(t, s.AttachDocument(owner, d, at))
	}
	require.ErrorIs(t, s.AttachDocument(owner, doc(t, "charter"), at), domain.ErrTooManyDocuments)
	assert.Len(t, s.Documents(), 20)

	other, otherOwner := draft(t, "legal_entity")
	require.NoError(t, other.AttachDocument(otherOwner, doc(t, "charter"), at))
	require.ErrorIs(t, other.AttachDocument(otherOwner, doc(t, "charter"), at), domain.ErrDuplicateDocument)
}

func TestCommissionOverridesAndPolicy(t *testing.T) {
	s, _ := active(t)
	admin := kernel.NewUserID()
	electronics := domain.NewCategoryID()
	books := domain.NewCategoryID()
	toys := domain.NewCategoryID()
	policy := domain.DefaultCommissionPolicy()

	base, err := domain.SetCategoryCommission(books, kernel.MustBasisPoints(1500), admin, at)
	require.NoError(t, err)
	assert.Equal(t, []string{"seller.category_commission_changed.v1"}, names(base.PullEvents()))

	require.NoError(t, s.SetCommissionOverride(electronics, kernel.MustBasisPoints(700), admin, at))
	require.NoError(t, s.SetCommissionOverride(electronics, kernel.MustBasisPoints(700), admin, at))
	require.ErrorIs(t, s.SetCommissionOverride(domain.CategoryID{}, kernel.MustBasisPoints(700), admin, at), kernel.ErrInvalidID)

	assert.Equal(t, 700, policy.RateFor(s, electronics, nil).Value())
	assert.Equal(t, 1500, policy.RateFor(s, books, base).Value())
	assert.Equal(t, 1000, policy.RateFor(s, toys, nil).Value())

	s.ClearCommissionOverride(electronics, admin, at)
	s.ClearCommissionOverride(electronics, admin, at)
	_, ok := s.CommissionOverride(electronics)
	assert.False(t, ok)
	assert.Equal(t, []string{"seller.commission_override_set.v1", "seller.commission_override_cleared.v1"}, names(s.PullEvents()))

	base.Change(kernel.MustBasisPoints(1500), admin, at)
	base.Change(kernel.MustBasisPoints(1200), admin, at)
	assert.Equal(t, 1200, base.Rate().Value())
	assert.Equal(t, books, base.CategoryID())
	assert.Len(t, base.PullEvents(), 1)

	_, err = domain.SetCategoryCommission(domain.CategoryID{}, kernel.MustBasisPoints(1), admin, at)
	require.ErrorIs(t, err, kernel.ErrInvalidID)

	restored, err := domain.RehydrateCategoryCommission(books.String(), 1200, at, 3)
	require.NoError(t, err)
	assert.Equal(t, 3, restored.Version())
	assert.Equal(t, at, restored.UpdatedAt())
	_, err = domain.RehydrateCategoryCommission("bad", 1, at, 1)
	require.Error(t, err)
	_, err = domain.RehydrateCategoryCommission(books.String(), 10_001, at, 1)
	require.Error(t, err)
}

func TestApplyPerformance_AutoSuspendsActiveSeller(t *testing.T) {
	policy := domain.DefaultRatingPolicy()
	bad := domain.PerformanceMetrics{Orders: 100, CancelledBySeller: 30, LateShipments: 30, Reviews: 10, ReviewScoreSum: 15}

	s, _ := active(t)
	rating, err := s.ApplyPerformance(bad, policy, at)
	require.NoError(t, err)
	assert.Less(t, rating.Score(), policy.SuspendBelow)
	assert.Equal(t, domain.StatusSuspended, s.Status())
	assert.Equal(t, domain.SuspendedByLowRating, s.SuspensionReason())
	assert.Equal(t, rating, s.Rating())
	assert.Equal(t, []string{"seller.rating_changed.v1", "seller.suspended.v1"}, names(s.PullEvents()))

	_, err = s.ApplyPerformance(bad, policy, at)
	require.NoError(t, err)
	assert.Equal(t, []string{"seller.rating_changed.v1"}, names(s.PullEvents()), "already suspended seller is not suspended twice")

	provisional, _ := active(t)
	_, err = provisional.ApplyPerformance(domain.PerformanceMetrics{Orders: 5, CancelledBySeller: 5}, policy, at)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusActive, provisional.Status())

	_, err = provisional.ApplyPerformance(domain.PerformanceMetrics{Orders: -1}, policy, at)
	require.ErrorIs(t, err, domain.ErrInvalidMetrics)
}

func TestSeller_SnapshotRoundTrip(t *testing.T) {
	s, owner := readyForReview(t)
	require.NoError(t, s.AddMember(owner, kernel.NewUserID(), domain.MemberOperator, at))
	require.NoError(t, s.SubmitForReview(owner, at))
	require.NoError(t, s.Approve(kernel.NewUserID(), at))
	require.NoError(t, s.SetCommissionOverride(domain.NewCategoryID(), kernel.MustBasisPoints(800), kernel.NewUserID(), at))
	_, err := s.ApplyPerformance(domain.PerformanceMetrics{Orders: 40, LateShipments: 4, Reviews: 4, ReviewScoreSum: 18}, domain.DefaultRatingPolicy(), at)
	require.NoError(t, err)
	s.AdvanceVersion()

	restored, err := domain.RehydrateSeller(s.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, s.Snapshot(), restored.Snapshot())
	assert.Equal(t, 1, restored.Version())
	assert.Empty(t, restored.PullEvents())
	assert.Equal(t, s.ID(), restored.ID())

	plain, _ := draft(t, "sole_proprietor")
	restored, err = domain.RehydrateSeller(plain.Snapshot())
	require.NoError(t, err)
	assert.Nil(t, restored.Snapshot().Rating)
	assert.True(t, restored.BankAccount().IsZero())
}

func TestRehydrateSeller_RejectsCorruptedData(t *testing.T) {
	s, _ := active(t)
	base := s.Snapshot()
	mutations := []func(*domain.SellerSnapshot){
		func(x *domain.SellerSnapshot) { x.ID = "bad" },
		func(x *domain.SellerSnapshot) { x.OwnerID = "bad" },
		func(x *domain.SellerSnapshot) { x.TaxID = "000000000001" },
		func(x *domain.SellerSnapshot) { x.BankIBAN = "KZ00" },
		func(x *domain.SellerSnapshot) { x.Documents = []domain.DocumentSnapshot{{Kind: "x", ObjectKey: "k"}} },
		func(x *domain.SellerSnapshot) {
			x.Members = []domain.MemberSnapshot{{UserID: "bad", Role: "seller_admin"}}
		},
		func(x *domain.SellerSnapshot) {
			x.Members = []domain.MemberSnapshot{{UserID: kernel.NewUserID().String(), Role: "boss"}}
		},
		func(x *domain.SellerSnapshot) { x.CommissionOverrides = map[string]int{"bad": 1} },
		func(x *domain.SellerSnapshot) {
			x.CommissionOverrides = map[string]int{domain.NewCategoryID().String(): -1}
		},
	}
	for i, mutate := range mutations {
		snap := base
		snap.Documents = append([]domain.DocumentSnapshot(nil), base.Documents...)
		snap.Members = append([]domain.MemberSnapshot(nil), base.Members...)
		mutate(&snap)
		_, err := domain.RehydrateSeller(snap)
		require.Error(t, err, "mutation %d", i)
	}
}

func TestStatusTransitions(t *testing.T) {
	all := []domain.SellerStatus{domain.StatusDraft, domain.StatusPendingReview, domain.StatusActive, domain.StatusSuspended, domain.StatusTerminated}
	allowed := map[domain.SellerStatus][]domain.SellerStatus{
		domain.StatusDraft:         {domain.StatusPendingReview},
		domain.StatusPendingReview: {domain.StatusActive, domain.StatusDraft},
		domain.StatusActive:        {domain.StatusSuspended, domain.StatusTerminated},
		domain.StatusSuspended:     {domain.StatusActive, domain.StatusTerminated},
	}
	for _, from := range all {
		for _, to := range all {
			err := from.CanTransitionTo(to)
			if slices.Contains(allowed[from], to) {
				assert.NoError(t, err, "%s -> %s", from, to)
				continue
			}
			require.ErrorIs(t, err, &domain.TransitionError{}, "%s -> %s", from, to)
			assert.Equal(t, kernel.KindConflict, kernel.KindOf(err))
			assert.Equal(t, "SELLER_INVALID_TRANSITION", kernel.CodeOf(err))
		}
	}
	assert.True(t, domain.StatusTerminated.IsTerminal())
	assert.False(t, domain.StatusSuspended.IsTerminal())
}
