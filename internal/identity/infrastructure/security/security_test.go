package security_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/security"
)

func fastParams() security.Argon2Params {
	return security.Argon2Params{MemoryKiB: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

func TestArgon2id_HashAndVerify(t *testing.T) {
	h, err := security.NewArgon2idHasher(fastParams())
	require.NoError(t, err)
	pw, err := domain.NewPassword("correct horse 42")
	require.NoError(t, err)

	hash, err := h.Hash(pw)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash.String(), "$argon2id$v=19$m=1024,t=1,p=1$"))
	assert.NotContains(t, hash.String(), "correct horse")

	ok, err := h.Verify(hash, "correct horse 42")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = h.Verify(hash, "wrong horse 42")
	require.NoError(t, err)
	assert.False(t, ok)

	again, err := h.Hash(pw)
	require.NoError(t, err)
	assert.NotEqual(t, hash.String(), again.String(), "salt must be random")
}

func TestArgon2id_ZeroHashNeverMatches(t *testing.T) {
	h, err := security.NewArgon2idHasher(fastParams())
	require.NoError(t, err)
	ok, err := h.Verify(domain.PasswordHash{}, "dummy-password-1")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestArgon2id_VerifyUsesStoredParameters(t *testing.T) {
	old, err := security.NewArgon2idHasher(fastParams())
	require.NoError(t, err)
	pw, err := domain.NewPassword("correct horse 42")
	require.NoError(t, err)
	hash, err := old.Hash(pw)
	require.NoError(t, err)

	stronger := fastParams()
	stronger.Iterations = 2
	current, err := security.NewArgon2idHasher(stronger)
	require.NoError(t, err)
	ok, err := current.Verify(hash, "correct horse 42")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestArgon2id_RejectsMalformedHashes(t *testing.T) {
	h, err := security.NewArgon2idHasher(fastParams())
	require.NoError(t, err)
	for _, raw := range []string{
		"plain",
		"$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=18$m=1,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$bad$c2FsdA$a2V5",
		"$argon2id$v=19$m=1,t=1,p=1$!!!$a2V5",
		"$argon2id$v=19$m=1,t=1,p=1$c2FsdA$",
	} {
		hash, err := domain.NewPasswordHash(raw)
		require.NoError(t, err)
		_, err = h.Verify(hash, "x")
		require.Error(t, err, raw)
	}
}

func TestNewArgon2idHasher_ValidatesParameters(t *testing.T) {
	_, err := security.NewArgon2idHasher(security.Argon2Params{})
	require.Error(t, err)
	assert.Equal(t, uint32(64*1024), security.DefaultArgon2Params().MemoryKiB)
}

func TestSecrets(t *testing.T) {
	s := security.Secrets{}
	a, err := s.Token()
	require.NoError(t, err)
	b, err := s.Token()
	require.NoError(t, err)
	assert.Len(t, a, 43)
	assert.NotEqual(t, a, b)
	assert.NotContains(t, a, ".")
}
