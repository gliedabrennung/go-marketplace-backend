package domain

import (
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type SavedMethod struct {
	id        MethodID
	buyerID   kernel.UserID
	provider  string
	token     string
	label     string
	createdAt time.Time
	removedAt time.Time
	version   int
}

func SaveMethod(id MethodID, buyer kernel.UserID, provider, token, label string, now time.Time) (*SavedMethod, error) {
	if id.IsZero() || buyer.IsZero() {
		return nil, kernel.ErrInvalidID
	}
	provider, token, label = strings.TrimSpace(provider), strings.TrimSpace(token), strings.TrimSpace(label)
	if provider == "" || token == "" || label == "" {
		return nil, ErrInvalidMethod
	}
	return &SavedMethod{id: id, buyerID: buyer, provider: provider, token: token, label: label, createdAt: now}, nil
}

func (m *SavedMethod) Remove(buyer kernel.UserID, now time.Time) error {
	if buyer != m.buyerID {
		return ErrNotMethodOwner
	}
	if m.IsRemoved() {
		return nil
	}
	m.removedAt = now
	return nil
}

func (m *SavedMethod) Usable(buyer kernel.UserID, provider string) error {
	if buyer != m.buyerID || m.IsRemoved() {
		return ErrMethodNotFound
	}
	if provider != m.provider {
		return ErrInvalidMethod.WithDetail("method belongs to provider %s", m.provider)
	}
	return nil
}

func (m *SavedMethod) ID() MethodID { return m.id }

func (m *SavedMethod) BuyerID() kernel.UserID { return m.buyerID }

func (m *SavedMethod) Provider() string { return m.provider }

func (m *SavedMethod) Token() string { return m.token }

func (m *SavedMethod) Label() string { return m.label }

func (m *SavedMethod) IsRemoved() bool { return !m.removedAt.IsZero() }

func (m *SavedMethod) Version() int { return m.version }

func (m *SavedMethod) AdvanceVersion() { m.version++ }

type SavedMethodSnapshot struct {
	ID        string
	BuyerID   string
	Provider  string
	Token     string
	Label     string
	CreatedAt time.Time
	RemovedAt time.Time
	Version   int
}

func (m *SavedMethod) Snapshot() SavedMethodSnapshot {
	return SavedMethodSnapshot{
		ID: m.id.String(), BuyerID: m.buyerID.String(), Provider: m.provider, Token: m.token, Label: m.label,
		CreatedAt: m.createdAt, RemovedAt: m.removedAt, Version: m.version,
	}
}

func RehydrateSavedMethod(s SavedMethodSnapshot) (*SavedMethod, error) {
	id, err := ParseMethodID(s.ID)
	if err != nil {
		return nil, err
	}
	buyer, err := kernel.ParseUserID(s.BuyerID)
	if err != nil {
		return nil, err
	}
	return &SavedMethod{
		id: id, buyerID: buyer, provider: s.Provider, token: s.Token, label: s.Label,
		createdAt: s.CreatedAt, removedAt: s.RemovedAt, version: s.Version,
	}, nil
}
