package outbox_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

type samplePlaced struct {
	id  string
	at  time.Time
	sum int64
}

func (e samplePlaced) EventName() string     { return "sample.placed.v1" }
func (e samplePlaced) AggregateID() string   { return e.id }
func (e samplePlaced) OccurredAt() time.Time { return e.at }

type sampleRemoved struct{ samplePlaced }

func TestCodec_EncodesRegisteredEvent(t *testing.T) {
	c := outbox.NewCodec()
	outbox.Register(c, func(e samplePlaced) any {
		return struct {
			ID  string `json:"id"`
			Sum int64  `json:"sum"`
		}{ID: e.id, Sum: e.sum}
	})

	raw, err := c.Encode(samplePlaced{id: "a-1", sum: 500, at: time.Now()})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"a-1","sum":500}`, string(raw))
}

func TestCodec_RejectsUnregisteredEvent(t *testing.T) {
	c := outbox.NewCodec()
	_, err := c.Encode(sampleRemoved{})
	require.Error(t, err)
}
