package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 蒸馏兜底改计费分组后的「三处同源」（规格 §15）。
//
// 预扣走 HandleGroupRatio 把结果存进 PriceData.GroupRatioInfo，结算与日志都读
// 这一份；唯一会在后面重新解析分组的是 WSS 预扣，现在也走
// RelayInfo.ResolveUsingGroup。只要这三处都经由同一个解析函数，就不会出现
// 「预扣按兜底价、结算还按原价」。

// withGroupRatios 临时替换分组倍率表，测试结束还原。
func withGroupRatios(t *testing.T, jsonStr string) {
	t.Helper()
	original := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(original))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(jsonStr))
}

func newBillingTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return ctx
}

// ⭐ 预扣算出来的倍率，必须等于「结算时用同一个解析函数得到的倍率」。
// Distribute 在蒸馏兜底时把 auto_group 写成兜底渠道的分组，这里模拟那条链路。
func TestHandleGroupRatioMatchesTheGroupSettlementWouldResolve(t *testing.T) {
	withGroupRatios(t, `{"default":1,"backup":3}`)

	ctx := newBillingTestContext(t)
	info := &relaycommon.RelayInfo{UsingGroup: "default", UserGroup: "default"}

	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "backup")

	// 预扣侧。
	preConsumed := HandleGroupRatio(ctx, info)
	if preConsumed.GroupRatio != 3 {
		t.Fatalf("pre-consume must use the fallback group ratio, got %v", preConsumed.GroupRatio)
	}
	if info.UsingGroup != "backup" {
		t.Fatalf("the relay info must be updated to the billing group, got %q", info.UsingGroup)
	}

	// 结算侧（service.PreWssConsumeQuota 与这里用的是同一个函数）。
	settled := ratio_setting.GetGroupRatio(info.ResolveUsingGroup(ctx))
	if settled != preConsumed.GroupRatio {
		t.Fatalf("settlement must bill the same ratio as pre-consume: pre=%v settled=%v",
			preConsumed.GroupRatio, settled)
	}
}

// 分组没变（观察模式 / 兜底渠道里就有用户原分组）时，倍率也不该变。
func TestHandleGroupRatioKeepsTheRatioWhenTheGroupIsUnchanged(t *testing.T) {
	withGroupRatios(t, `{"default":1,"backup":3}`)

	ctx := newBillingTestContext(t)
	info := &relaycommon.RelayInfo{UsingGroup: "default", UserGroup: "default"}
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "default")

	got := HandleGroupRatio(ctx, info)
	if got.GroupRatio != 1 {
		t.Fatalf("an unchanged group must keep its ratio, got %v", got.GroupRatio)
	}
}

// 用户分组特例倍率（group_group_ratio）要跟着**新分组**重新取，
// 不能沿用换分组前那张表的结果。
func TestHandleGroupRatioRecomputesTheSpecialRatioForTheNewGroup(t *testing.T) {
	withGroupRatios(t, `{"default":1,"backup":3}`)

	originalSpecial := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalSpecial))
	})
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"default":{"backup":0.5}}`))

	ctx := newBillingTestContext(t)
	info := &relaycommon.RelayInfo{UsingGroup: "default", UserGroup: "default"}
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "backup")

	got := HandleGroupRatio(ctx, info)
	if !got.HasSpecialRatio || got.GroupRatio != 0.5 {
		t.Fatalf("the special ratio must be resolved for the new group, got %+v", got)
	}
}

// ── 蒸馏 vs 拒答：两条口径必须并排存在（规格 §15.2）──────────────────────
//
// ⛔ 用户口径：**拒答降级命中后仍按原渠道原分组计费，一个字不动**；只有蒸馏
// 重导改分组。谁把这两条合并成「兜底就换分组」的通用逻辑，下面两个测试会立刻红。

func distillBillingTestInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UsingGroup:  "default",
		UserGroup:   "default",
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
}

// ① 蒸馏重导后：使用日志的「分组」列 = 兜底分组，group_ratio = 兜底分组倍率，
// 日志里两个分组字段齐全。
//
// ⛔ 日志列的来源是 relayInfo.UsingGroup（text_quota.go / quota.go 写
// RecordConsumeLog 时取的就是它），所以断言它等于兜底分组，就是断言日志列。
func TestDistillRedirectBillsAtTheFallbackGroupRatio(t *testing.T) {
	withGroupRatios(t, `{"default":1,"backup":3}`)

	ctx := newBillingTestContext(t)
	info := distillBillingTestInfo()
	// Distribute 在蒸馏兜底时写入的键。
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "backup")
	common.SetContextKey(ctx, constant.ContextKeyDistillFallbackChannelId, 42)
	common.SetContextKey(ctx, constant.ContextKeyDistillOriginGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyDistillFallbackGroup, "backup")

	priceData := HandleGroupRatio(ctx, info)
	other := service.GenerateTextOtherInfo(ctx, info, 0.5, priceData.GroupRatio, 1, 0, 1, 0, -1)

	// 使用日志的「分组」列。
	if info.UsingGroup != "backup" {
		t.Fatalf("the consume log's group column must show the fallback group, got %q", info.UsingGroup)
	}
	if got := other["group_ratio"]; got != float64(3) {
		t.Fatalf("a distill redirect must be billed at the fallback group ratio, got %v", got)
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		t.Fatalf("admin_info must be present")
	}
	if adminInfo["distill_origin_group"] != "default" {
		t.Fatalf("distill_origin_group must be the pre-redirect group, got %v", adminInfo["distill_origin_group"])
	}
	if adminInfo["distill_fallback_group"] != "backup" {
		t.Fatalf("distill_fallback_group must be the billed group, got %v", adminInfo["distill_fallback_group"])
	}
}

// ② 拒答降级命中后：使用日志的「分组」列仍是原分组、group_ratio 仍是原分组
// 倍率，且 other 里没有那两个新字段。
//
// ⛔ 这是**产品口径**，不是漏改：拒答降级沿用用户原分组，避免用户看到的价格
// 因风控降级而漂移。把分组替换做成通用行为会破坏这条，测试必须挡住。
func TestRefusalDemoteKeepsTheOriginalGroupRatio(t *testing.T) {
	withGroupRatios(t, `{"default":1,"backup":3}`)

	ctx := newBillingTestContext(t)
	info := distillBillingTestInfo()
	// 拒答降级：Distribute 保留原分组（pickFallbackChannelGroup），
	// 所以 auto_group 仍然是 default。
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyRefusalDemoted, true)
	common.SetContextKey(ctx, constant.ContextKeyFallbackFromChannelId, 11)
	common.SetContextKey(ctx, constant.ContextKeyFallbackFromGroup, "default")

	priceData := HandleGroupRatio(ctx, info)
	other := service.GenerateTextOtherInfo(ctx, info, 0.5, priceData.GroupRatio, 1, 0, 1, 0, -1)

	// 使用日志的「分组」列必须还是原分组。
	if info.UsingGroup != "default" {
		t.Fatalf("a refusal demotion must keep the log group column at the original group, got %q", info.UsingGroup)
	}
	if got := other["group_ratio"]; got != float64(1) {
		t.Fatalf("a refusal demotion must keep billing at the original group ratio, got %v", got)
	}
	if adminInfo, ok := other["admin_info"].(map[string]interface{}); ok {
		if _, exists := adminInfo["distill_fallback_group"]; exists {
			t.Fatal("a refusal demotion must not carry the distill billing group fields")
		}
		if _, exists := adminInfo["distill_origin_group"]; exists {
			t.Fatal("a refusal demotion must not carry the distill billing group fields")
		}
	}
}
