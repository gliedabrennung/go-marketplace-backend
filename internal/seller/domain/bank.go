package domain

import (
	"strings"
)

var ibanLengths = map[string]int{
	"KZ": 20,
	"DE": 22,
	"GB": 22,
	"FR": 27,
}

type IBAN struct {
	value string
}

func NewIBAN(raw string) (IBAN, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""))
	if len(s) < 15 || len(s) > 34 {
		return IBAN{}, ErrInvalidIBAN
	}
	if !isUpperLetter(s[0]) || !isUpperLetter(s[1]) || !isDigit(s[2]) || !isDigit(s[3]) {
		return IBAN{}, ErrInvalidIBAN
	}
	if want, known := ibanLengths[s[:2]]; known && len(s) != want {
		return IBAN{}, ErrInvalidIBAN
	}
	remainder := 0
	for _, c := range []byte(s[4:] + s[:4]) {
		switch {
		case isDigit(c):
			remainder = (remainder*10 + int(c-'0')) % 97
		case isUpperLetter(c):
			remainder = (remainder*100 + int(c-'A') + 10) % 97
		default:
			return IBAN{}, ErrInvalidIBAN
		}
	}
	if remainder != 1 {
		return IBAN{}, ErrInvalidIBAN
	}
	return IBAN{value: s}, nil
}

func (i IBAN) String() string { return i.value }

func (i IBAN) Masked() string {
	if len(i.value) <= 4 {
		return i.value
	}
	return strings.Repeat("*", len(i.value)-4) + i.value[len(i.value)-4:]
}

func (i IBAN) IsZero() bool { return i.value == "" }

type BIC struct {
	value string
}

func NewBIC(raw string) (BIC, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if len(s) != 8 && len(s) != 11 {
		return BIC{}, ErrInvalidBIC
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case i < 6 && !isUpperLetter(c):
			return BIC{}, ErrInvalidBIC
		case i >= 6 && !isUpperLetter(c) && !isDigit(c):
			return BIC{}, ErrInvalidBIC
		}
	}
	return BIC{value: s}, nil
}

func (b BIC) String() string { return b.value }

type BankAccount struct {
	iban        IBAN
	bic         BIC
	bankName    string
	beneficiary string
}

func NewBankAccount(iban, bic, bankName, beneficiary string) (BankAccount, error) {
	i, err := NewIBAN(iban)
	if err != nil {
		return BankAccount{}, err
	}
	b, err := NewBIC(bic)
	if err != nil {
		return BankAccount{}, err
	}
	bankName = strings.TrimSpace(bankName)
	if !lengthBetween(bankName, 2, 200) {
		return BankAccount{}, ErrInvalidBankName
	}
	beneficiary = strings.TrimSpace(beneficiary)
	if !lengthBetween(beneficiary, 2, 300) {
		return BankAccount{}, ErrInvalidBeneficiary
	}
	return BankAccount{iban: i, bic: b, bankName: bankName, beneficiary: beneficiary}, nil
}

func (a BankAccount) IBAN() IBAN { return a.iban }

func (a BankAccount) BIC() BIC { return a.bic }

func (a BankAccount) BankName() string { return a.bankName }

func (a BankAccount) Beneficiary() string { return a.beneficiary }

func (a BankAccount) IsZero() bool { return a.iban.IsZero() }

func (a BankAccount) Equals(other BankAccount) bool { return a == other }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isUpperLetter(c byte) bool { return c >= 'A' && c <= 'Z' }
