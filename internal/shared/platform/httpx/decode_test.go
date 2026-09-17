package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

type sample struct {
	Name string `json:"name"`
}

func decode(body string, limit int64) (sample, error) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if limit > 0 {
		req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, limit)
	}
	var s sample
	err := httpx.DecodeJSON(req, &s)
	return s, err
}

func TestDecodeJSON(t *testing.T) {
	s, err := decode(`{"name":"ok"}`, 0)
	require.NoError(t, err)
	assert.Equal(t, "ok", s.Name)

	_, err = decode(`{"name":"ok","extra":1}`, 0)
	require.ErrorIs(t, err, httpx.ErrMalformedBody)

	_, err = decode(`{"name":"ok"}{"name":"again"}`, 0)
	require.ErrorIs(t, err, httpx.ErrMalformedBody)

	_, err = decode(``, 0)
	require.ErrorIs(t, err, httpx.ErrEmptyBody)

	_, err = decode(`{"name":"`+strings.Repeat("x", 100)+`"}`, 16)
	require.ErrorIs(t, err, httpx.ErrBodyTooLarge)
}
