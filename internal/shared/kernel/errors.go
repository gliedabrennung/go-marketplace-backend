package kernel

import (
	"errors"
	"fmt"
)

type ErrorKind uint8

const (
	KindInternal ErrorKind = iota
	KindValidation
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindBusinessRule
	KindRateLimited
)

func (k ErrorKind) String() string {
	switch k {
	case KindValidation:
		return "validation"
	case KindUnauthenticated:
		return "unauthenticated"
	case KindForbidden:
		return "forbidden"
	case KindNotFound:
		return "not_found"
	case KindConflict:
		return "conflict"
	case KindBusinessRule:
		return "business_rule"
	case KindRateLimited:
		return "rate_limited"
	default:
		return "internal"
	}
}

type FieldViolation struct {
	Field   string
	Code    string
	Message string
}

type Error struct {
	kind    ErrorKind
	code    string
	message string
	detail  string
	fields  []FieldViolation
}

func NewError(kind ErrorKind, code, message string) *Error {
	return &Error{kind: kind, code: code, message: message}
}

func Validation(code, message string, fields ...FieldViolation) *Error {
	e := NewError(KindValidation, code, message)
	e.fields = append(e.fields, fields...)
	return e
}

func Unauthenticated(code, message string) *Error {
	return NewError(KindUnauthenticated, code, message)
}

func Forbidden(code, message string) *Error {
	return NewError(KindForbidden, code, message)
}

func NotFound(code, message string) *Error {
	return NewError(KindNotFound, code, message)
}

func Conflict(code, message string) *Error {
	return NewError(KindConflict, code, message)
}

func BusinessRule(code, message string) *Error {
	return NewError(KindBusinessRule, code, message)
}

func RateLimited(code, message string) *Error {
	return NewError(KindRateLimited, code, message)
}

func (e *Error) Error() string {
	if e.detail == "" {
		return e.message
	}
	return e.message + ": " + e.detail
}

func (e *Error) Kind() ErrorKind { return e.kind }

func (e *Error) Code() string { return e.code }

func (e *Error) Message() string { return e.message }

func (e *Error) Detail() string { return e.detail }

func (e *Error) Fields() []FieldViolation {
	out := make([]FieldViolation, len(e.fields))
	copy(out, e.fields)
	return out
}

func (e *Error) WithDetail(format string, args ...any) *Error {
	c := e.clone()
	c.detail = fmt.Sprintf(format, args...)
	return c
}

func (e *Error) WithFields(fields ...FieldViolation) *Error {
	c := e.clone()
	c.fields = append(c.fields, fields...)
	return c
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.kind == t.kind && e.code == t.code
}

func (e *Error) clone() *Error {
	c := *e
	c.fields = make([]FieldViolation, len(e.fields))
	copy(c.fields, e.fields)
	return &c
}

type kinded interface {
	Kind() ErrorKind
}

type coded interface {
	Code() string
}

func KindOf(err error) ErrorKind {
	var k kinded
	if errors.As(err, &k) {
		return k.Kind()
	}
	return KindInternal
}

func CodeOf(err error) string {
	var c coded
	if errors.As(err, &c) {
		return c.Code()
	}
	return "INTERNAL"
}

var (
	ErrConcurrentModification = Conflict("CONCURRENT_MODIFICATION", "aggregate was modified concurrently")
	ErrInvalidID              = Validation("INVALID_ID", "identifier is not a valid UUID")
)
