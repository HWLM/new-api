package middleware

import (
	"testing"
)

// 速率限制兜底与拒答降级的拆分（规格 §21）。
//
// ⛔ 这三条出口必须各走各的：
//   - 分组策略 fallback=false → 直接 429；
//   - 分组策略 fallback=true  → 全局速率兜底渠道；
//   - 没配分组速率、只超总并发 → 旧行为（渠道自配兜底）。
// 谁把它们合并成一段通用逻辑，下面的表会把谁打红。

func TestDecideConcurrencyActionPicksTheRightExit(t *testing.T) {
	cfgFallback := groupRateLimitConfig{Limit: 2, Fallback: true}
	cfgReject := groupRateLimitConfig{Limit: 2, Fallback: false}

	cases := []struct {
		name        string
		totalLimit  int
		count       int
		hasGroupCfg bool
		cfg         groupRateLimitConfig
		groupLimit  bool
		groupCount  int
		wantOver    bool
		wantReject  bool
		wantRateFB  bool
		wantLegacy  bool
	}{
		{
			name:       "within every limit passes through",
			totalLimit: 10, count: 3, hasGroupCfg: true, cfg: cfgFallback, groupLimit: true, groupCount: 1,
		},
		{
			name:       "not configured and within total limit is untouched",
			totalLimit: 10, count: 3,
		},
		{
			name:       "group over limit with fallback goes to the rate-limit fallback",
			totalLimit: 10, count: 3, hasGroupCfg: true, cfg: cfgFallback, groupLimit: true, groupCount: 3,
			wantOver: true, wantRateFB: true,
		},
		{
			name:       "group over limit without fallback is rejected outright",
			totalLimit: 10, count: 3, hasGroupCfg: true, cfg: cfgReject, groupLimit: true, groupCount: 3,
			wantOver: true, wantReject: true,
		},
		{
			name:       "total over with a fallback-enabled group also uses the rate-limit fallback",
			totalLimit: 5, count: 6, hasGroupCfg: true, cfg: cfgFallback, groupLimit: true, groupCount: 1,
			wantOver: true, wantRateFB: true,
		},
		{
			name:       "total over with a fallback-disabled group is rejected",
			totalLimit: 5, count: 6, hasGroupCfg: true, cfg: cfgReject, groupLimit: true, groupCount: 1,
			wantOver: true, wantReject: true,
		},
		{
			// 规格 §21.6 #4：只超总并发、没有分组配置 → 旧行为（渠道自配兜底）。
			name:       "total over without any group config keeps the legacy path",
			totalLimit: 5, count: 6,
			wantOver: true, wantLegacy: true,
		},
		{
			// 分组配置写了但 limit<=0（不单独限制）：不参与判定，也不改变总并发的出口。
			name:       "a group entry without its own limit does not change the legacy exit",
			totalLimit: 5, count: 6, hasGroupCfg: true, cfg: groupRateLimitConfig{Limit: 0, Fallback: false},
			wantOver: true, wantReject: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideConcurrencyAction(tc.totalLimit, tc.count, tc.hasGroupCfg, tc.cfg, tc.groupLimit, tc.groupCount)
			if got.Over != tc.wantOver || got.Reject != tc.wantReject ||
				got.RateLimitFallback != tc.wantRateFB || got.LegacyOverload != tc.wantLegacy {
				t.Fatalf("decision mismatch: got %+v, want over=%v reject=%v rateFB=%v legacy=%v",
					got, tc.wantOver, tc.wantReject, tc.wantRateFB, tc.wantLegacy)
			}
			// 三个出口互斥：任何一条超限只能落进一个分支。
			exits := 0
			for _, on := range []bool{got.Reject, got.RateLimitFallback, got.LegacyOverload} {
				if on {
					exits++
				}
			}
			if tc.wantOver && exits != 1 {
				t.Fatalf("exactly one exit must be chosen, got %d: %+v", exits, got)
			}
		})
	}
}

func TestParseGroupRateLimits(t *testing.T) {
	if got := parseGroupRateLimits(""); len(got) != 0 {
		t.Fatalf("empty config must yield no groups, got %+v", got)
	}
	if got := parseGroupRateLimits("not json"); len(got) != 0 {
		t.Fatalf("bad JSON must be treated as unconfigured, got %+v", got)
	}
	got := parseGroupRateLimits(`{"vip":{"limit":20,"fallback":true},"default":{"limit":5}}`)
	if len(got) != 2 {
		t.Fatalf("want 2 groups, got %+v", got)
	}
	if vip := got["vip"]; vip.Limit != 20 || !vip.Fallback {
		t.Fatalf("vip config parsed wrong: %+v", vip)
	}
	// 缺省 fallback 的语义是「走兜底」——但 JSON 零值是 false，所以这里显式
	// 断言解析结果，提醒调用方：默认 true 必须由写入端保证（前端默认开）。
	if def := got["default"]; def.Limit != 5 || def.Fallback {
		t.Fatalf("default config parsed wrong: %+v", def)
	}
}

func TestUserGroupConcurrencyKeyIsSeparateFromTheTotalKey(t *testing.T) {
	total := userConcurrencyKey(7)
	group := userGroupConcurrencyKey(7, "vip")
	if total == group {
		t.Fatal("the group counter must not share the total counter's key")
	}
	if group != "concurrency:user:7:group:vip" {
		t.Fatalf("unexpected group key: %s", group)
	}
}
