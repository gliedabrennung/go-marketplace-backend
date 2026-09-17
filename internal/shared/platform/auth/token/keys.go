package token

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

type KeyRing struct {
	activeKID string
	private   map[string]*ecdsa.PrivateKey
	public    map[string]*ecdsa.PublicKey
}

func (k *KeyRing) Signing() (string, *ecdsa.PrivateKey, error) {
	key, ok := k.private[k.activeKID]
	if !ok {
		return "", nil, fmt.Errorf("signing key %q not loaded", k.activeKID)
	}
	return k.activeKID, key, nil
}

func (k *KeyRing) Public(kid string) (*ecdsa.PublicKey, bool) {
	key, ok := k.public[kid]
	return key, ok
}

func GenerateKeyRing(kid string) (*KeyRing, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ecdsa key: %w", err)
	}
	return &KeyRing{
		activeKID: kid,
		private:   map[string]*ecdsa.PrivateKey{kid: key},
		public:    map[string]*ecdsa.PublicKey{kid: &key.PublicKey},
	}, nil
}

func LoadKeyRing(dir, activeKID string) (ring *KeyRing, err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open key dir: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close key dir: %w", closeErr)
		}
	}()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read key dir: %w", err)
	}
	ring = &KeyRing{
		activeKID: activeKID,
		private:   make(map[string]*ecdsa.PrivateKey),
		public:    make(map[string]*ecdsa.PublicKey),
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".pem") {
			continue
		}
		raw, err := root.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read key %s: %w", name, err)
		}
		if err := ring.add(strings.TrimSuffix(strings.TrimSuffix(name, ".pem"), ".pub"), raw); err != nil {
			return nil, fmt.Errorf("parse key %s: %w", name, err)
		}
	}
	if _, _, err := ring.Signing(); err != nil {
		return nil, err
	}
	return ring, nil
}

func (k *KeyRing) add(kid string, raw []byte) error {
	block, _ := pem.Decode(raw)
	if block == nil {
		return errors.New("no PEM block")
	}
	switch block.Type {
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return err
		}
		k.private[kid], k.public[kid] = key, &key.PublicKey
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return err
		}
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return errors.New("private key is not ECDSA")
		}
		k.private[kid], k.public[kid] = key, &key.PublicKey
	case "PUBLIC KEY":
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return err
		}
		key, ok := parsed.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("public key is not ECDSA")
		}
		k.public[kid] = key
	default:
		return fmt.Errorf("unsupported PEM type %q", block.Type)
	}
	return nil
}
