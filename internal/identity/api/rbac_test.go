package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

func TestAuthorize(t *testing.T) {
	buyer := auth.Principal{UserID: "u-1", Roles: []string{api.RoleBuyer}}
	admin := auth.Principal{UserID: "u-2", Roles: []string{api.RoleBuyer, api.RolePlatformAdmin}}
	moderator := auth.Principal{UserID: "u-3", Roles: []string{api.RoleContentModerator}}

	require.ErrorIs(t, api.Authorize(auth.Principal{}, api.PermUsersBlock), auth.ErrUnauthenticated)
	require.ErrorIs(t, api.Authorize(buyer, api.PermUsersBlock), auth.ErrForbidden)
	require.NoError(t, api.Authorize(admin, api.PermUsersBlock))
	require.NoError(t, api.Authorize(moderator, api.PermSellersModerate))
	require.ErrorIs(t, api.Authorize(moderator, api.PermCommissionsManage), auth.ErrForbidden)
}

func TestPermissionsOf(t *testing.T) {
	assert.Empty(t, api.PermissionsOf([]string{api.RoleBuyer}))
	assert.Empty(t, api.PermissionsOf([]string{"unknown"}))

	perms := api.PermissionsOf([]string{api.RoleContentModerator, api.RolePlatformAdmin})
	assert.Contains(t, perms, api.PermUsersBlock)
	assert.Contains(t, perms, api.PermCatalogModerate)
	seen := map[api.Permission]int{}
	for _, p := range perms {
		seen[p]++
	}
	for p, n := range seen {
		assert.Equal(t, 1, n, p)
	}
}

func TestPlatformAdminHasEveryPermission(t *testing.T) {
	admin := auth.Principal{UserID: "u", Roles: []string{api.RolePlatformAdmin}}
	all := api.PermissionsOf([]string{api.RoleContentModerator, api.RoleSupportAgent})
	for _, p := range all {
		assert.True(t, api.Can(admin, p), p)
	}
}
