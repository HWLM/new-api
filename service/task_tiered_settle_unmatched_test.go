package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettleTaskTieredExprMatrixFallbackRetainsPreConsume(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{name: "matrix unmatched marker", expr: `v2:resolution == "1080p" ? tier("paid", 1) : tier("__matrix_unmatched__", 0)`},
		{name: "legacy zero cost fallback", expr: `v2:resolution == "1080p" ? tier("paid", 1) : tier("fallback", 0)`},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			ctx := context.Background()

			userID := 80 + i
			tokenID := 80 + i
			channelID := 80 + i
			const userQuota = 7000
			const tokenRemain = 5000
			const preConsumed = 3000

			seedUser(t, userID, userQuota)
			seedToken(t, tokenID, userID, "sk-matrix-unmatched", tokenRemain)
			seedChannel(t, channelID)

			task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
			task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
				BillingMode:   "tiered_expr",
				ExprString:    tc.expr,
				ExprHash:      billingexpr.ExprHashString(tc.expr),
				GroupRatio:    1,
				QuotaPerUnit:  common.QuotaPerUnit,
				ExprVersion:   billingexpr.ExprVersion(tc.expr),
				EstimatedTier: "paid",
			}
			task.PrivateData.BillingContext.TieredExprVars = &model.TaskExprVars{Resolution: "720p"}

			settleTaskTieredExpr(ctx, task, &relaycommon.TaskInfo{Resolution: "720p"})

			assert.Equal(t, userQuota, getUserQuota(t, userID))
			assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
			assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
			assert.Equal(t, preConsumed, task.Quota)
			assert.Equal(t, int64(0), countLogs(t))
		})
	}
}

func TestSettleTaskTieredExprExplicitFreeTierStillRefunds(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 82
	const tokenID = 82
	const channelID = 82
	const userQuota = 7000
	const tokenRemain = 5000
	const preConsumed = 3000
	const expr = `v2:tier("free", 0)`

	seedUser(t, userID, userQuota)
	seedToken(t, tokenID, userID, "sk-explicit-free", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   expr,
		ExprHash:     billingexpr.ExprHashString(expr),
		GroupRatio:   1,
		QuotaPerUnit: common.QuotaPerUnit,
		ExprVersion:  billingexpr.ExprVersion(expr),
	}
	require.NoError(t, task.Insert())

	settleTaskTieredExpr(ctx, task, &relaycommon.TaskInfo{})

	assert.Equal(t, userQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, -preConsumed, getTokenUsedQuota(t, tokenID))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
}
