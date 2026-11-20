package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
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
		name       string
		totalLimit int
		count      int
		cfg        groupRateLimitConfig
		groupLimit bool
		groupCount int
		wantOver   bool
		wantReject bool
		wantRateFB bool
		wantLegacy bool
	}{
		{
			name:       "within every limit passes through",
			totalLimit: 10, count: 3, cfg: cfgFallback, groupLimit: true, groupCount: 1,
		},
		{
			name:       "not configured and within total limit is untouched",
			totalLimit: 10, count: 3,
		},
		{
			name:       "group over limit with fallback goes to the rate-limit fallback",
			totalLimit: 10, count: 3, cfg: cfgFallback, groupLimit: true, groupCount: 3,
			wantOver: true, wantRateFB: true,
		},
		{
			name:       "group over limit without fallback is rejected outright",
			totalLimit: 10, count: 3, cfg: cfgReject, groupLimit: true, groupCount: 3,
			wantOver: true, wantReject: true,
		},
		{
			name:       "total over with a fallback-enabled group also uses the rate-limit fallback",
			totalLimit: 5, count: 6, cfg: cfgFallback, groupLimit: true, groupCount: 1,
			wantOver: true, wantRateFB: true,
		},
		{
			name:       "total over with a fallback-disabled group is rejected",
			totalLimit: 5, count: 6, cfg: cfgReject, groupLimit: true, groupCount: 1,
			wantOver: true, wantReject: true,
		},
		{
			// 规格 §21.6 #4：只超总并发、没有分组配置 → 旧行为（渠道自配兜底）。
			name:       "total over without any group config keeps the legacy path",
			totalLimit: 5, count: 6,
			wantOver: true, wantLegacy: true,
		},
		{
			// 分组条目写了但 limit<=0 = 没配分组速率：只超总并发必须走旧行为。
			// ⛔ 这里曾经写成 wantReject，把 Reviewer 抓到的 bug 固化进了测试。
			name:       "a group entry without its own limit keeps the legacy exit",
			totalLimit: 5, count: 6, cfg: groupRateLimitConfig{Limit: 0, Fallback: false},
			wantOver: true, wantLegacy: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideConcurrencyAction(tc.totalLimit, tc.count, tc.groupLimit, tc.cfg, tc.groupCount)
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
	// ⛔ 缺省 fallback 的语义是「走兜底」：库里手写没写 fallback 也不能被
	// 当成关掉（Opus 实测指出的坑）。
	if def := got["default"]; def.Limit != 5 || !def.Fallback {
		t.Fatalf("default config parsed wrong (missing fallback must default to true): %+v", def)
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

// 降级态 + 并发超限 → 直接 429，不再跳第二次兜底。
// ⛔ 这里曾经漏判：蒸馏降级一置 distillRedirected，Distribute 里「渠道兜底」
// 与「速率兜底」两段全被跳过，降级用户等于不受并发约束，超限请求照样 200
// 打到蒸馏兜底渠道上（用户实测 limit=1 并发 5，全 200）。
func TestDistillDemotedOverLimitRejectsInsteadOfHopping(t *testing.T) {
	cases := []struct {
		name                  string
		distillRedirected     bool
		concurrencyOverloaded bool
		wantReject            bool
	}{
		{"demoted and over the limit rejects instead of a second hop", true, true, true},
		{"demoted but within the limit flows to the distill fallback", true, false, false},
		{"over the limit without demotion keeps the normal exits", false, true, false},
		{"neither demoted nor over the limit", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := distillDemotedOverLimit(tc.distillRedirected, tc.concurrencyOverloaded); got != tc.wantReject {
				t.Fatalf("distillDemotedOverLimit(%v, %v) = %v, want %v",
					tc.distillRedirected, tc.concurrencyOverloaded, got, tc.wantReject)
			}
		})
	}
}

// 这条 429 必须带 rate_limit_exceeded，且文案只讲并发 ——
// 不能和蒸馏兜底不可用那句「无可用兜底渠道」混用（两种 429 运营要能一眼分开）。
func TestRejectDistillDemotedConcurrencyCarriesTheRateLimitCode(t *testing.T) {
	c, rec := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{}`)
	// 模拟蒸馏兜底已经写好「实际落到哪个渠道」——被 429 之后这行命中记录
	// 不能留着这个 id，否则对账看起来像请求已经切过去了。
	common.SetContextKey(c, constant.ContextKeyDistillFallbackChannelId, 5)
	rejectDistillDemotedConcurrency(c)

	if got := common.GetContextKeyInt(c, constant.ContextKeyDistillFallbackChannelId); got != 0 {
		t.Fatalf("a rejected request must not claim a distill redirect, got fallback channel %d", got)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", rec.Code)
	}
	if !c.IsAborted() {
		t.Fatal("the rejection must abort the chain, otherwise the relay still runs")
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad JSON body: %v (%s)", err, rec.Body.String())
	}
	if body.Error.Code != string(types.ErrorCodeRateLimitExceeded) {
		t.Fatalf("want code %q, got %q", types.ErrorCodeRateLimitExceeded, body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, "并发") {
		t.Fatalf("the message must talk about concurrency, got %q", body.Error.Message)
	}
	if strings.Contains(body.Error.Message, "无可用兜底渠道") {
		t.Fatalf("rate-limit rejection must not reuse the distill fallback wording, got %q", body.Error.Message)
	}
}
