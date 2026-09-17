package outbox

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Codec struct {
	mappers map[reflect.Type]func(kernel.DomainEvent) any
}

func NewCodec() *Codec {
	return &Codec{mappers: make(map[reflect.Type]func(kernel.DomainEvent) any)}
}

func Register[E kernel.DomainEvent](c *Codec, toPayload func(E) any) {
	c.mappers[reflect.TypeFor[E]()] = func(e kernel.DomainEvent) any {
		return toPayload(e.(E))
	}
}

func (c *Codec) Encode(e kernel.DomainEvent) ([]byte, error) {
	mapper, ok := c.mappers[reflect.TypeOf(e)]
	if !ok {
		return nil, fmt.Errorf("outbox codec: event %T is not registered", e)
	}
	raw, err := json.Marshal(mapper(e))
	if err != nil {
		return nil, fmt.Errorf("outbox codec: marshal %s: %w", e.EventName(), err)
	}
	return raw, nil
}
