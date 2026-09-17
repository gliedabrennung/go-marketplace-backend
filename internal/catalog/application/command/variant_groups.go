package command

import (
	"context"
	"errors"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type variantOperation func(ctx context.Context, repos application.Repositories, g *domain.VariantGroup, owner kernel.SellerID) error

func (b Base) variantAction(ctx context.Context, p auth.Principal, rawGroupID string, op variantOperation) error {
	groupID, err := domain.ParseVariantGroupID(rawGroupID)
	if err != nil {
		return domain.ErrVariantGroupNotFound
	}
	var sellerID kernel.SellerID
	err = b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		group, err := repos.VariantGroups().FindByID(ctx, groupID)
		if err != nil {
			return err
		}
		sellerID = group.SellerID()
		return nil
	})
	if err != nil {
		return err
	}
	owner, err := b.requireMember(ctx, p, sellerID.String())
	if errors.Is(err, sellerapi.ErrSellerNotFound) {
		return domain.ErrVariantGroupNotFound
	}
	if err != nil {
		return err
	}
	return b.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		group, err := repos.VariantGroups().FindByID(ctx, groupID)
		if err != nil {
			return err
		}
		if err := op(ctx, repos, group, owner); err != nil {
			return err
		}
		return repos.VariantGroups().Save(ctx, group)
	})
}

type CreateVariantGroup struct {
	Actor      auth.Principal
	SellerID   string
	CategoryID string
	Axes       []string
}

type CreateVariantGroupResult struct {
	GroupID string
}

type CreateVariantGroupHandler struct {
	base Base
}

func NewCreateVariantGroupHandler(base Base) *CreateVariantGroupHandler {
	return &CreateVariantGroupHandler{base: base}
}

func (h *CreateVariantGroupHandler) Handle(ctx context.Context, cmd CreateVariantGroup) (CreateVariantGroupResult, error) {
	owner, err := h.base.requireMember(ctx, cmd.Actor, cmd.SellerID)
	if err != nil {
		return CreateVariantGroupResult{}, err
	}
	categoryID, err := parseCategoryID(cmd.CategoryID)
	if err != nil {
		return CreateVariantGroupResult{}, err
	}
	var created *domain.VariantGroup
	err = h.base.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		class, err := classification(ctx, repos, categoryID)
		if err != nil {
			return err
		}
		group, err := domain.CreateVariantGroup(domain.NewVariantGroupID(), owner, class, cmd.Axes, h.base.clock.Now())
		if err != nil {
			return err
		}
		created = group
		return repos.VariantGroups().Save(ctx, group)
	})
	if err != nil {
		return CreateVariantGroupResult{}, err
	}
	return CreateVariantGroupResult{GroupID: created.ID().String()}, nil
}

type AddVariantMember struct {
	Actor     auth.Principal
	GroupID   string
	ProductID string
}

type AddVariantMemberHandler struct {
	base Base
}

func NewAddVariantMemberHandler(base Base) *AddVariantMemberHandler {
	return &AddVariantMemberHandler{base: base}
}

func (h *AddVariantMemberHandler) Handle(ctx context.Context, cmd AddVariantMember) (struct{}, error) {
	productID, err := parseProductID(cmd.ProductID)
	if err != nil {
		return struct{}{}, err
	}
	return struct{}{}, h.base.variantAction(ctx, cmd.Actor, cmd.GroupID,
		func(ctx context.Context, repos application.Repositories, g *domain.VariantGroup, owner kernel.SellerID) error {
			product, err := repos.Products().FindByID(ctx, productID)
			if err != nil {
				return err
			}
			return g.AddProduct(owner, product, h.base.clock.Now())
		})
}

type RemoveVariantMember struct {
	Actor     auth.Principal
	GroupID   string
	ProductID string
}

type RemoveVariantMemberHandler struct {
	base Base
}

func NewRemoveVariantMemberHandler(base Base) *RemoveVariantMemberHandler {
	return &RemoveVariantMemberHandler{base: base}
}

func (h *RemoveVariantMemberHandler) Handle(ctx context.Context, cmd RemoveVariantMember) (struct{}, error) {
	productID, err := parseProductID(cmd.ProductID)
	if err != nil {
		return struct{}{}, domain.ErrVariantMemberNotFound
	}
	return struct{}{}, h.base.variantAction(ctx, cmd.Actor, cmd.GroupID,
		func(_ context.Context, _ application.Repositories, g *domain.VariantGroup, owner kernel.SellerID) error {
			return g.RemoveProduct(owner, productID, h.base.clock.Now())
		})
}
