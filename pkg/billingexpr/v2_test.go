package billingexpr_test

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// v2 compile env exposes task/video variables
// ---------------------------------------------------------------------------

func TestV2_ResolutionBranch(t *testing.T) {
	const expr = `v2:resolution == "1080p" ? tier("hi", 0.30) : tier("lo", 0.10)`

	hi, trHi, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{Resolution: "1080p"})
	require.NoError(t, err)
	assert.InDelta(t, 0.30, hi, 1e-9)
	assert.Equal(t, "hi", trHi.MatchedTier)

	lo, trLo, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{Resolution: "720p"})
	require.NoError(t, err)
	assert.InDelta(t, 0.10, lo, 1e-9)
	assert.Equal(t, "lo", trLo.MatchedTier)
}

func TestV2_SecondsAndHasVideo(t *testing.T) {
	const expr = `v2:has_video
		? tier("i2v", 0.10 + 0.05 * seconds)
		: tier("t2v", 0.05 * seconds)`

	i2v, tr, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{Seconds: 8, HasVideo: true})
	require.NoError(t, err)
	assert.InDelta(t, 0.10+0.05*8, i2v, 1e-9)
	assert.Equal(t, "i2v", tr.MatchedTier)

	t2v, tr, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{Seconds: 4})
	require.NoError(t, err)
	assert.InDelta(t, 0.05*4, t2v, 1e-9)
	assert.Equal(t, "t2v", tr.MatchedTier)
}

// ---------------------------------------------------------------------------
// v1 cannot reference v2-only identifiers; v2 can
// ---------------------------------------------------------------------------

func TestV1_CannotReferenceV2Vars(t *testing.T) {
	_, _, err := billingexpr.RunExpr(`resolution == "1080p" ? tier("a", 1) : tier("b", 2)`, billingexpr.TokenParams{})
	require.Error(t, err, "v1 compile env should reject `resolution` identifier")
}

func TestV2_CanReferenceTokenVars(t *testing.T) {
	// v2 keeps p/c so token-billed variants (kling FinalUnitDeduction) still work.
	const expr = `v2:tier("tok", p * 0.001)`
	out, _, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{P: 1234})
	require.NoError(t, err)
	assert.InDelta(t, 1.234, out, 1e-9)
}

func TestV2_PerTokenTaskPreConsumeUsesDurationEstimate(t *testing.T) {
	const expr = `v2:tier("seedance", 7 * (p > 0 ? p : (seconds > 0 ? 50000 * seconds : 180000000)) / 1000000)`

	preConsume, _, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{Seconds: 5})
	require.NoError(t, err)
	assert.InDelta(t, 1.75, preConsume, 1e-9)

	settled, _, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{P: 40594, Seconds: 5})
	require.NoError(t, err)
	assert.InDelta(t, 7.0*40594/1_000_000, settled, 1e-9)

	fallback, _, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{})
	require.NoError(t, err)
	assert.InDelta(t, 1260.0, fallback, 1e-9)
}

// ---------------------------------------------------------------------------
// v2 quotaConversion: $ per call vs v1's $/M tokens
// ---------------------------------------------------------------------------

func TestV2_QuotaConversionIsPerCall(t *testing.T) {
	const expr = `v2:tier("flat", 0.30)`
	snap := &billingexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   expr,
		ExprHash:     billingexpr.ExprHashString(expr),
		GroupRatio:   1,
		QuotaPerUnit: common.QuotaPerUnit,
		ExprVersion:  billingexpr.ExprVersion(expr),
	}
	tr, err := billingexpr.ComputeTieredQuota(snap, billingexpr.TokenParams{})
	require.NoError(t, err)
	// 0.30 USD × QuotaPerUnit (500000) × groupRatio 1 = 150000
	assert.Equal(t, int(math.Round(0.30*common.QuotaPerUnit)), tr.ActualQuotaAfterGroup)
}

