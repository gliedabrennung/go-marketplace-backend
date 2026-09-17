package domain

import (
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

func (s *Seller) AddMember(actor, userID kernel.UserID, role MemberRole, now time.Time) error {
	if err := s.requireAdmin(actor); err != nil {
		return err
	}
	if err := s.requireNotTerminated(); err != nil {
		return err
	}
	if userID.IsZero() {
		return kernel.ErrInvalidID
	}
	if _, err := ParseMemberRole(string(role)); err != nil {
		return err
	}
	if s.IsMember(userID) {
		return ErrMemberExists
	}
	if len(s.members) >= maxMembers {
		return ErrTooManyMembers
	}
	s.members = append(s.members, Member{userID: userID, role: role, addedAt: now})
	s.touch(now)
	s.events.Record(SellerMemberAdded{SellerID: s.id, UserID: userID, Role: role, AddedBy: actor, At: now})
	return nil
}

func (s *Seller) RemoveMember(actor, userID kernel.UserID, now time.Time) error {
	if err := s.requireAdmin(actor); err != nil {
		return err
	}
	if userID == s.ownerID {
		return ErrCannotRemoveOwner
	}
	idx := slices.IndexFunc(s.members, func(m Member) bool { return m.userID == userID })
	if idx < 0 {
		return ErrMemberNotFound
	}
	s.members = slices.Delete(s.members, idx, idx+1)
	s.touch(now)
	s.events.Record(SellerMemberRemoved{SellerID: s.id, UserID: userID, RemovedBy: actor, At: now})
	return nil
}

func (s *Seller) SetCommissionOverride(category CategoryID, rate kernel.BasisPoints, by kernel.UserID, now time.Time) error {
	if err := s.requireNotTerminated(); err != nil {
		return err
	}
	if category.IsZero() {
		return kernel.ErrInvalidID
	}
	if current, ok := s.commissionOverrides[category]; ok && current == rate {
		return nil
	}
	s.commissionOverrides[category] = rate
	s.touch(now)
	s.events.Record(SellerCommissionOverrideSet{SellerID: s.id, CategoryID: category, Rate: rate, SetBy: by, At: now})
	return nil
}

func (s *Seller) ClearCommissionOverride(category CategoryID, by kernel.UserID, now time.Time) {
	if _, ok := s.commissionOverrides[category]; !ok {
		return
	}
	delete(s.commissionOverrides, category)
	s.touch(now)
	s.events.Record(SellerCommissionOverrideCleared{SellerID: s.id, CategoryID: category, ClearedBy: by, At: now})
}

func (s *Seller) ApplyPerformance(metrics PerformanceMetrics, policy RatingPolicy, now time.Time) (Rating, error) {
	rating, err := policy.Calculate(metrics, now)
	if err != nil {
		return Rating{}, err
	}
	s.rating = rating
	s.touch(now)
	s.events.Record(SellerRatingChanged{
		SellerID: s.id, Score: rating.score, Provisional: rating.provisional, Orders: rating.orders, At: now,
	})
	if s.status == StatusActive && policy.RequiresSuspension(rating) {
		if err := s.Suspend(kernel.UserID{}, SuspendedByLowRating, "", now); err != nil {
			return Rating{}, err
		}
	}
	return rating, nil
}

func (s *Seller) ID() kernel.SellerID { return s.id }

func (s *Seller) OwnerID() kernel.UserID { return s.ownerID }

func (s *Seller) Status() SellerStatus { return s.status }

func (s *Seller) Legal() LegalDetails { return s.legal }

func (s *Seller) BankAccount() BankAccount { return s.bank }

func (s *Seller) BankVerified() bool { return s.bankVerified }

func (s *Seller) Documents() []Document { return slices.Clone(s.documents) }

func (s *Seller) Members() []Member { return slices.Clone(s.members) }

func (s *Seller) Rating() Rating { return s.rating }

func (s *Seller) RejectionReason() string { return s.rejectionReason }

func (s *Seller) SuspensionReason() SuspensionReason { return s.suspensionReason }

func (s *Seller) CanSell() bool { return s.status == StatusActive }

func (s *Seller) PayoutsAllowed() bool {
	return s.bankVerified && (s.status == StatusActive || s.status == StatusSuspended)
}

func (s *Seller) IsMember(userID kernel.UserID) bool {
	_, ok := s.MemberRole(userID)
	return ok
}

func (s *Seller) MemberRole(userID kernel.UserID) (MemberRole, bool) {
	for _, m := range s.members {
		if m.userID == userID {
			return m.role, true
		}
	}
	return "", false
}

func (s *Seller) CommissionOverride(category CategoryID) (kernel.BasisPoints, bool) {
	rate, ok := s.commissionOverrides[category]
	return rate, ok
}

func (s *Seller) Version() int { return s.version }

func (s *Seller) AdvanceVersion() { s.version++ }

func (s *Seller) PullEvents() []kernel.DomainEvent { return s.events.Pull() }
