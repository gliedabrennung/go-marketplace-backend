package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type ID[T any] struct {
	value string
}

func NewID[T any]() ID[T] {
	return ID[T]{value: newUUIDv7(time.Now())}
}

func ParseID[T any](s string) (ID[T], error) {
	normalized, ok := normalizeUUID(s)
	if !ok {
		return ID[T]{}, ErrInvalidID.WithDetail("%q", s)
	}
	return ID[T]{value: normalized}, nil
}

func MustParseID[T any](s string) ID[T] {
	id, err := ParseID[T](s)
	if err != nil {
		panic(err)
	}
	return id
}

func (id ID[T]) String() string { return id.value }

func (id ID[T]) IsZero() bool { return id.value == "" }

type (
	userTag    struct{}
	buyerTag   struct{}
	sellerTag  struct{}
	paymentTag struct{}
)

type (
	UserID    = ID[userTag]
	BuyerID   = ID[buyerTag]
	SellerID  = ID[sellerTag]
	PaymentID = ID[paymentTag]
)

func NewUserID() UserID { return NewID[userTag]() }

func NewSellerID() SellerID { return NewID[sellerTag]() }

func NewPaymentID() PaymentID { return NewID[paymentTag]() }

func ParseUserID(s string) (UserID, error) { return ParseID[userTag](s) }

func ParseBuyerID(s string) (BuyerID, error) { return ParseID[buyerTag](s) }

func ParseSellerID(s string) (SellerID, error) { return ParseID[sellerTag](s) }

func ParsePaymentID(s string) (PaymentID, error) { return ParseID[paymentTag](s) }

func BuyerIDOf(u UserID) BuyerID {
	return BuyerID(u)
}

func UserIDOfBuyer(b BuyerID) UserID {
	return UserID(b)
}

func newUUIDv7(now time.Time) string {
	var b [16]byte
	ms := uint64(now.UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUID(b)
}

func formatUUID(b [16]byte) string {
	var dst [36]byte
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst[:])
}

func normalizeUUID(s string) (string, bool) {
	if len(s) != 36 {
		return "", false
	}
	out := make([]byte, 36)
	for i := range 36 {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return "", false
			}
			out[i] = c
			continue
		}
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
			out[i] = c
		case c >= 'A' && c <= 'F':
			out[i] = c + ('a' - 'A')
		default:
			return "", false
		}
	}
	if string(out) == "00000000-0000-0000-0000-000000000000" {
		return "", false
	}
	return string(out), true
}
