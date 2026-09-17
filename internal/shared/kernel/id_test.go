package kernel_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func TestNewID_IsVersion7(t *testing.T) {
	id := kernel.NewID[struct{}]()
	s := id.String()
	require.Len(t, s, 36)
	assert.Equal(t, byte('7'), s[14])
	assert.Contains(t, "89ab", string(s[19]))
	assert.False(t, id.IsZero())
}

func TestNewID_IsTimeOrdered(t *testing.T) {
	first := kernel.NewID[struct{}]().String()
	for range 1000 {
		next := kernel.NewID[struct{}]().String()
		require.LessOrEqual(t, first[:13], next[:13])
	}
}

func TestNewID_IsUnique(t *testing.T) {
	seen := make(map[string]struct{}, 10_000)
	for range 10_000 {
		s := kernel.NewID[struct{}]().String()
		_, dup := seen[s]
		require.False(t, dup)
		seen[s] = struct{}{}
	}
}

func TestParseID(t *testing.T) {
	id, err := kernel.ParseID[struct{}]("0190F5A2-7C3E-7B1A-9C2D-1E2F3A4B5C6D")
	require.NoError(t, err)
	assert.Equal(t, strings.ToLower("0190F5A2-7C3E-7B1A-9C2D-1E2F3A4B5C6D"), id.String())

	invalid := []string{
		"",
		"not-a-uuid",
		"0190f5a2x7c3e-7b1a-9c2d-1e2f3a4b5c6d",
		"0190f5a2-7c3e-7b1a-9c2d-1e2f3a4b5c6g",
		"00000000-0000-0000-0000-000000000000",
	}
	for _, s := range invalid {
		_, err := kernel.ParseID[struct{}](s)
		require.ErrorIs(t, err, kernel.ErrInvalidID, s)
	}
}

func TestBuyerUserConversion(t *testing.T) {
	user := kernel.NewUserID()
	parsed, err := kernel.ParseUserID(user.String())
	require.NoError(t, err)
	assert.Equal(t, user, parsed)

	buyer := kernel.BuyerIDOf(user)
	assert.Equal(t, user.String(), buyer.String())
	assert.Equal(t, user, kernel.UserIDOfBuyer(buyer))

	var zero kernel.UserID
	assert.True(t, zero.IsZero())
}
