package command_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func listMine(p auth.Principal) query.ListMySellers {
	return query.ListMySellers{Actor: p}
}

func TestModeration_RequiresPermission(t *testing.T) {
	e := newEnv(t)
	id := e.submitted(t, user(), validBIN)
	buyer := user()

	_, err := e.approve.Handle(ctx, command.ApproveApplication{Actor: buyer, SellerID: id})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.approve.Handle(ctx, command.ApproveApplication{SellerID: id})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.suspend.Handle(ctx, command.SuspendSeller{Actor: user("content_moderator"), SellerID: id, Reason: "x"})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.approve.Handle(ctx, command.ApproveApplication{Actor: user("content_moderator"), SellerID: "bad"})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)
	_, err = e.approve.Handle(ctx, command.ApproveApplication{Actor: auth.Principal{UserID: "bad", Roles: []string{"platform_admin"}}, SellerID: id})
	require.ErrorIs(t, err, auth.ErrInvalidToken)
	_, err = e.approve.Handle(ctx, command.ApproveApplication{Actor: user("platform_admin"), SellerID: kernel.NewSellerID().String()})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)
	assert.Equal(t, "pending_review", e.status(t, id))
}

func TestModeration_RejectApproveVerify(t *testing.T) {
	e := newEnv(t)
	owner := user()
	moderator := user("content_moderator")
	id := e.submitted(t, owner, validBIN)

	_, err := e.reject.Handle(ctx, command.RejectApplication{Actor: moderator, SellerID: id, Reason: "certificate expired"})
	require.NoError(t, err)
	view, err := e.getSeller.Handle(ctx, query.GetSeller{Actor: owner, SellerID: id})
	require.NoError(t, err)
	assert.Equal(t, "draft", view.Status)
	assert.Equal(t, "certificate expired", view.RejectionReason)

	_, err = e.submit.Handle(ctx, command.SubmitApplication{Actor: owner, SellerID: id})
	require.NoError(t, err)
	_, err = e.approve.Handle(ctx, command.ApproveApplication{Actor: moderator, SellerID: id})
	require.NoError(t, err)
	assert.Equal(t, "active", e.status(t, id))

	_, err = e.changeBank.Handle(ctx, command.ChangeBankAccount{
		Actor: owner, SellerID: id, IBAN: secondIBAN, BIC: "HSBKKZKX", BankName: "Halyk Bank", Beneficiary: "ТОО Ромашка",
	})
	require.NoError(t, err)
	view, err = e.store.Seller(ctx, id)
	require.NoError(t, err)
	assert.False(t, view.BankVerified)
	assert.Equal(t, "****************0100", view.BankIBANMasked)

	_, err = e.verifyBank.Handle(ctx, command.VerifyBankAccount{Actor: moderator, SellerID: id})
	require.NoError(t, err)

	actions := []string{}
	for _, entry := range e.store.AuditEntries() {
		actions = append(actions, entry.Action)
		assert.Equal(t, moderator.UserID, entry.ActorID)
		assert.Equal(t, id, entry.ObjectID)
	}
	assert.Equal(t, []string{"seller.application.reject", "seller.application.approve", "seller.bank_account.verify"}, actions)
}

func TestSuspendReinstateTerminate(t *testing.T) {
	e := newEnv(t)
	id := e.activeSeller(t, user(), validBIN)
	admin := user("platform_admin")

	_, err := e.suspend.Handle(ctx, command.SuspendSeller{Actor: admin, SellerID: id, Reason: " "})
	require.ErrorIs(t, err, domain.ErrReasonRequired)
	_, err = e.suspend.Handle(ctx, command.SuspendSeller{Actor: admin, SellerID: id, Reason: "counterfeit"})
	require.NoError(t, err)
	assert.Equal(t, "suspended", e.status(t, id))

	_, err = e.reinstate.Handle(ctx, command.ReinstateSeller{Actor: admin, SellerID: id})
	require.NoError(t, err)
	_, err = e.terminate.Handle(ctx, command.TerminateSeller{Actor: admin, SellerID: id, Reason: "contract ended"})
	require.NoError(t, err)
	assert.Equal(t, "terminated", e.status(t, id))

	_, err = e.reinstate.Handle(ctx, command.ReinstateSeller{Actor: admin, SellerID: id})
	require.ErrorIs(t, err, &domain.TransitionError{})

	e.openDraft(t, user(), validBIN)
}

