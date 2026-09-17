package domain

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type AttributeType string

const (
	AttributeString  AttributeType = "string"
	AttributeNumber  AttributeType = "number"
	AttributeBoolean AttributeType = "boolean"
	AttributeEnum    AttributeType = "enum"
	AttributeUnit    AttributeType = "unit"
)

const (
	maxEnumOptions     = 200
	maxAttributeText   = 1000
	maxAttributeName   = 100
	maxAttributeCode   = 64
	maxAttributeUnit   = 16
	maxEnumOptionChars = 100
)

type AttributeDefinition struct {
	code       string
	name       string
	typ        AttributeType
	required   bool
	filterable bool
	options    []string
	unit       string
}

type AttributeSpec struct {
	Code       string
	Name       string
	Type       string
	Required   bool
	Filterable bool
	Options    []string
	Unit       string
}

func NewAttributeDefinition(spec AttributeSpec) (AttributeDefinition, error) {
	if !validCode(spec.Code) {
		return AttributeDefinition{}, ErrInvalidAttributeCode.WithDetail("%q", spec.Code)
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" || utf8.RuneCountInString(name) > maxAttributeName {
		return AttributeDefinition{}, ErrInvalidAttributeName
	}
	typ := AttributeType(spec.Type)
	switch typ {
	case AttributeString, AttributeNumber, AttributeBoolean, AttributeEnum, AttributeUnit:
	default:
		return AttributeDefinition{}, ErrInvalidAttributeType.WithDetail("%q", spec.Type)
	}
	options, err := normalizeOptions(typ, spec.Options)
	if err != nil {
		return AttributeDefinition{}, err
	}
	unit := strings.TrimSpace(spec.Unit)
	if (typ == AttributeUnit) != (unit != "") || utf8.RuneCountInString(unit) > maxAttributeUnit {
		return AttributeDefinition{}, ErrInvalidAttributeUnit
	}
	return AttributeDefinition{
		code:       spec.Code,
		name:       name,
		typ:        typ,
		required:   spec.Required,
		filterable: spec.Filterable,
		options:    options,
		unit:       unit,
	}, nil
}

func normalizeOptions(typ AttributeType, raw []string) ([]string, error) {
	if typ != AttributeEnum {
		if len(raw) > 0 {
			return nil, ErrInvalidAttributeOptions
		}
		return nil, nil
	}
	if len(raw) == 0 || len(raw) > maxEnumOptions {
		return nil, ErrInvalidAttributeOptions
	}
	out := make([]string, 0, len(raw))
	for _, o := range raw {
		o = strings.TrimSpace(o)
		if o == "" || utf8.RuneCountInString(o) > maxEnumOptionChars || slices.Contains(out, o) {
			return nil, ErrInvalidAttributeOptions
		}
		out = append(out, o)
	}
	return out, nil
}

func validCode(code string) bool {
	if code == "" || len(code) > maxAttributeCode || code[0] < 'a' || code[0] > 'z' {
		return false
	}
	for i := range len(code) {
		c := code[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func (d AttributeDefinition) Code() string { return d.code }

func (d AttributeDefinition) Name() string { return d.name }

func (d AttributeDefinition) Type() AttributeType { return d.typ }

func (d AttributeDefinition) Required() bool { return d.required }

func (d AttributeDefinition) Filterable() bool { return d.filterable }

func (d AttributeDefinition) Options() []string { return slices.Clone(d.options) }

func (d AttributeDefinition) Unit() string { return d.unit }

func (d AttributeDefinition) Spec() AttributeSpec {
	return AttributeSpec{
		Code:       d.code,
		Name:       d.name,
		Type:       string(d.typ),
		Required:   d.required,
		Filterable: d.filterable,
		Options:    slices.Clone(d.options),
		Unit:       d.unit,
	}
}

func (d AttributeDefinition) Parse(raw string) (AttributeValue, error) {
	v, violation := d.parse(raw)
	if violation != nil {
		return AttributeValue{}, ErrInvalidAttributes.WithFields(*violation)
	}
	return v, nil
}

func (d AttributeDefinition) parse(raw string) (AttributeValue, *kernel.FieldViolation) {
	raw = strings.TrimSpace(raw)
	switch d.typ {
	case AttributeNumber, AttributeUnit:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return AttributeValue{}, d.violation("must be a number")
		}
		return AttributeValue{typ: d.typ, number: n}, nil
	case AttributeBoolean:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return AttributeValue{}, d.violation("must be true or false")
		}
		return AttributeValue{typ: d.typ, boolean: b}, nil
	case AttributeEnum:
		if !slices.Contains(d.options, raw) {
			return AttributeValue{}, d.violation("must be one of the allowed options")
		}
		return AttributeValue{typ: d.typ, text: raw}, nil
	default:
		if raw == "" || utf8.RuneCountInString(raw) > maxAttributeText {
			return AttributeValue{}, d.violation("must be 1-1000 characters long")
		}
		return AttributeValue{typ: d.typ, text: raw}, nil
	}
}

func (d AttributeDefinition) violation(message string) *kernel.FieldViolation {
	return &kernel.FieldViolation{Field: "attributes." + d.code, Code: "INVALID_VALUE", Message: message}
}

type AttributeValue struct {
	typ     AttributeType
	text    string
	number  float64
	boolean bool
}

func (v AttributeValue) Type() AttributeType { return v.typ }

func (v AttributeValue) Number() (float64, bool) {
	return v.number, v.typ == AttributeNumber || v.typ == AttributeUnit
}

func (v AttributeValue) String() string {
	switch v.typ {
	case AttributeNumber, AttributeUnit:
		return strconv.FormatFloat(v.number, 'f', -1, 64)
	case AttributeBoolean:
		return strconv.FormatBool(v.boolean)
	default:
		return v.text
	}
}

func (v AttributeValue) IsZero() bool { return v.typ == "" }
