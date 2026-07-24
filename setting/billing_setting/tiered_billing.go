package billing_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/samber/lo"
)

const (
	BillingModeRatio      = "ratio"
	BillingModeTieredExpr = "tiered_expr"
	BillingModeField      = "billing_mode"
	BillingExprField      = "billing_expr"
)

// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr
type BillingSetting struct {
	BillingMode map[string]string `json:"billing_mode"`
	BillingExpr map[string]string `json:"billing_expr"`
}

var billingSetting = BillingSetting{
	BillingMode: make(map[string]string),
	BillingExpr: make(map[string]string),
}

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	if mode, ok := billingSetting.BillingMode[model]; ok {
		return mode
	}
	return BillingModeRatio
}

func GetBillingExpr(model string) (string, bool) {
	expr, ok := billingSetting.BillingExpr[model]
	return expr, ok
}

func GetBillingModeCopy() map[string]string {
	return lo.Assign(billingSetting.BillingMode)
}

func GetBillingExprCopy() map[string]string {
	return lo.Assign(billingSetting.BillingExpr)
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 2)
	if modes := GetBillingModeCopy(); len(modes) > 0 {
		extra[BillingModeField] = modes
	}
	if exprs := GetBillingExprCopy(); len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	return lo.Assign(base, extra)
}

// ---------------------------------------------------------------------------
// Smoke test (called externally for validation before save)
// ---------------------------------------------------------------------------

func SmokeTestExpr(exprStr string) error {
	return smokeTestExpr(exprStr)
}

func smokeTestExpr(exprStr string) error {
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	// v2 (task/video) expressions expect task-shaped variables in addition to
	// token counts; augment each vector with a representative task payload.
	if billingexpr.ExprVersion(exprStr) == 2 {
		taskVariants := []struct {
			seconds    float64
			resolution string
			hasVideo   bool
			hasImage   bool
			n          float64
			mode       string
		}{
			{0, "", false, false, 0, ""},
			{5, "480p", false, false, 1, "std"},
			{10, "1080p", true, true, 1, "pro"},
			{60, "4k", false, false, 4, ""},
		}
		expanded := make([]billingexpr.TokenParams, 0, len(vectors)*len(taskVariants))
		for _, base := range vectors {
			for _, t := range taskVariants {
				v := base
				v.Seconds = t.seconds
				v.Resolution = t.resolution
				v.HasVideo = t.hasVideo
				v.HasImage = t.hasImage
				v.N = t.n
				v.Mode = t.mode
				expanded = append(expanded, v)
			}
		}
		vectors = expanded
		requests = append(requests, billingexpr.RequestInput{
			Body: []byte(`{"model":"video-x","duration":5,"size":"1920x1080","metadata":{"resolution":"1080p","content":[{"type":"video_url"}]}}`),
		})
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g, sec=%g, res=%q}: run failed: %w", v.P, v.C, v.Seconds, v.Resolution, err)
			}
			if result < 0 {
				return fmt.Errorf("vector {p=%g, c=%g, sec=%g, res=%q}: result %f < 0", v.P, v.C, v.Seconds, v.Resolution, result)
			}
		}
	}
	return nil
}
