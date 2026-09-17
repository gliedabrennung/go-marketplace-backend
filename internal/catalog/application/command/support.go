package command

import (
	"context"
	"errors"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type Base struct {
	uow     application.UnitOfWork
	clock   application.Clock
	sellers application.SellerDirectory
	policy  application.Policy
}

func NewBase(uow application.UnitOfWork, clock application.Clock, sellers application.SellerDirectory, policy application.Policy) Base {
	return Base{uow: uow, clock: clock, sellers: sellers, policy: policy}
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

func (b Base) requireMember(ctx context.Context, p auth.Principal, sellerID string) (kernel.SellerID, error) {
	if _, err := actorID(p); err != nil {
		return kernel.SellerID{}, err
	}
	id, err := kernel.ParseSellerID(sellerID)
	if err != nil {
		return kernel.SellerID{}, sellerapi.ErrSellerNotFound
	}
	_, member, err := b.sellers.MemberRole(ctx, id.String(), p.UserID)
	if err != nil {
		return kernel.SellerID{}, err
	}
	if !member {
		return kernel.SellerID{}, sellerapi.ErrSellerNotFound
	}
	return id, nil
}

func (b Base) requireSellingMember(ctx context.Context, p auth.Principal, sellerID string) (kernel.SellerID, error) {
	id, err := b.requireMember(ctx, p, sellerID)
	if err != nil {
		return kernel.SellerID{}, err
	}
	if err := b.requireCanSell(ctx, id); err != nil {
		return kernel.SellerID{}, err
	}
	return id, nil
}

func (b Base) requireCanSell(ctx context.Context, id kernel.SellerID) error {
	info, err := b.sellers.Seller(ctx, id.String())
	if err != nil {
		return err
	}
	if !info.CanSell {
		return application.ErrSellerInactive
	}
	return nil
}

func (b Base) requireStaff(p auth.Principal, perm identity.Permission) (kernel.UserID, error) {
	if err := identity.Authorize(p, perm); err != nil {
		return kernel.UserID{}, err
	}
	return actorID(p)
}

func classification(ctx context.Context, repos application.Repositories, categoryID domain.CategoryID) (domain.Classification, error) {
	chain, err := repos.Categories().FindChain(ctx, categoryID)
	if err != nil {
		return domain.Classification{}, err
	}
	return domain.Classify(chain)
}

func parseProductID(raw string) (domain.ProductID, error) {
	id, err := domain.ParseProductID(raw)
	if err != nil {
		return domain.ProductID{}, domain.ErrProductNotFound
	}
	return id, nil
}

func parseCategoryID(raw string) (domain.CategoryID, error) {
	id, err := domain.ParseCategoryID(raw)
	if err != nil {
		return domain.CategoryID{}, domain.ErrCategoryNotFound
	}
	return id, nil
}

func (b Base) productAuthor(ctx context.Context, p auth.Principal, productID domain.ProductID) (kernel.SellerID, error) {
	var sellerID kernel.SellerID
	err := b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		product, err := repos.Products().FindByID(ctx, productID)
		if err != nil {
			return err
		}
		sellerID = product.SellerID()
		return nil
	})
	if err != nil {
		return kernel.SellerID{}, err
	}
	if _, err := b.requireMember(ctx, p, sellerID.String()); err != nil {
		if errors.Is(err, sellerapi.ErrSellerNotFound) {
			return kernel.SellerID{}, domain.ErrProductNotFound
		}
		return kernel.SellerID{}, err
	}
	return sellerID, nil
}

type productOperation func(p *domain.Product, author kernel.SellerID, class domain.Classification, now time.Time) error

func (b Base) authorAction(ctx context.Context, p auth.Principal, rawProductID string, needsClass bool, op productOperation) error {
	productID, err := parseProductID(rawProductID)
	if err != nil {
		return err
	}
	author, err := b.productAuthor(ctx, p, productID)
	if err != nil {
		return err
	}
	return b.withProduct(ctx, productID, author, needsClass, op)
}

func (b Base) withProduct(ctx context.Context, productID domain.ProductID, author kernel.SellerID, needsClass bool, op productOperation) error {
	now := b.clock.Now()
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		product, err := repos.Products().FindByID(ctx, productID)
		if err != nil {
			return err
		}
		var class domain.Classification
		if needsClass {
			if class, err = classification(ctx, repos, product.CategoryID()); err != nil {
				return err
			}
		}
		if err := op(product, author, class, now); err != nil {
			return err
		}
		return repos.Products().Save(ctx, product)
	})
}

func audit(p auth.Principal, action, objectType, objectID string, details map[string]string, at time.Time) application.AuditEntry {
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

type categoryOperation func(ctx context.Context, repos application.Repositories, c *domain.Category) error

func (b Base) categoryAction(ctx context.Context, p auth.Principal, rawID, action string, details map[string]string, op categoryOperation) error {
	if _, err := b.requireStaff(p, identity.PermCategoriesManage); err != nil {
		return err
	}
	id, err := parseCategoryID(rawID)
	if err != nil {
		return err
	}
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		category, err := repos.Categories().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if err := op(ctx, repos, category); err != nil {
			return err
		}
		if err := repos.Categories().Save(ctx, category); err != nil {
			return err
		}
		return repos.Audit().Record(ctx, audit(p, action, "category", id.String(), details, b.clock.Now()))
	})
}

type moderationOperation func(p *domain.Product, moderator kernel.UserID, class domain.Classification, now time.Time) error

func (b Base) moderationAction(ctx context.Context, p auth.Principal, rawProductID, action string, details map[string]string, op moderationOperation) error {
	moderator, err := b.requireStaff(p, identity.PermCatalogModerate)
	if err != nil {
		return err
	}
	productID, err := parseProductID(rawProductID)
	if err != nil {
		return err
	}
	now := b.clock.Now()
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		product, err := repos.Products().FindByID(ctx, productID)
		if err != nil {
			return err
		}
		class, err := classification(ctx, repos, product.CategoryID())
		if err != nil {
			return err
		}
		if err := op(product, moderator, class, now); err != nil {
			return err
		}
		if err := repos.Products().Save(ctx, product); err != nil {
			return err
		}
		return repos.Audit().Record(ctx, audit(p, action, "product", productID.String(), details, now))
	})
}
