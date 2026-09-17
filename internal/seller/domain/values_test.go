package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
)

const (
	validBIN  = "990140000384"
	validIIN  = "870103300129"
	validIBAN = "KZ86125KZT5004100100"
	otherIBAN = "KZ75125KZT2069100100"
)

func TestNewTaxID(t *testing.T) {
	for _, raw := range []string{validBIN, validIIN, " 050440001239 ", "101240004568"} {
		id, err := domain.NewTaxID(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, strings.TrimSpace(raw), id.String())
		assert.False(t, id.IsZero())
	}
	for _, raw := range []string{"", "99014000038", "9901400003845", "990140000385", "99014000038a"} {
		_, err := domain.NewTaxID(raw)
		require.ErrorIs(t, err, domain.ErrInvalidTaxID, raw)
	}
}

func TestNewLegalDetails(t *testing.T) {
	l, err := domain.NewLegalDetails("legal_entity", "  ТОО «Ромашка» ", validBIN, "г. Алматы, пр. Абая 1")
	require.NoError(t, err)
	assert.Equal(t, domain.LegalEntity, l.Form())
	assert.Equal(t, "ТОО «Ромашка»", l.Name())
	assert.Equal(t, validBIN, l.TaxID().String())
	assert.Equal(t, "г. Алматы, пр. Абая 1", l.Address())
	assert.False(t, l.IsZero())

	_, err = domain.NewLegalDetails("corp", "Name", validBIN, "Address 1")
	require.ErrorIs(t, err, domain.ErrInvalidLegalForm)
	_, err = domain.NewLegalDetails("sole_proprietor", "Я", validIIN, "Address 1")
	require.ErrorIs(t, err, domain.ErrInvalidLegalName)
	_, err = domain.NewLegalDetails("sole_proprietor", "ИП Иванов", "123", "Address 1")
	require.ErrorIs(t, err, domain.ErrInvalidTaxID)
	_, err = domain.NewLegalDetails("sole_proprietor", "ИП Иванов", validIIN, "abc")
	require.ErrorIs(t, err, domain.ErrInvalidLegalAddress)
}

func TestNewIBAN(t *testing.T) {
	iban, err := domain.NewIBAN(" kz86 125K ZT50 0410 0100 ")
	require.NoError(t, err)
	assert.Equal(t, validIBAN, iban.String())
	assert.Equal(t, "****************0100", iban.Masked())

	for _, raw := range []string{"", "KZ86125KZT5004100101", "KZ86125KZT50041001000", "1Z86125KZT5004100100", "KZ8X125KZT5004100100", "DE89370400440532013001", "KZ86125KZT50041001-0"} {
		_, err := domain.NewIBAN(raw)
		require.ErrorIs(t, err, domain.ErrInvalidIBAN, raw)
	}

	de, err := domain.NewIBAN("DE89370400440532013000")
	require.NoError(t, err)
	assert.Equal(t, "DE89370400440532013000", de.String())
}

func TestNewBIC(t *testing.T) {
	for _, raw := range []string{"HSBKKZKX", "caspkzkaxxx"} {
		b, err := domain.NewBIC(raw)
		require.NoError(t, err)
		assert.Equal(t, strings.ToUpper(raw), b.String())
	}
	for _, raw := range []string{"", "HSBK1ZKX", "HSBKKZK", "HSBKKZKX1", "HSBKKZK!"} {
		_, err := domain.NewBIC(raw)
		require.ErrorIs(t, err, domain.ErrInvalidBIC, raw)
	}
}

func TestNewBankAccount(t *testing.T) {
	a, err := domain.NewBankAccount(validIBAN, "HSBKKZKX", "Halyk Bank", "ТОО Ромашка")
	require.NoError(t, err)
	b, err := domain.NewBankAccount(validIBAN, "hsbkkzkx", " Halyk Bank ", "ТОО Ромашка")
	require.NoError(t, err)
	assert.True(t, a.Equals(b))
	assert.Equal(t, "HSBKKZKX", a.BIC().String())
	assert.Equal(t, "Halyk Bank", a.BankName())
	assert.Equal(t, "ТОО Ромашка", a.Beneficiary())
	assert.Equal(t, validIBAN, a.IBAN().String())
	assert.False(t, a.IsZero())

	_, err = domain.NewBankAccount("bad", "HSBKKZKX", "Bank", "Name")
	require.ErrorIs(t, err, domain.ErrInvalidIBAN)
	_, err = domain.NewBankAccount(validIBAN, "bad", "Bank", "Name")
	require.ErrorIs(t, err, domain.ErrInvalidBIC)
	_, err = domain.NewBankAccount(validIBAN, "HSBKKZKX", "B", "Name")
	require.ErrorIs(t, err, domain.ErrInvalidBankName)
	_, err = domain.NewBankAccount(validIBAN, "HSBKKZKX", "Bank", "N")
	require.ErrorIs(t, err, domain.ErrInvalidBeneficiary)
}

func TestNewDocument(t *testing.T) {
	at := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	d, err := domain.NewDocument("charter", " sellers/abc/charter.pdf ", at)
	require.NoError(t, err)
	assert.Equal(t, domain.DocumentCharter, d.Kind())
	assert.Equal(t, "sellers/abc/charter.pdf", d.ObjectKey())
	assert.Equal(t, at, d.UploadedAt())

	_, err = domain.NewDocument("passport_scan", "key", at)
	require.ErrorIs(t, err, domain.ErrInvalidDocumentKind)
	for _, key := range []string{"", "../secret", "/etc/passwd", strings.Repeat("k", 513)} {
		_, err := domain.NewDocument("charter", key, at)
		require.ErrorIs(t, err, domain.ErrInvalidDocumentKey, key)
	}
}

func TestParsers(t *testing.T) {
	role, err := domain.ParseMemberRole("seller_operator")
	require.NoError(t, err)
	assert.Equal(t, domain.MemberOperator, role)
	_, err = domain.ParseMemberRole("owner")
	require.ErrorIs(t, err, domain.ErrInvalidMemberRole)

	form, err := domain.ParseLegalForm("sole_proprietor")
	require.NoError(t, err)
	assert.Equal(t, domain.SoleProprietor, form)

	category := domain.NewCategoryID()
	parsed, err := domain.ParseCategoryID(category.String())
	require.NoError(t, err)
	assert.Equal(t, category, parsed)
}
