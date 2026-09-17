package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	ErrMalformedBody = kernel.Validation("MALFORMED_BODY", "request body is not valid JSON")
	ErrBodyTooLarge  = kernel.Validation("BODY_TOO_LARGE", "request body is too large")
	ErrEmptyBody     = kernel.Validation("EMPTY_BODY", "request body is required")
)

func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			return ErrBodyTooLarge.WithDetail("limit %d bytes", tooLarge.Limit)
		case errors.Is(err, io.EOF):
			return ErrEmptyBody
		default:
			return ErrMalformedBody.WithDetail("%s", err.Error())
		}
	}
	if dec.More() {
		return ErrMalformedBody.WithDetail("unexpected data after JSON object")
	}
	return nil
}
