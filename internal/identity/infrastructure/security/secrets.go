package security

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

type Secrets struct{}

func (Secrets) Token() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
