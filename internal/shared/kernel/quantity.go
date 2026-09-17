package kernel

import "math"

const MaxQuantity = math.MaxInt32

var (
	ErrNegativeQuantity = Validation("NEGATIVE_QUANTITY", "quantity must not be negative")
	ErrQuantityOverflow = BusinessRule("QUANTITY_OVERFLOW", "quantity overflow")
)

type Quantity struct {
	value int
}

func NewQuantity(v int) (Quantity, error) {
	if v < 0 {
		return Quantity{}, ErrNegativeQuantity.WithDetail("%d", v)
	}
	if v > MaxQuantity {
		return Quantity{}, ErrQuantityOverflow.WithDetail("%d", v)
	}
	return Quantity{value: v}, nil
}

func MustQuantity(v int) Quantity {
	q, err := NewQuantity(v)
	if err != nil {
		panic(err)
	}
	return q
}

func (q Quantity) Value() int { return q.value }

func (q Quantity) IsZero() bool { return q.value == 0 }

func (q Quantity) Add(other Quantity) (Quantity, error) {
	if other.value > MaxQuantity-q.value {
		return Quantity{}, ErrQuantityOverflow
	}
	return Quantity{value: q.value + other.value}, nil
}

func (q Quantity) Sub(other Quantity) (Quantity, error) {
	if q.value < other.value {
		return Quantity{}, ErrNegativeQuantity
	}
	return Quantity{value: q.value - other.value}, nil
}

func (q Quantity) LessThan(other Quantity) bool { return q.value < other.value }

func (q Quantity) Equals(other Quantity) bool { return q.value == other.value }
