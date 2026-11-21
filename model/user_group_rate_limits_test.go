package model

import "testing"

// 分组速率 JSON 校验（规格 §21.3）。
//
// ⛔ limit 只允许 -1 / 0 / 正数：-2 既不是「不限」也不是「不限制」，
// 会让判定落进未定义分支。
func TestValidateGroupRateLimits(t *testing.T) {
	valid := []string{
		"",
		"{}",
		`{"vip":{"limit":20,"fallback":true}}`,
		`{"default":{"limit":5,"fallback":false},"vip":{"limit":-1}}`,
		`{"auto":{"limit":0}}`,
	}
	for _, raw := range valid {
		if err := ValidateGroupRateLimits(raw); err != nil {
			t.Fatalf("config %q must pass: %v", raw, err)
		}
	}
	invalid := []string{
		"not json",
		`[]`,
		`{"":{"limit":1}}`,
		`{"vip":{"limit":-2}}`,
		`{"vip":{"limit":1000001}}`,
	}
	for _, raw := range invalid {
		if err := ValidateGroupRateLimits(raw); err == nil {
			t.Fatalf("config %q must be rejected", raw)
		}
	}
}
