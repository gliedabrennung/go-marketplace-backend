package domain

import (
	"strings"
	"unicode/utf8"
)

type LegalForm string

const (
	LegalEntity    LegalForm = "legal_entity"
	SoleProprietor LegalForm = "sole_proprietor"
)

func ParseLegalForm(s string) (LegalForm, error) {
	switch f := LegalForm(s); f {
	case LegalEntity, SoleProprietor:
		return f, nil
	default:
		return "", ErrInvalidLegalForm.WithDetail("%q", s)
	}
}

type TaxID struct {
	value string
}

var (
	taxWeightsPrimary   = [11]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	taxWeightsSecondary = [11]int{3, 4, 5, 6, 7, 8, 9, 10, 11, 1, 2}
)

func NewTaxID(raw string) (TaxID, error) {
	s := strings.TrimSpace(raw)
	if len(s) != 12 {
		return TaxID{}, ErrInvalidTaxID
	}
	var digits [12]int
	for i := range 12 {
		if s[i] < '0' || s[i] > '9' {
			return TaxID{}, ErrInvalidTaxID
		}
		digits[i] = int(s[i] - '0')
	}
	control := weightedMod11(digits, taxWeightsPrimary)
	if control == 10 {
		control = weightedMod11(digits, taxWeightsSecondary)
	}
	if control == 10 || control != digits[11] {
		return TaxID{}, ErrInvalidTaxID
	}
	return TaxID{value: s}, nil
}

func weightedMod11(digits [12]int, weights [11]int) int {
	sum := 0
	for i, w := range weights {
		sum += digits[i] * w
	}
	return sum % 11
}

func (t TaxID) String() string { return t.value }

func (t TaxID) IsZero() bool { return t.value == "" }

type LegalDetails struct {
	form    LegalForm
	name    string
	taxID   TaxID
	address string
}

func NewLegalDetails(form, name, taxID, address string) (LegalDetails, error) {
	f, err := ParseLegalForm(form)
	if err != nil {
		return LegalDetails{}, err
	}
	name = strings.TrimSpace(name)
	if !lengthBetween(name, 2, 300) {
		return LegalDetails{}, ErrInvalidLegalName
	}
	id, err := NewTaxID(taxID)
	if err != nil {
		return LegalDetails{}, err
	}
	address = strings.TrimSpace(address)
	if !lengthBetween(address, 5, 500) {
		return LegalDetails{}, ErrInvalidLegalAddress
	}
	return LegalDetails{form: f, name: name, taxID: id, address: address}, nil
}

func (l LegalDetails) Form() LegalForm { return l.form }

func (l LegalDetails) Name() string { return l.name }

func (l LegalDetails) TaxID() TaxID { return l.taxID }

func (l LegalDetails) Address() string { return l.address }

func (l LegalDetails) IsZero() bool { return l.taxID.IsZero() }

func lengthBetween(s string, lo, hi int) bool {
	n := utf8.RuneCountInString(s)
	return n >= lo && n <= hi
}
