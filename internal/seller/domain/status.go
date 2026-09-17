package domain

import "slices"

type SellerStatus string

const (
	StatusDraft         SellerStatus = "draft"
	StatusPendingReview SellerStatus = "pending_review"
	StatusActive        SellerStatus = "active"
	StatusSuspended     SellerStatus = "suspended"
	StatusTerminated    SellerStatus = "terminated"
)

var sellerTransitions = map[SellerStatus][]SellerStatus{
	StatusDraft:         {StatusPendingReview},
	StatusPendingReview: {StatusActive, StatusDraft},
	StatusActive:        {StatusSuspended, StatusTerminated},
	StatusSuspended:     {StatusActive, StatusTerminated},
	StatusTerminated:    {},
}

func (s SellerStatus) CanTransitionTo(target SellerStatus) error {
	if slices.Contains(sellerTransitions[s], target) {
		return nil
	}
	return &TransitionError{From: s, To: target}
}

func (s SellerStatus) IsTerminal() bool {
	return len(sellerTransitions[s]) == 0
}
