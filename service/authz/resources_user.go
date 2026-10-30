package authz

const ResourceUser = "user"

var (
	UserRead  = Permission{Resource: ResourceUser, Action: ActionRead}
	UserWrite = Permission{Resource: ResourceUser, Action: ActionWrite}
	UserAdd   = Permission{Resource: ResourceUser, Action: ActionAdd}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceUser,
		LabelKey: "User Management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read users",
				DescriptionKey: "View the user list and user detail pages.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Edit users",
				DescriptionKey: "Edit existing user profile, quota, group, and other admin-controlled fields.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionAdd,
				LabelKey:       "Add users",
				DescriptionKey: "Create new user accounts from the admin console.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}
