package domain

import "time"

type MovementReason string

const (
	MovementReserve    MovementReason = "reserve"
	MovementRelease    MovementReason = "release"
	MovementCommit     MovementReason = "commit"
	MovementRestock    MovementReason = "restock"
	MovementReturn     MovementReason = "return"
	MovementCorrection MovementReason = "correction"
)

type Movement struct {
	SKU         string
	Delta       int
	Reason      MovementReason
	ReferenceID string
	OccurredAt  time.Time
}
