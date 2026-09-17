package kernel

import (
	"fmt"
	"math"
)

type Currency string

const (
	KZT Currency = "KZT"
	RUB Currency = "RUB"
	USD Currency = "USD"
)

var (
	ErrCurrencyRequired = Validation("CURRENCY_REQUIRED", "currency is required")
	ErrInvalidCurrency  = Validation("INVALID_CURRENCY", "currency must be an ISO 4217 code")
	ErrNegativeAmount   = Validation("NEGATIVE_AMOUNT", "amount must not be negative")
	ErrCurrencyMismatch = BusinessRule("CURRENCY_MISMATCH", "currencies do not match")
	ErrMoneyOverflow    = BusinessRule("MONEY_OVERFLOW", "money amount overflow")
	ErrInvalidBasis     = Validation("INVALID_BASIS_POINTS", "basis points must be between 0 and 10000")
)

func NewCurrency(code string) (Currency, error) {
	if code == "" {
		return "", ErrCurrencyRequired
	}
	if len(code) != 3 {
		return "", ErrInvalidCurrency.WithDetail("%q", code)
	}
	for i := range len(code) {
		if code[i] < 'A' || code[i] > 'Z' {
			return "", ErrInvalidCurrency.WithDetail("%q", code)
		}
	}
	return Currency(code), nil
}

func (c Currency) String() string { return string(c) }

type Money struct {
	amount   int64
	currency Currency
}

func NewMoney(amount int64, c Currency) (Money, error) {
	cur, err := NewCurrency(string(c))
	if err != nil {
		return Money{}, err
	}
	if amount < 0 {
		return Money{}, ErrNegativeAmount.WithDetail("%d", amount)
	}
	return Money{amount: amount, currency: cur}, nil
}

func MustMoney(amount int64, c Currency) Money {
	m, err := NewMoney(amount, c)
	if err != nil {
		panic(err)
	}
	return m
}

func ZeroMoney(c Currency) (Money, error) {
	return NewMoney(0, c)
}

func ZeroLike(m Money) Money {
	return Money{amount: 0, currency: m.currency}
}

func (m Money) Amount() int64 { return m.amount }

func (m Money) Currency() Currency { return m.currency }

func (m Money) IsZero() bool { return m.amount == 0 }

func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	if other.amount > math.MaxInt64-m.amount {
		return Money{}, ErrMoneyOverflow
	}
	return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

func (m Money) Sub(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	if m.amount < other.amount {
		return Money{}, ErrNegativeAmount
	}
	return Money{amount: m.amount - other.amount, currency: m.currency}, nil
}

func (m Money) MulQuantity(q Quantity) (Money, error) {
	n := int64(q.Value())
	if n != 0 && m.amount > math.MaxInt64/n {
		return Money{}, ErrMoneyOverflow
	}
	return Money{amount: m.amount * n, currency: m.currency}, nil
}

func (m Money) ApplyPercent(bp BasisPoints) Money {
	whole := m.amount / 10_000
	rest := m.amount % 10_000
	v := int64(bp.Value())
	return Money{amount: whole*v + rest*v/10_000, currency: m.currency}
}

func (m Money) Compare(other Money) (int, error) {
	if m.currency != other.currency {
		return 0, ErrCurrencyMismatch
	}
	switch {
	case m.amount < other.amount:
		return -1, nil
	case m.amount > other.amount:
		return 1, nil
	default:
		return 0, nil
	}
}

func (m Money) Equals(other Money) bool {
	return m.amount == other.amount && m.currency == other.currency
}

func (m Money) String() string {
	return fmt.Sprintf("%d.%02d %s", m.amount/100, m.amount%100, m.currency)
}

func SumMoney(c Currency, values ...Money) (Money, error) {
	total, err := ZeroMoney(c)
	if err != nil {
		return Money{}, err
	}
	for _, v := range values {
		if total, err = total.Add(v); err != nil {
			return Money{}, err
		}
	}
	return total, nil
}

type BasisPoints struct {
	value int32
}

const MaxBasisPoints = 10_000

func NewBasisPoints(v int) (BasisPoints, error) {
	if v < 0 || v > MaxBasisPoints {
		return BasisPoints{}, ErrInvalidBasis.WithDetail("%d", v)
	}
	return BasisPoints{value: int32(v)}, nil
}

func MustBasisPoints(v int) BasisPoints {
	bp, err := NewBasisPoints(v)
	if err != nil {
		panic(err)
	}
	return bp
}

func (b BasisPoints) Value() int { return int(b.value) }

func (b BasisPoints) IsZero() bool { return b.value == 0 }

func (b BasisPoints) String() string {
	return fmt.Sprintf("%d.%02d%%", b.value/100, b.value%100)
}
