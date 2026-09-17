package psp

import (
	"github.com/gliedabrennung/go-marketplace-backend/internal/payment/application"
)

type Registry struct {
	providers map[string]application.Provider
	fallback  application.Provider
}

func NewRegistry(fallback application.Provider, others ...application.Provider) *Registry {
	r := &Registry{providers: map[string]application.Provider{fallback.Name(): fallback}, fallback: fallback}
	for _, provider := range others {
		r.providers[provider.Name()] = provider
	}
	return r
}

func (r *Registry) Get(name string) (application.Provider, error) {
	provider, ok := r.providers[name]
	if !ok {
		return nil, application.ErrUnknownProvider.WithDetail("provider %q", name)
	}
	return provider, nil
}

func (r *Registry) Default() application.Provider {
	return r.fallback
}
