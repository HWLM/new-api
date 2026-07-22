package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// settlementUSDRate reports whether the logged-in user settles in USD and, if so,
// the display exchange rate (消耗汇率展示, default 6.8) that money amounts should be
// divided by. When the user settles in CNY / is unconfigured / is unauthenticated,
// it returns (false, 0) and callers must leave amounts untouched.
func settlementUSDRate(c *gin.Context) (bool, float64) {
	id := c.GetInt("id")
	if id == 0 {
		return false, 0
	}
	if model.GetUserSettlementCurrency(id) != model.SettlementCurrencyUSD {
		return false, 0
	}
	return true, operation_setting.GetConsumeUSDExchangeRate()
}

// structToMoneyMap converts a struct (or struct pointer) into a JSON map preserving
// all original JSON keys, so a handler can override only the money keys with
// settlement-converted floats without changing the struct's field types. Numeric
// values decode as float64.
func structToMoneyMap(v any) (map[string]interface{}, error) {
	data, err := common.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := common.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// convertMoneyKeys divides each named money key in m by rate (rounded to 6
// decimals) when the key exists and holds a number. Non-numeric or absent keys
// are skipped.
func convertMoneyKeys(m map[string]interface{}, rate float64, keys ...string) {
	for _, k := range keys {
		if f, ok := numericValue(m[k]); ok {
			m[k] = common.ConvertAmountByRate(f, rate)
		}
	}
}

// numericValue extracts a float64 from a JSON-decoded value (float64 after
// Unmarshal; int-family kept for directly-built maps).
func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}

// perLogSettlementRate returns the consumption exchange rate snapshotted on a
// single log's `other` JSON (consume_usd_exchange_rate). It falls back to the
// provided global rate when the snapshot is absent or non-positive, so
// non-consumption rows still convert consistently.
func perLogSettlementRate(otherStr string, fallback float64) float64 {
	if otherStr == "" {
		return fallback
	}
	otherMap, err := common.StrToMap(otherStr)
	if err != nil || otherMap == nil {
		return fallback
	}
	if f, ok := numericValue(otherMap["consume_usd_exchange_rate"]); ok && f > 0 {
		return f
	}
	return fallback
}

// convertTokenForSettlement returns a JSON map of the token with its quota
// fields divided by rate. remain_quota is left untouched for unlimited tokens
// (where it is a sentinel, not a real balance). On marshal failure the original
// token is returned so the field type / values are preserved.
func convertTokenForSettlement(t *model.Token, rate float64) interface{} {
	m, err := structToMoneyMap(t)
	if err != nil {
		return t
	}
	keys := []string{"used_quota", "daily_quota", "weekly_quota", "daily_used", "weekly_used"}
	if unlimited, _ := m["unlimited_quota"].(bool); !unlimited {
		keys = append(keys, "remain_quota")
	}
	convertMoneyKeys(m, rate, keys...)
	return m
}

func convertTokensForSettlement(tokens []*model.Token, rate float64) []interface{} {
	out := make([]interface{}, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, convertTokenForSettlement(t, rate))
	}
	return out
}

// applyTokenSettlementToStorage scales a USD-settlement user's submitted quota
// inputs back to the underlying storage quota (inverse of the read-side division),
// so token amounts persist in the same CNY-based unit as CNY users. remain_quota
// is left untouched for unlimited tokens (a sentinel, not a real balance). It is a
// no-op for CNY / unconfigured users, keeping their submitted values byte-identical.
func applyTokenSettlementToStorage(c *gin.Context, token *model.Token) {
	active, rate := settlementUSDRate(c)
	if !active {
		return
	}
	if !token.UnlimitedQuota {
		token.RemainQuota = int(common.ConvertAmountToStorage(float64(token.RemainQuota), rate))
	}
	token.DailyQuota = int(common.ConvertAmountToStorage(float64(token.DailyQuota), rate))
	token.WeeklyQuota = int(common.ConvertAmountToStorage(float64(token.WeeklyQuota), rate))
}

// logOtherPriceKeys are the absolute money fields inside a log's `other` JSON that
// the usage-log "价格标准" column and detail dialog render as prices. model_ratio
// drives the per-token input/output/cache/audio/image prices (frontend uses
// baseInputUSD = model_ratio×2), so dividing it scales all those derived per-M
// prices; the rest are per-call absolute prices. Relative multipliers (group_ratio,
// completion_ratio, cache_ratio, audio ratios, …), token counts, and the
// consume_usd_exchange_rate snapshot are NOT money and must stay untouched.
var logOtherPriceKeys = []string{
	"model_ratio",
	"model_price",
	"web_search_price",
	"file_search_price",
	"image_generation_call_price",
	"audio_input_price",
}

// convertLogOtherPrices returns the log `other` JSON string with its absolute price
// fields divided by rate (rounded to 6 decimals). On any parse/marshal issue, or when
// no price field is present, it returns the original string unchanged so a malformed
// or price-less snapshot never breaks the response.
func convertLogOtherPrices(otherStr string, rate float64) string {
	if otherStr == "" {
		return otherStr
	}
	m, err := common.StrToMap(otherStr)
	if err != nil || m == nil {
		return otherStr
	}
	changed := false
	for _, k := range logOtherPriceKeys {
		if f, ok := numericValue(m[k]); ok {
			m[k] = common.ConvertAmountByRate(f, rate)
			changed = true
		}
	}
	if !changed {
		return otherStr
	}
	data, err := common.Marshal(m)
	if err != nil {
		return otherStr
	}
	return string(data)
}

// convertLogsForSettlement returns JSON maps of the logs with money fields
// converted for USD settlement. Each row's `quota` and the price fields inside its
// `other` snapshot use that row's own rate (other.consume_usd_exchange_rate),
// falling back to fallbackRate; the CNY recharge amounts (present only on
// recharge-type rows) use fallbackRate.
func convertLogsForSettlement(logs []*model.Log, fallbackRate float64) []interface{} {
	out := make([]interface{}, 0, len(logs))
	for _, lg := range logs {
		m, err := structToMoneyMap(lg)
		if err != nil {
			out = append(out, lg)
			continue
		}
		rate := perLogSettlementRate(lg.Other, fallbackRate)
		convertMoneyKeys(m, rate, "quota")
		convertMoneyKeys(m, fallbackRate, "recharge_input_amount", "recharge_after_ratio_amount")
		if otherStr, ok := m["other"].(string); ok {
			m["other"] = convertLogOtherPrices(otherStr, rate)
		}
		out = append(out, m)
	}
	return out
}

// respondUserLogs sets the log page items (converting for USD settlement when
// active) and writes the success response.
func respondUserLogs(c *gin.Context, pageInfo *common.PageInfo, logs []*model.Log) {
	if active, rate := settlementUSDRate(c); active {
		pageInfo.SetItems(convertLogsForSettlement(logs, rate))
	} else {
		pageInfo.SetItems(logs)
	}
	common.ApiSuccess(c, pageInfo)
}

// convertStructsForSettlement returns JSON maps of each item with the named
// money keys divided by rate. Used for uniform aggregate item lists (dashboard
// charts, token-stat rows) where every row uses the same global rate.
func convertStructsForSettlement[T any](items []T, rate float64, keys ...string) []interface{} {
	out := make([]interface{}, 0, len(items))
	for i := range items {
		m, err := structToMoneyMap(items[i])
		if err != nil {
			out = append(out, items[i])
			continue
		}
		convertMoneyKeys(m, rate, keys...)
		out = append(out, m)
	}
	return out
}
