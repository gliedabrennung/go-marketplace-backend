package command

import (
	"context"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type sellerOperation func(s *domain.Seller, actor kernel.UserID, now time.Time) error

type Unit struct {
	uow   application.UnitOfWork
	clock application.Clock
}

func NewUnit(uow application.UnitOfWork, clock application.Clock) Unit {
	return Unit{uow: uow, clock: clock}
}

func actorID(p auth.Principal) (kernel.UserID, error) {
	if p.UserID == "" {
		return kernel.UserID{}, auth.ErrUnauthenticated
	}
	id, err := kernel.ParseUserID(p.UserID)
	if err != nil {
		return kernel.UserID{}, auth.ErrInvalidToken
	}
	return id, nil
}

func parseSellerID(raw string) (kernel.SellerID, error) {
	id, err := kernel.ParseSellerID(raw)
	if err != nil {
		return kernel.SellerID{}, domain.ErrSellerNotFound
	}
	return id, nil
}

func (u Unit) memberAction(ctx context.Context, p auth.Principal, sellerID string, op sellerOperation) error {
	actor, err := actorID(p)
	if err != nil {
		return err
	}
	id, err := parseSellerID(sellerID)
	if err != nil {
		return err
	}
	now := u.clock.Now()
	return u.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		s, err := repos.Sellers().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if !s.IsMember(actor) {
			return domain.ErrSellerNotFound
		}
		if err := op(s, actor, now); err != nil {
			return err
		}
		return repos.Sellers().Save(ctx, s)
	})
}

type platformAction struct {
	permission identity.Permission
	action     string
	details    map[string]string
}

func (u Unit) platformAction(ctx context.Context, p auth.Principal, sellerID string, pa platformAction, op sellerOperation) error {
	if err := identity.Authorize(p, pa.permission); err != nil {
		return err
	}
	actor, err := actorID(p)
	if err != nil {
		return err
	}
	id, err := parseSellerID(sellerID)
	if err != nil {
		return err
	}
	now := u.clock.Now()
	return u.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		s, err := repos.Sellers().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if err := op(s, actor, now); err != nil {
			return err
		}
		if err := repos.Sellers().Save(ctx, s); err != nil {
			return err
		}
		return repos.Audit().Record(ctx, auditEntry(p, pa.action, "seller", id.String(), pa.details, now))
	})
}

func auditEntry(p auth.Principal, action, objectType, objectID string, details map[string]string, at time.Time) application.AuditEntry {
	return application.AuditEntry{
		ActorID:    p.UserID,
		ActorRoles: p.Roles,
		Action:     action,
		ObjectType: objectType,
		ObjectID:   objectID,
		Details:    details,
		OccurredAt: at,
	}
}
