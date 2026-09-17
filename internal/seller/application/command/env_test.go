package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
)

var ctx = context.Background()

const (
	validBIN   = "990140000384"
	otherBIN   = "050440001239"
	validIBAN  = "KZ86125KZT5004100100"
	secondIBAN = "KZ75125KZT2069100100"
)

type env struct {
	store *memory.Store
	clock *clock.Manual

	open             *command.OpenApplicationHandler
	updateLegal      *command.UpdateLegalDetailsHandler
	changeBank       *command.ChangeBankAccountHandler
	attachDocument   *command.AttachDocumentHandler
	submit           *command.SubmitApplicationHandler
	addMember        *command.AddMemberHandler
	removeMember     *command.RemoveMemberHandler
	approve          *command.ApproveApplicationHandler
	reject           *command.RejectApplicationHandler
	verifyBank       *command.VerifyBankAccountHandler
	suspend          *command.SuspendSellerHandler
	reinstate        *command.ReinstateSellerHandler
	terminate        *command.TerminateSellerHandler
	setOverride      *command.SetCommissionOverrideHandler
	clearOverride    *command.ClearCommissionOverrideHandler
	setCategory      *command.SetCategoryCommissionHandler
	applyPerformance *command.ApplyPerformanceHandler
	getSeller        *query.GetSellerHandler
	listMine         *query.ListMySellersHandler
	listApplications *query.ListApplicationsHandler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	store := memory.NewStore()
	e := &env{store: store, clock: clock.NewManual(time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC))}
	unit := command.NewUnit(memory.NewUnitOfWork(store), e.clock)

	e.open = command.NewOpenApplicationHandler(unit)
	e.updateLegal = command.NewUpdateLegalDetailsHandler(unit)
	e.changeBank = command.NewChangeBankAccountHandler(unit)
	e.attachDocument = command.NewAttachDocumentHandler(unit)
	e.submit = command.NewSubmitApplicationHandler(unit)
	e.addMember = command.NewAddMemberHandler(unit)
	e.removeMember = command.NewRemoveMemberHandler(unit)
	e.approve = command.NewApproveApplicationHandler(unit)
	e.reject = command.NewRejectApplicationHandler(unit)
	e.verifyBank = command.NewVerifyBankAccountHandler(unit)
	e.suspend = command.NewSuspendSellerHandler(unit)
	e.reinstate = command.NewReinstateSellerHandler(unit)
	e.terminate = command.NewTerminateSellerHandler(unit)
	e.setOverride = command.NewSetCommissionOverrideHandler(unit)
	e.clearOverride = command.NewClearCommissionOverrideHandler(unit)
	e.setCategory = command.NewSetCategoryCommissionHandler(unit)
	e.applyPerformance = command.NewApplyPerformanceHandler(unit, domain.DefaultRatingPolicy())
	e.getSeller = query.NewGetSellerHandler(store)
	e.listMine = query.NewListMySellersHandler(store)
	e.listApplications = query.NewListApplicationsHandler(store)
	return e
}

func user(roles ...string) auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: append([]string{"buyer"}, roles...)}
}

func (e *env) openDraft(t *testing.T, owner auth.Principal, taxID string) string {
	t.Helper()
	res, err := e.open.Handle(ctx, command.OpenApplication{
		Actor: owner, LegalForm: "legal_entity", LegalName: "ТОО Ромашка", TaxID: taxID, LegalAddress: "г. Алматы, пр. Абая 1",
	})
	require.NoError(t, err)
	return res.SellerID
}

func (e *env) submitted(t *testing.T, owner auth.Principal, taxID string) string {
	t.Helper()
	id := e.openDraft(t, owner, taxID)
	_, err := e.changeBank.Handle(ctx, command.ChangeBankAccount{
		Actor: owner, SellerID: id, IBAN: validIBAN, BIC: "HSBKKZKX", BankName: "Halyk Bank", Beneficiary: "ТОО Ромашка",
	})
	require.NoError(t, err)
	for _, kind := range []string{"registration_certificate", "bank_confirmation", "charter"} {
		_, err := e.attachDocument.Handle(ctx, command.AttachDocument{Actor: owner, SellerID: id, Kind: kind, ObjectKey: "sellers/" + id + "/" + kind + ".pdf"})
		require.NoError(t, err)
	}
	_, err = e.submit.Handle(ctx, command.SubmitApplication{Actor: owner, SellerID: id})
	require.NoError(t, err)
	return id
}

func (e *env) activeSeller(t *testing.T, owner auth.Principal, taxID string) string {
	t.Helper()
	id := e.submitted(t, owner, taxID)
	_, err := e.approve.Handle(ctx, command.ApproveApplication{Actor: user("content_moderator"), SellerID: id})
	require.NoError(t, err)
	return id
}

func (e *env) status(t *testing.T, sellerID string) string {
	t.Helper()
	view, err := e.store.Seller(ctx, sellerID)
	require.NoError(t, err)
	return view.Status
}
