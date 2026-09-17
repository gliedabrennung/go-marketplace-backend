package domain

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Email struct {
	value string
}

func NewEmail(raw string) (Email, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || len(s) > 254 {
		return Email{}, ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return Email{}, ErrInvalidEmail
	}
	local, host, _ := strings.Cut(s, "@")
	if len(local) > 64 || !strings.Contains(host, ".") || strings.HasSuffix(host, ".") {
		return Email{}, ErrInvalidEmail
	}
	return Email{value: s}, nil
}

func (e Email) String() string { return e.value }

func (e Email) IsZero() bool { return e.value == "" }

type Password struct {
	value string
}

const (
	minPasswordLength = 8
	maxPasswordLength = 128
)

func NewPassword(raw string) (Password, error) {
	n := utf8.RuneCountInString(raw)
	if n < minPasswordLength || n > maxPasswordLength {
		return Password{}, ErrWeakPassword
	}
	var letter, digit bool
	for _, r := range raw {
		letter = letter || unicode.IsLetter(r)
		digit = digit || unicode.IsDigit(r)
	}
	if !letter || !digit {
		return Password{}, ErrWeakPassword
	}
	return Password{value: raw}, nil
}

func (p Password) Reveal() string { return p.value }

func (p Password) String() string { return "***" }

type PasswordHash struct {
	value string
}

func NewPasswordHash(encoded string) (PasswordHash, error) {
	if encoded == "" {
		return PasswordHash{}, ErrInvalidPasswordHash
	}
	return PasswordHash{value: encoded}, nil
}

func (h PasswordHash) String() string { return h.value }

func (h PasswordHash) IsZero() bool { return h.value == "" }