func TestCommissions(t *testing.T) {
	e := newEnv(t)
	id := e.activeSeller(t, user(), validBIN)
	admin := user("platform_admin")
	category := domain.NewCategoryID().String()

	_, err := e.setOverride.Handle(ctx, command.SetCommissionOverride{Actor: user("content_moderator"), SellerID: id, CategoryID: category, BasisPoints: 500})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.setOverride.Handle(ctx, command.SetCommissionOverride{Actor: admin, SellerID: id, CategoryID: "bad", BasisPoints: 500})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.setOverride.Handle(ctx, command.SetCommissionOverride{Actor: admin, SellerID: id, CategoryID: category, BasisPoints: 20_000})
	require.ErrorIs(t, err, kernel.ErrInvalidBasis)

	_, err = e.setOverride.Handle(ctx, command.SetCommissionOverride{Actor: admin, SellerID: id, CategoryID: category, BasisPoints: 500})
	require.NoError(t, err)
	view, err := e.store.Seller(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{category: 500}, view.CommissionOverrides)

	_, err = e.clearOverride.Handle(ctx, command.ClearCommissionOverride{Actor: admin, SellerID: id, CategoryID: "bad"})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.clearOverride.Handle(ctx, command.ClearCommissionOverride{Actor: admin, SellerID: id, CategoryID: category})
	require.NoError(t, err)
	view, err = e.store.Seller(ctx, id)
	require.NoError(t, err)
	assert.Empty(t, view.CommissionOverrides)

	_, err = e.setCategory.Handle(ctx, command.SetCategoryCommission{Actor: admin, CategoryID: category, BasisPoints: 1200})
	require.NoError(t, err)
	_, err = e.setCategory.Handle(ctx, command.SetCategoryCommission{Actor: admin, CategoryID: category, BasisPoints: 1300})
	require.NoError(t, err)
	parsed, err := domain.ParseCategoryID(category)
	require.NoError(t, err)
	stored, err := e.store.CategoryCommissions().FindByCategory(ctx, parsed)
	require.NoError(t, err)
	assert.Equal(t, 1300, stored.Rate().Value())
	assert.Equal(t, 2, stored.Version())

	_, err = e.setCategory.Handle(ctx, command.SetCategoryCommission{Actor: user(), CategoryID: category, BasisPoints: 1})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.setCategory.Handle(ctx, command.SetCategoryCommission{Actor: admin, CategoryID: "bad", BasisPoints: 1})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.setCategory.Handle(ctx, command.SetCategoryCommission{Actor: admin, CategoryID: category, BasisPoints: -1})
	require.ErrorIs(t, err, kernel.ErrInvalidBasis)
	_, err = e.setCategory.Handle(ctx, command.SetCategoryCommission{Actor: auth.Principal{UserID: "bad", Roles: []string{"platform_admin"}}, CategoryID: category, BasisPoints: 1})
	require.ErrorIs(t, err, auth.ErrInvalidToken)

	var names []string
	for _, ev := range e.store.Events() {
		names = append(names, ev.EventName())
	}
	assert.Contains(t, names, "seller.commission_override_set.v1")
	assert.Contains(t, names, "seller.commission_override_cleared.v1")
	assert.Contains(t, names, "seller.category_commission_changed.v1")
}

func TestApplyPerformance(t *testing.T) {
	e := newEnv(t)
	id := e.activeSeller(t, user(), validBIN)
	e.clock.Advance(time.Hour)

	res, err := e.applyPerformance.Handle(ctx, command.ApplyPerformance{SellerID: id, Orders: 100, Reviews: 10, ReviewScoreSum: 48})
	require.NoError(t, err)
	assert.False(t, res.Suspended)
	assert.False(t, res.Provisional)

	res, err = e.applyPerformance.Handle(ctx, command.ApplyPerformance{SellerID: id, Orders: 100, CancelledBySeller: 40, LateShipments: 40, Reviews: 10, ReviewScoreSum: 10})
	require.NoError(t, err)
	assert.True(t, res.Suspended)
	assert.Equal(t, "suspended", e.status(t, id))

	_, err = e.applyPerformance.Handle(ctx, command.ApplyPerformance{SellerID: id, Orders: -1})
	require.ErrorIs(t, err, domain.ErrInvalidMetrics)
	_, err = e.applyPerformance.Handle(ctx, command.ApplyPerformance{SellerID: "bad"})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)
	_, err = e.applyPerformance.Handle(ctx, command.ApplyPerformance{SellerID: kernel.NewSellerID().String()})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)
}

func TestQueries(t *testing.T) {
	e := newEnv(t)
	owner := user()
	first := e.submitted(t, owner, validBIN)
	e.clock.Advance(time.Minute)
	second := e.submitted(t, user(), otherBIN)
	moderator := user("content_moderator")

	view, err := e.getSeller.Handle(ctx, query.GetSeller{Actor: owner, SellerID: first})
	require.NoError(t, err)
	assert.Len(t, view.Documents, 3)
	assert.Len(t, view.Members, 1)
	_, err = e.getSeller.Handle(ctx, query.GetSeller{Actor: user(), SellerID: first})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)
	_, err = e.getSeller.Handle(ctx, query.GetSeller{Actor: moderator, SellerID: first})
	require.NoError(t, err)
	_, err = e.getSeller.Handle(ctx, query.GetSeller{SellerID: first})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
	_, err = e.getSeller.Handle(ctx, query.GetSeller{Actor: moderator, SellerID: kernel.NewSellerID().String()})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)

	page, err := e.listApplications.Handle(ctx, query.ListApplications{Actor: moderator, Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, first, page.Items[0].ID)
	assert.True(t, page.HasMore)

	rest, err := e.listApplications.Handle(ctx, query.ListApplications{Actor: moderator, Limit: 1, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, rest.Items, 1)
	assert.Equal(t, second, rest.Items[0].ID)
	assert.False(t, rest.HasMore)

	drafts, err := e.listApplications.Handle(ctx, query.ListApplications{Actor: moderator, Status: "draft"})
	require.NoError(t, err)
	assert.Empty(t, drafts.Items)

	_, err = e.listApplications.Handle(ctx, query.ListApplications{Actor: moderator, Status: "archived"})
	require.ErrorIs(t, err, query.ErrUnknownStatus)
	_, err = e.listApplications.Handle(ctx, query.ListApplications{Actor: moderator, Cursor: "!!"})
	require.Error(t, err)
	_, err = e.listApplications.Handle(ctx, query.ListApplications{Actor: owner})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.listMine.Handle(ctx, query.ListMySellers{})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}
