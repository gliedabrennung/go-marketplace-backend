package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var phonePattern = regexp.MustCompile(`^\+?[0-9]{10,15}$`)

type Address struct {
	Recipient  string
	Phone      string
	Country    string
	City       string
	Line       string
	PostalCode string
}

func NewAddress(a Address) (Address, error) {
	out := Address{
		Recipient: strings.TrimSpace(a.Recipient), Phone: strings.Map(dropFormatting, strings.TrimSpace(a.Phone)),
		Country: strings.ToUpper(strings.TrimSpace(a.Country)), City: strings.TrimSpace(a.City),
		Line: strings.TrimSpace(a.Line), PostalCode: strings.TrimSpace(a.PostalCode),
	}
	if out.Country == "" {
		out.Country = "KZ"
	}
	switch {
	case !within(out.Recipient, 1, 200), !within(out.City, 1, 100), !within(out.Line, 1, 300):
		return Address{}, ErrInvalidAddress.WithDetail("recipient, city and address line are required")
	case !phonePattern.MatchString(out.Phone):
		return Address{}, ErrInvalidAddress.WithDetail("phone must contain 10-15 digits")
	case utf8.RuneCountInString(out.Country) != 2:
		return Address{}, ErrInvalidAddress.WithDetail("country must be an ISO 3166-1 alpha-2 code")
	case !within(out.PostalCode, 0, 20):
		return Address{}, ErrInvalidAddress.WithDetail("postal code is too long")
	}
	return out, nil
}

func dropFormatting(r rune) rune {
	switch r {
	case ' ', '-', '(', ')':
		return -1
	}
	return r
}

func within(value string, minRunes, maxRunes int) bool {
	count := utf8.RuneCountInString(value)
	return count >= minRunes && count <= maxRunes
}
