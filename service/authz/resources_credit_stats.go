package authz

const ResourceCreditStats = "credit_stats"

var (
	CreditStatsRead          = Permission{Resource: ResourceCreditStats, Action: ActionRead}
	CreditStatsRepaymentEdit = Permission{Resource: ResourceCreditStats, Action: ActionRepaymentEdit}
)

const ActionRepaymentEdit = "repayment_edit"

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceCreditStats,
		LabelKey: "Credit Statistics",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View credit statistics",
				DescriptionKey: "View recharge, gift, credit, repayment and outstanding figures on the credit statistics tab.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionRepaymentEdit,
				LabelKey:       "Edit repayment records",
				DescriptionKey: "Record a new repayment or backfill historical credit/repayment entries.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}
