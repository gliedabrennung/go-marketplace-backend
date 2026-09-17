package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
)

type Argon2Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

func DefaultArgon2Params() Argon2Params {
	return Argon2Params{MemoryKiB: 64 * 1024, Iterations: 2, Parallelism: 2, SaltLength: 16, KeyLength: 32}
}

var errMalformedHash = errors.New("malformed argon2id hash")

type Argon2idHasher struct {
	params Argon2Params
	dummy  domain.PasswordHash
}

func NewArgon2idHasher(params Argon2Params) (*Argon2idHasher, error) {
	if params.MemoryKiB == 0 || params.Iterations == 0 || params.Parallelism == 0 || params.SaltLength < 16 || params.KeyLength < 16 {
		return nil, fmt.Errorf("invalid argon2id parameters: %+v", params)
	}
	h := &Argon2idHasher{params: params}
	pw, err := domain.NewPassword("dummy-password-1")
	if err != nil {
		return nil, err
	}
	if h.dummy, err = h.Hash(pw); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *Argon2idHasher) Hash(password domain.Password) (domain.PasswordHash, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return domain.PasswordHash{}, fmt.Errorf("generate salt: %w", err)
	}
	p := h.params
	key := argon2.IDKey([]byte(password.Reveal()), salt, p.Iterations, p.MemoryKiB, p.Parallelism, p.KeyLength)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
	return domain.NewPasswordHash(encoded)
}

func (h *Argon2idHasher) Verify(hash domain.PasswordHash, password string) (bool, error) {
	target := hash
	if target.IsZero() {
		target = h.dummy
	}
	p, salt, key, err := decodeArgon2id(target.String())
	if err != nil {
		return false, err
	}
	computed := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(key)))
	match := subtle.ConstantTimeCompare(computed, key) == 1
	return match && !hash.IsZero(), nil
}

func decodeArgon2id(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Parallelism); err != nil {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	return p, salt, key, nil
}
