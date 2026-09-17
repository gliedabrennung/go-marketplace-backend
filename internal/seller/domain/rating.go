package domain

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type PerformanceMetrics struct {
	Orders            int
	CancelledBySeller int
	LateShipments     int
	Reviews           int
	ReviewScoreSum    int
}

func (m PerformanceMetrics) Validate() error {
	switch {
	case m.Orders < 0, m.CancelledBySeller < 0, m.LateShipments < 0, m.Reviews < 0, m.ReviewScoreSum < 0:
		return ErrInvalidMetrics.WithDetail("values must not be negative")
	case m.CancelledBySeller > m.Orders, m.LateShipments > m.Orders:
		return ErrInvalidMetrics.WithDetail("cancellations and late shipments cannot exceed orders")
	case m.ReviewScoreSum < m.Reviews || m.ReviewScoreSum > 5*m.Reviews:
		return ErrInvalidMetrics.WithDetail("review scores must be between 1 and 5")
	default:
		return nil
	}
}

type Rating struct {
	score            int
	cancellationRate kernel.BasisPoints
	lateShipmentRate kernel.BasisPoints
	averageReview    int
	orders           int
	provisional      bool
	calculatedAt     time.Time
}

func (r Rating) Score() int { return r.score }

func (r Rating) CancellationRate() kernel.BasisPoints { return r.cancellationRate }

func (r Rating) LateShipmentRate() kernel.BasisPoints { return r.lateShipmentRate }

func (r Rating) AverageReview() int { return r.averageReview }

func (r Rating) Orders() int { return r.orders }

func (r Rating) Provisional() bool { return r.provisional }

func (r Rating) CalculatedAt() time.Time { return r.calculatedAt }

func (r Rating) IsZero() bool { return r.calculatedAt.IsZero() }

type RatingPolicy struct {
	MinOrders          int
	SuspendBelow       int
	CancellationWeight int
	LatenessWeight     int
	ReviewWeight       int
}

const neutralReviewScore = 80

func DefaultRatingPolicy() RatingPolicy {
	return RatingPolicy{
		MinOrders:          20,
		SuspendBelow:       40,
		CancellationWeight: 35,
		LatenessWeight:     25,
		ReviewWeight:       40,
	}
}

func (p RatingPolicy) Validate() error {
	weights := p.CancellationWeight + p.LatenessWeight + p.ReviewWeight
	if p.MinOrders < 0 || p.SuspendBelow < 0 || p.SuspendBelow > 100 ||
		p.CancellationWeight < 0 || p.LatenessWeight < 0 || p.ReviewWeight < 0 || weights == 0 {
		return ErrInvalidRatingPolicy
	}
	return nil
}

func (p RatingPolicy) Calculate(m PerformanceMetrics, now time.Time) (Rating, error) {
	if err := p.Validate(); err != nil {
		return Rating{}, err
	}
	if err := m.Validate(); err != nil {
		return Rating{}, err
	}

	cancellation := ratio(m.CancelledBySeller, m.Orders)
	lateness := ratio(m.LateShipments, m.Orders)
	cancellationScore := max(0, 100-cancellation*5/100)
	latenessScore := max(0, 100-lateness*4/100)

	average := 0
	reviewScore := neutralReviewScore
	if m.Reviews > 0 {
		average = m.ReviewScoreSum * 100 / m.Reviews
		reviewScore = (average - 100) * 100 / 400
	}

	weights := p.CancellationWeight + p.LatenessWeight + p.ReviewWeight
	score := (cancellationScore*p.CancellationWeight + latenessScore*p.LatenessWeight + reviewScore*p.ReviewWeight) / weights

	return Rating{
		score:            score,
		cancellationRate: kernel.MustBasisPoints(cancellation),
		lateShipmentRate: kernel.MustBasisPoints(lateness),
		averageReview:    average,
		orders:           m.Orders,
		provisional:      m.Orders < p.MinOrders,
		calculatedAt:     now,
	}, nil
}

func (p RatingPolicy) RequiresSuspension(r Rating) bool {
	return !r.provisional && r.score < p.SuspendBelow
}

func ratio(part, total int) int {
	if total == 0 {
		return 0
	}
	return part * kernel.MaxBasisPoints / total
}
