package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func TestOpenApplication(t *testing.T) {
	e := newEnv(t)
	owner := user()
	id := e.openDraft(t, owner, validBIN)
	assert.Equal(t, "draft", e.status(t, id))

	_, err := e.open.Handle(ctx, command.OpenApplication{Actor: owner, LegalForm: "legal_entity", LegalName: "Второе ТОО", TaxID: otherBIN, LegalAddress: "г. Астана, ул. 1"})
	require.ErrorIs(t, err, domain.ErrSellerAlreadyExists)

	_, err = e.open.Handle(ctx, command.OpenApplication{Actor: user(), LegalForm: "legal_entity", LegalName: "Клон", TaxID: validBIN, LegalAddress: "г. Астана, ул. 1"})
	require.ErrorIs(t, err, domain.ErrTaxIDTaken)

	_, err = e.open.Handle(ctx, command.OpenApplication{Actor: user(), LegalForm: "corp", LegalName: "X", TaxID: validBIN, LegalAddress: "addr"})
	require.ErrorIs(t, err, domain.ErrInvalidLegalForm)

	_, err = e.open.Handle(ctx, command.OpenApplication{LegalForm: "legal_entity"})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)

	_, err = e.open.Handle(ctx, command.OpenApplication{Actor: auth.Principal{UserID: "bad"}})
	require.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestApplicantFlow(t *testing.T) {
	e := newEnv(t)
	owner := user()
	id := e.openDraft(t, owner, validBIN)

	_, err := e.updateLegal.Handle(ctx, command.UpdateLegalDetails{
		Actor: owner, SellerID: id, LegalForm: "legal_entity", LegalName: "ТОО Ромашка Плюс", TaxID: validBIN, LegalAddress: "г. Алматы, пр. Абая 2",
	})
	require.NoError(t, err)

	_, err = e.submit.Handle(ctx, command.SubmitApplication{Actor: owner, SellerID: id})
	require.ErrorIs(t, err, domain.ErrApplicationIncomplete)

	id = e.submitted(t, user(), otherBIN)
	assert.Equal(t, "pending_review", e.status(t, id))
}

func TestApplicantCommands_Validation(t *testing.T) {
	e := newEnv(t)
	owner := user()
	id := e.openDraft(t, owner, validBIN)

	_, err := e.updateLegal.Handle(ctx, command.UpdateLegalDetails{Actor: owner, SellerID: id, LegalForm: "legal_entity", LegalName: "X", TaxID: validBIN, LegalAddress: "addr 1"})
	require.ErrorIs(t, err, domain.ErrInvalidLegalName)
	_, err = e.changeBank.Handle(ctx, command.ChangeBankAccount{Actor: owner, SellerID: id, IBAN: "bad"})
	require.ErrorIs(t, err, domain.ErrInvalidIBAN)
	_, err = e.attachDocument.Handle(ctx, command.AttachDocument{Actor: owner, SellerID: id, Kind: "selfie", ObjectKey: "k"})
	require.ErrorIs(t, err, domain.ErrInvalidDocumentKind)
	_, err = e.submit.Handle(ctx, command.SubmitApplication{Actor: owner, SellerID: "not-a-uuid"})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)
	_, err = e.submit.Handle(ctx, command.SubmitApplication{Actor: owner, SellerID: kernel.NewSellerID().String()})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)
	_, err = e.submit.Handle(ctx, command.SubmitApplication{SellerID: id})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)
}

func TestApplicantCommands_HideForeignSellers(t *testing.T) {
	e := newEnv(t)
	owner := user()
	id := e.openDraft(t, owner, validBIN)
	stranger := user()

	_, err := e.changeBank.Handle(ctx, command.ChangeBankAccount{
		Actor: stranger, SellerID: id, IBAN: validIBAN, BIC: "HSBKKZKX", BankName: "Halyk Bank", Beneficiary: "Мошенник",
	})
	require.ErrorIs(t, err, domain.ErrSellerNotFound)

	platformAdmin := user("platform_admin")
	_, err = e.submit.Handle(ctx, command.SubmitApplication{Actor: platformAdmin, SellerID: id})
	require.ErrorIs(t, err, domain.ErrSellerNotFound, "platform staff cannot act on behalf of a seller")
}

func TestMembersManagement(t *testing.T) {
	e := newEnv(t)
	owner := user()
	operator := user()
	id := e.openDraft(t, owner, validBIN)

	_, err := e.addMember.Handle(ctx, command.AddMember{Actor: owner, SellerID: id, UserID: operator.UserID, Role: "seller_operator"})
	require.NoError(t, err)

	_, err = e.changeBank.Handle(ctx, command.ChangeBankAccount{
		Actor: operator, SellerID: id, IBAN: validIBAN, BIC: "HSBKKZKX", BankName: "Halyk Bank", Beneficiary: "ТОО Ромашка",
	})
	require.ErrorIs(t, err, domain.ErrNotSellerAdmin)

	_, err = e.addMember.Handle(ctx, command.AddMember{Actor: owner, SellerID: id, UserID: "bad", Role: "seller_operator"})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
	_, err = e.addMember.Handle(ctx, command.AddMember{Actor: owner, SellerID: id, UserID: operator.UserID, Role: "king"})
	require.ErrorIs(t, err, domain.ErrInvalidMemberRole)

	mine, err := e.listMine.Handle(ctx, listMine(operator))
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, "seller_operator", mine[0].Role)

	_, err = e.removeMember.Handle(ctx, command.RemoveMember{Actor: owner, SellerID: id, UserID: "bad"})
	require.ErrorIs(t, err, domain.ErrMemberNotFound)
	_, err = e.removeMember.Handle(ctx, command.RemoveMember{Actor: owner, SellerID: id, UserID: owner.UserID})
	require.ErrorIs(t, err, domain.ErrCannotRemoveOwner)
	_, err = e.removeMember.Handle(ctx, command.RemoveMember{Actor: owner, SellerID: id, UserID: operator.UserID})
	require.NoError(t, err)

	mine, err = e.listMine.Handle(ctx, listMine(operator))
	require.NoError(t, err)
	assert.Empty(t, mine)
}
