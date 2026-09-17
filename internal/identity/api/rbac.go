package api

import (
	"slices"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type Permission string

const (
	PermUsersBlock        Permission = "identity.users.block"
	PermUsersManageRoles  Permission = "identity.users.manage_roles"
	PermSessionsRevokeAny Permission = "identity.sessions.revoke_any"
	PermSellersModerate   Permission = "seller.applications.moderate"
	PermSellersManage     Permission = "seller.sellers.manage"
	PermCommissionsManage Permission = "seller.commissions.manage"
	PermCatalogModerate   Permission = "catalog.products.moderate"
	PermCategoriesManage  Permission = "catalog.categories.manage"
	PermReviewsModerate   Permission = "review.reviews.moderate"
	PermOrdersSupport     Permission = "ordering.orders.support"
	PermRefundsInitiate   Permission = "payment.refunds.initiate"
	PermDisputesResolve   Permission = "ordering.disputes.resolve"
	PermPromotionsManage  Permission = "pricing.promotions.manage"
)

const (
	RoleBuyer            = "buyer"
	RoleContentModerator = "content_moderator"
	RoleSupportAgent     = "support_agent"
	RolePlatformAdmin    = "platform_admin"
)

var rolePermissions = map[string][]Permission{
	RoleBuyer: {},
	RoleContentModerator: {
		PermCatalogModerate,
		PermReviewsModerate,
		PermSellersModerate,
	},
	RoleSupportAgent: {
		PermOrdersSupport,
		PermRefundsInitiate,
		PermDisputesResolve,
		PermSessionsRevokeAny,
	},
	RolePlatformAdmin: {
		PermUsersBlock,
		PermUsersManageRoles,
		PermSessionsRevokeAny,
		PermSellersModerate,
		PermSellersManage,
		PermCommissionsManage,
		PermCatalogModerate,
		PermCategoriesManage,
		PermReviewsModerate,
		PermOrdersSupport,
		PermRefundsInitiate,
		PermDisputesResolve,
		PermPromotionsManage,
	},
}

func PermissionsOf(roles []string) []Permission {
	var out []Permission
	for _, r := range roles {
		for _, p := range rolePermissions[r] {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

func Can(p auth.Principal, perm Permission) bool {
	for _, r := range p.Roles {
		if slices.Contains(rolePermissions[r], perm) {
			return true
		}
	}
	return false
}

func Authorize(p auth.Principal, perm Permission) error {
	if p.UserID == "" {
		return auth.ErrUnauthenticated
	}
	if !Can(p, perm) {
		return auth.ErrForbidden.WithDetail("missing permission %s", perm)
	}
	return nil
}
