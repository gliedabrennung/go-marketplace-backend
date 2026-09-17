package domain

import (
	"regexp"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type cartTag struct{}

type CartID = kernel.ID[cartTag]

func NewCartID() CartID { return kernel.NewID[cartTag]() }

func ParseCartID(s string) (CartID, error) { return kernel.ParseID[cartTag](s) }

type OwnerKind string

const (
	OwnerUser   OwnerKind = "user"
	OwnerDevice OwnerKind = "device"
)

var devicePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

type Owner struct {
	kind OwnerKind
	id   string
}

func UserOwner(id kernel.UserID) (Owner, error) {
	if id.IsZero() {
		return Owner{}, kernel.ErrInvalidID
	}
	return Owner{kind: OwnerUser, id: id.String()}, nil
}

func DeviceOwner(raw string) (Owner, error) {
	value := strings.TrimSpace(raw)
	if !devicePattern.MatchString(value) {
		return Owner{}, ErrInvalidDevice
	}
	return Owner{kind: OwnerDevice, id: value}, nil
}

func RehydrateOwner(kind, id string) (Owner, error) {
	switch OwnerKind(kind) {
	case OwnerUser:
		user, err := kernel.ParseUserID(id)
		if err != nil {
			return Owner{}, err
		}
		return UserOwner(user)
	case OwnerDevice:
		return DeviceOwner(id)
	default:
		return Owner{}, ErrInvalidDevice
	}
}

func (o Owner) Kind() OwnerKind { return o.kind }

func (o Owner) ID() string { return o.id }

func (o Owner) IsAnonymous() bool { return o.kind == OwnerDevice }

func (o Owner) IsZero() bool { return o.kind == "" }

type Limits struct {
	MaxItems     int
	MaxQuantity  int
	AnonymousTTL time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxItems: 100, MaxQuantity: 999, AnonymousTTL: 30 * 24 * time.Hour}
}

var (
	skuPattern   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	promoPattern = regexp.MustCompile(`^[A-Z0-9-]{4,32}$`)
)

func NormalizeSKU(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if !skuPattern.MatchString(value) {
		return "", ErrInvalidSKU
	}
	return value, nil
}

func NormalizePromoCode(raw string) (string, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if !promoPattern.MatchString(value) {
		return "", ErrInvalidPromoCode
	}
	return value, nil
}
