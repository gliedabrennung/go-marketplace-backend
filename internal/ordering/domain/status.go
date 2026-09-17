package domain

import "slices"

type Status string

const (
	StatusCreated         Status = "created"
	StatusAwaitingPayment Status = "awaiting_payment"
	StatusPaid            Status = "paid"
	StatusInFulfilment    Status = "in_fulfilment"
	StatusShipped         Status = "shipped"
	StatusDelivered       Status = "delivered"
	StatusCompleted       Status = "completed"
	StatusCancelled       Status = "cancelled"
	StatusFailed          Status = "failed"
	StatusReturning       Status = "returning"
	StatusReturned        Status = "returned"
)

var transitions = map[Status][]Status{
	StatusCreated:         {StatusAwaitingPayment, StatusCancelled, StatusFailed},
	StatusAwaitingPayment: {StatusPaid, StatusCancelled, StatusFailed},
	StatusPaid:            {StatusInFulfilment, StatusCancelled},
	StatusInFulfilment:    {StatusShipped, StatusCancelled},
	StatusShipped:         {StatusDelivered, StatusReturning, StatusCancelled},
	StatusDelivered:       {StatusCompleted, StatusReturning},
	StatusReturning:       {StatusReturned},
}

var cancellable = []Status{StatusCreated, StatusAwaitingPayment, StatusPaid, StatusInFulfilment}

func ParseStatus(raw string) (Status, bool) {
	status := Status(raw)
	_, known := transitions[status]
	return status, known || status.IsTerminal()
}

func (s Status) CanTransitionTo(target Status) error {
	if !slices.Contains(transitions[s], target) {
		return &TransitionError{From: s, To: target}
	}
	return nil
}

func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled || s == StatusFailed || s == StatusReturned
}

func (s Status) IsPaid() bool {
	switch s {
	case StatusPaid, StatusInFulfilment, StatusShipped, StatusDelivered, StatusCompleted, StatusReturning, StatusReturned:
		return true
	}
	return false
}

func (s Status) VisibleToSeller() bool {
	return s.IsPaid() || s == StatusCancelled
}

type ActorKind string

const (
	ActorBuyer   ActorKind = "buyer"
	ActorSeller  ActorKind = "seller"
	ActorSupport ActorKind = "support"
	ActorSystem  ActorKind = "system"
)

type Actor struct {
	Kind ActorKind
	ID   string
}

func System() Actor { return Actor{Kind: ActorSystem} }

func (a Actor) valid() bool {
	switch a.Kind {
	case ActorSystem:
		return true
	case ActorBuyer, ActorSeller, ActorSupport:
		return a.ID != ""
	}
	return false
}

func (s Status) Cancellable() bool {
	return slices.Contains(cancellable, s)
}
