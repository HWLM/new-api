package middleware

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/require"
)

// 蒸馏兜底的计费分组选取（规格 §15）。
//
// ⛔ 与并发超限/拒答降级不同：那两套沿用用户原分组以求计费口径稳定；
// 蒸馏兜底是强制降级，必须按兜底渠道的价格结算。这里是这条规则的守卫。

func withDistillGroupRatios(t *testing.T, jsonStr string) {
	t.Helper()
	original := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(original))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(jsonStr))
}

// ⭐ 兜底渠道里有与用户原分组同名的分组：直接用它，计费口径最平滑。
func TestPickDistillFallbackGroupPrefersTheMatchingGroup(t *testing.T) {
	withDistillGroupRatios(t, `{"default":1,"backup":3}`)
	ch := &model.Channel{Id: 7, Group: "default,backup"}
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{}`)

	group, ok := pickDistillFallbackGroup(c, ch, "default")
	if !ok || group != "default" {
		t.Fatalf("the matching group must win, got %q ok=%v", group, ok)
	}
}

// ⭐ 这是实测到的 bug 的另一半：用户原分组不在兜底渠道里时，必须切到兜底
// 渠道自己的分组，否则价格根本没变（仍按原分组倍率结算）。
func TestPickDistillFallbackGroupSwitchesToTheFallbackChannelsGroup(t *testing.T) {
	withDistillGroupRatios(t, `{"default":1,"premium":5,"backup":3}`)
	ch := &model.Channel{Id: 7, Group: "premium,backup"}
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{}`)

	group, ok := pickDistillFallbackGroup(c, ch, "default")
	if !ok || group != "premium" {
		t.Fatalf("the first priced group of the fallback channel must be used, got %q ok=%v", group, ok)
	}
}

// 没配倍率的分组跳过：落到默认 1.0 会让「兜底价」看起来像没打折也没加价。
func TestPickDistillFallbackGroupSkipsGroupsWithoutARatio(t *testing.T) {
	withDistillGroupRatios(t, `{"default":1,"backup":3}`)
	ch := &model.Channel{Id: 7, Group: "unpriced,backup"}
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{}`)

	group, ok := pickDistillFallbackGroup(c, ch, "default")
	if !ok || group != "backup" {
		t.Fatalf("a group without a configured ratio must be skipped, got %q ok=%v", group, ok)
	}
}

// 兜底渠道一个配了倍率的分组都没有：维持原分组并返回 ok=false，
// 让调用方打 warn —— 这时代码按原价结算，运营需要知道「其实没改价」。
func TestPickDistillFallbackGroupFallsBackToTheOriginGroup(t *testing.T) {
	withDistillGroupRatios(t, `{"default":1}`)
	ch := &model.Channel{Id: 7, Group: "unpriced,alsounpriced"}
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{}`)

	group, ok := pickDistillFallbackGroup(c, ch, "default")
	if ok {
		t.Fatal("without any priced group the caller must be told the billing group did not change")
	}
	if group != "default" {
		t.Fatalf("the origin group must be kept, got %q", group)
	}
}

// auto 不是具体分组名，不能拿它去匹配。
func TestPickDistillFallbackGroupIgnoresTheAutoPseudoGroup(t *testing.T) {
	withDistillGroupRatios(t, `{"default":1,"backup":3}`)
	ch := &model.Channel{Id: 7, Group: "auto,backup"}
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{}`)

	group, ok := pickDistillFallbackGroup(c, ch, "auto")
	if !ok || group != "backup" {
		t.Fatalf("an auto origin group must resolve to a real fallback group, got %q ok=%v", group, ok)
	}
}