func TestV2_FrozenRequestMultiplierIsReusedAtSettlement(t *testing.T) {
	const expr = `v2:tier("base", 2 * seconds) * (has(header("x-tier"), "vip") ? 3 : 1)`
	params := billingexpr.TokenParams{Seconds: 5}
	request := billingexpr.RequestInput{
		Headers: map[string]string{"X-Tier": "vip"},
	}

	preCost, trace, err := billingexpr.RunExprWithRequest(expr, params, request)
	require.NoError(t, err)
	assert.InDelta(t, 30, preCost, 1e-9)
	assert.InDelta(t, 10, trace.Cost, 1e-9)

	frozen, _, err := billingexpr.RunExprRequestMultiplierWithRequest(expr, params, request)
	require.NoError(t, err)
	assert.InDelta(t, 3, frozen, 1e-9)

	snap := &billingexpr.BillingSnapshot{
		BillingMode:             "tiered_expr",
		ExprString:              expr,
		ExprHash:                billingexpr.ExprHashString(expr),
		GroupRatio:              1,
		QuotaPerUnit:            common.QuotaPerUnit,
		ExprVersion:             billingexpr.ExprVersion(expr),
		FrozenRequestMultiplier: &frozen,
	}

	// Settlement has no request headers, but must retain the pre-consume VIP
	// multiplier while recalculating the actual duration.
	settled, err := billingexpr.ComputeTieredQuotaWithRequest(
		snap,
		billingexpr.TokenParams{Seconds: 8},
		billingexpr.RequestInput{},
	)
	require.NoError(t, err)
	assert.Equal(t, int(16*3*common.QuotaPerUnit), settled.ActualQuotaAfterGroup)
}

func TestV1_QuotaConversionIsPerMillion(t *testing.T) {
	// v1: expression coefficients are $ per 1M tokens; RunExpr returns the raw
	// cost in $ (already scaled by tokens). Conversion multiplies by
	// QuotaPerUnit / 1e6 to bring it into quota units.
	const expr = `tier("tok", p * 3.0)`
	snap := &billingexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   expr,
		ExprHash:     billingexpr.ExprHashString(expr),
		GroupRatio:   1,
		QuotaPerUnit: common.QuotaPerUnit,
		ExprVersion:  billingexpr.ExprVersion(expr),
	}
	// p=500000 → exprOut = 1_500_000 USD-per-M-tokens units
	// quota = 1_500_000 / 1e6 * 500000 = 750_000
	tr, err := billingexpr.ComputeTieredQuota(snap, billingexpr.TokenParams{P: 500000})
	require.NoError(t, err)
	assert.Equal(t, int(math.Round(500000*3.0/1_000_000*common.QuotaPerUnit)), tr.ActualQuotaAfterGroup)
}

// ---------------------------------------------------------------------------
// v2 saturation: absurd seconds still clamps int32, records QuotaClamp
// ---------------------------------------------------------------------------

func TestV2_SaturationRecordsClamp(t *testing.T) {
	const expr = `v2:tier("burst", 1e30 * seconds)` // guaranteed to saturate
	snap := &billingexpr.BillingSnapshot{
		BillingMode:  "tiered_expr",
		ExprString:   expr,
		ExprHash:     billingexpr.ExprHashString(expr),
		GroupRatio:   1,
		QuotaPerUnit: common.QuotaPerUnit,
		ExprVersion:  billingexpr.ExprVersion(expr),
	}
	tr, err := billingexpr.ComputeTieredQuota(snap, billingexpr.TokenParams{Seconds: 1})
	require.NoError(t, err)
	require.NotNil(t, tr.Clamp, "clamp should be recorded on saturation")
	assert.Equal(t, int(math.MaxInt32), tr.ActualQuotaAfterGroup)
}

// ---------------------------------------------------------------------------
// v2 version parsing
// ---------------------------------------------------------------------------

func TestParseExprVersion_V2(t *testing.T) {
	v, body := billingexpr.ParseExprVersion(`v2:tier("x", 1)`)
	assert.Equal(t, 2, v)
	assert.Equal(t, `tier("x", 1)`, body)
}

func TestExprVersion_V2CachesCorrectly(t *testing.T) {
	const expr = `v2:tier("flat", 0.42)`
	// First compile populates the version cache.
	_, _, err := billingexpr.RunExpr(expr, billingexpr.TokenParams{})
	require.NoError(t, err)
	assert.Equal(t, 2, billingexpr.ExprVersion(expr))
}
