package domain

type Role string

const (
	RoleBuyer            Role = "buyer"
	RoleContentModerator Role = "content_moderator"
	RoleSupportAgent     Role = "support_agent"
	RolePlatformAdmin    Role = "platform_admin"
)

func ParseRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleBuyer, RoleContentModerator, RoleSupportAgent, RolePlatformAdmin:
		return r, nil
	default:
		return "", ErrInvalidRole.WithDetail("%q", s)
	}
}

func (r Role) String() string { return string(r) }
