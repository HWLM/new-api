package authz

const ResourceUpstream = "upstream"

var (
	UpstreamRead           = Permission{Resource: ResourceUpstream, Action: ActionRead}
	UpstreamOperate        = Permission{Resource: ResourceUpstream, Action: ActionOperate}
	UpstreamWrite          = Permission{Resource: ResourceUpstream, Action: ActionWrite}
	UpstreamSensitiveWrite = Permission{Resource: ResourceUpstream, Action: ActionSensitiveWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceUpstream,
		LabelKey: "Upstream Management",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "Read upstreams", DescriptionKey: "View upstream candidates and benchmark results.", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: ActionOperate, LabelKey: "Operate upstreams", DescriptionKey: "Run benchmarks and synchronize upstreams.", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: ActionWrite, LabelKey: "Edit upstreams", DescriptionKey: "Edit non-sensitive upstream fields.", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: ActionSensitiveWrite, LabelKey: "Edit sensitive upstream settings", DescriptionKey: "Create or edit upstream URLs and API keys."},
		},
	})
}
