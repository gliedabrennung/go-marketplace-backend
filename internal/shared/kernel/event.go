package kernel

import "time"

type DomainEvent interface {
	EventName() string
	AggregateID() string
	OccurredAt() time.Time
}

type EventBuffer struct {
	events []DomainEvent
}

func (b *EventBuffer) Record(e DomainEvent) {
	b.events = append(b.events, e)
}

func (b *EventBuffer) Pull() []DomainEvent {
	out := b.events
	b.events = nil
	return out
}

func (b *EventBuffer) Peek() []DomainEvent {
	out := make([]DomainEvent, len(b.events))
	copy(out, b.events)
	return out
}
