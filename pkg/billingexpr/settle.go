package billingexpr

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// quotaConversion converts raw expression output to quota based on the
// expression version. This is the central dispatch point for future versions
// that may use a different conversion formula.
//
// v1: coefficients are $/1M tokens prices — matches chat / per-token billing.
// v2: expression output is $ per call — matches per-call task billing where
//
//	the expression already integrates seconds / resolution / etc.
func quotaConversion(exprOutput float64, snap *BillingSnapshot) float64 {
	switch snap.ExprVersion {
	case 2:
		return exprOutput * snap.QuotaPerUnit
	default:
		return exprOutput / 1_000_000 * snap.QuotaPerUnit
	}
}

// ComputeTieredQuota runs the Expr from a frozen BillingSnapshot against
// actual token counts and returns the settlement result.
func ComputeTieredQuota(snap *BillingSnapshot, params TokenParams) (TieredResult, error) {
	return ComputeTieredQuotaWithRequest(snap, params, RequestInput{})
}

func ComputeTieredQuotaWithRequest(snap *BillingSnapshot, params TokenParams, request RequestInput) (TieredResult, error) {
	cost, trace, err := RunExprByHashWithRequest(snap.ExprString, snap.ExprHash, params, request)
	if err != nil {
		return TieredResult{}, err
	}
	if snap.FrozenRequestMultiplier != nil && trace.MatchedTier != "" {
		cost = trace.Cost * *snap.FrozenRequestMultiplier
	}
	if IsUnmatchedMatrixTier(snap.ExprVersion, trace.MatchedTier, trace.Cost) {
		return TieredResult{}, fmt.Errorf("pricing matrix does not match request parameters")
	}
	return ComputeTieredQuotaFromCost(snap, cost, trace), nil
}

// ComputeTieredQuotaFromCost converts an already-evaluated expression result
// to quota. It is used by pre-consume after the expression and its frozen
// request multiplier have each been evaluated once.
func ComputeTieredQuotaFromCost(snap *BillingSnapshot, cost float64, trace TraceResult) TieredResult {

	quotaBeforeGroup := quotaConversion(cost, snap)
	afterGroup, clamp := common.QuotaRoundChecked(quotaBeforeGroup * snap.GroupRatio)
	crossed := trace.MatchedTier != snap.EstimatedTier

	return TieredResult{
		ActualQuotaBeforeGroup: quotaBeforeGroup,
		ActualQuotaAfterGroup:  afterGroup,
		MatchedTier:            trace.MatchedTier,
		CrossedTier:            crossed,
		Clamp:                  clamp,
	}
}
