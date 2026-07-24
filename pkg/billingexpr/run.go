package billingexpr

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/tidwall/gjson"
)

// RunExpr compiles (with cache) and executes an expression string.
// The environment exposes:
//   - p, c             — prompt / completion tokens (auto-excluding separately-priced sub-categories)
//   - len              — total input context length for tier conditions (never reduced by sub-category exclusion)
//   - cr, cc, cc1h     — cache read / creation / creation-1h tokens
//   - tier(name, value) — trace callback that records which tier matched
//   - max, min, abs, ceil, floor — standard math helpers
//
// Returns the resulting float64 quota (before group ratio) and a TraceResult
// with side-channel info captured by tier() during execution.
func RunExpr(exprStr string, params TokenParams) (float64, TraceResult, error) {
	return RunExprWithRequest(exprStr, params, RequestInput{})
}

func RunExprWithRequest(exprStr string, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	prog, err := CompileFromCache(exprStr)
	if err != nil {
		return 0, TraceResult{}, err
	}
	return runProgram(prog, params, request)
}

// RunExprRequestMultiplierWithRequest evaluates an expression while making
// every selected tier return 1. This extracts an outer request-condition
// multiplier without depending on the tier's current cost.
func RunExprRequestMultiplierWithRequest(exprStr string, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	prog, err := CompileFromCache(exprStr)
	if err != nil {
		return 0, TraceResult{}, err
	}
	return runProgramWithTierUnit(prog, params, request)
}

// RunExprByHash is like RunExpr but accepts a pre-computed hash for the cache
// lookup, avoiding a redundant SHA-256 computation when the caller already
// holds BillingSnapshot.ExprHash.
func RunExprByHash(exprStr, hash string, params TokenParams) (float64, TraceResult, error) {
	return RunExprByHashWithRequest(exprStr, hash, params, RequestInput{})
}

func RunExprByHashWithRequest(exprStr, hash string, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	prog, err := CompileFromCacheByHash(exprStr, hash)
	if err != nil {
		return 0, TraceResult{}, err
	}
	return runProgram(prog, params, request)
}

func runProgram(prog *vm.Program, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	return runProgramWithTierUnitValue(prog, params, request, false)
}

func runProgramWithTierUnit(prog *vm.Program, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	return runProgramWithTierUnitValue(prog, params, request, true)
}

func runProgramWithTierUnitValue(prog *vm.Program, params TokenParams, request RequestInput, returnTierUnit bool) (float64, TraceResult, error) {
	trace := TraceResult{}
	headers := normalizeHeaders(request.Headers)

	env := map[string]interface{}{
		"p":     params.P,
		"c":     params.C,
		"len":   params.Len,
		"cr":    params.CR,
		"cc":    params.CC,
		"cc1h":  params.CC1h,
		"img":   params.Img,
		"img_o": params.ImgO,
		"ai":    params.AI,
		"ao":    params.AO,
		// v2 task/video variables — extra keys are inert for v1 programs.
		"seconds":    params.Seconds,
		"resolution": params.Resolution,
		"size":       params.Size,
		"has_video":  params.HasVideo,
		"has_image":  params.HasImage,
		"n":          params.N,
		"mode":       params.Mode,
		"tier": func(name string, value float64) float64 {
			trace.MatchedTier = name
			trace.Cost = value
			if returnTierUnit {
				return 1
			}
			return value
		},
		"header": func(key string) string {
			return headers[strings.ToLower(strings.TrimSpace(key))]
		},
		"param": func(path string) interface{} {
			path = strings.TrimSpace(path)
			if path == "" || len(request.Body) == 0 {
				return nil
			}
			result := gjson.GetBytes(request.Body, path)
			if !result.Exists() {
				return nil
			}
			return result.Value()
		},
		"has": func(source interface{}, substr string) bool {
			if source == nil || substr == "" {
				return false
			}
			return strings.Contains(fmt.Sprint(source), substr)
		},
		"hour":    func(tz string) int { return timeInZone(tz).Hour() },
		"minute":  func(tz string) int { return timeInZone(tz).Minute() },
		"weekday": func(tz string) int { return int(timeInZone(tz).Weekday()) },
		"month":   func(tz string) int { return int(timeInZone(tz).Month()) },
		"day":     func(tz string) int { return timeInZone(tz).Day() },
		"max":     math.Max,
		"min":     math.Min,
		"abs":     math.Abs,
		"ceil":    math.Ceil,
		"floor":   math.Floor,
	}

	out, err := expr.Run(prog, env)
	if err != nil {
		return 0, trace, fmt.Errorf("expr run error: %w", err)
	}
	f, ok := out.(float64)
	if !ok {
		return 0, trace, fmt.Errorf("expr result is %T, want float64", out)
	}
	return f, trace, nil
}

func timeInZone(tz string) time.Time {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.Now().UTC()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Now().UTC()
	}
	return time.Now().In(loc)
}

func normalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		k := strings.ToLower(strings.TrimSpace(key))
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		normalized[k] = v
	}
	return normalized
}
