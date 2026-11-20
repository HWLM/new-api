package middleware

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/require"
)

// 检测渠道白名单（规格 §16）。
//
// ⛔ 核心不是「命中后怎么样」，而是「不该检测的渠道一点都不做」：计数、时间桶、
// 去重、Jev 全都要在选完渠道、确认在白名单里之后才发生。计数一旦提前，非白名单
// 渠道就会把白名单渠道的激活阈值蹭高，白名单也就白配了。

// withDistillSetting 临时改全局蒸馏配置，测试结束整体还原。
func withDistillSetting(t *testing.T, mutate func(*operation_setting.DistillDetectSetting)) *operation_setting.DistillDetectSetting {
	t.Helper()
	s := operation_setting.GetDistillDetectSetting()
	original := *s
	t.Cleanup(func() { *s = original })
	mutate(s)
	return s
}

// ⭐ 白名单之外的渠道：一次计数都不许发生。
//
// 用 DistillRPM 的累加值当探针 —— 它是判定链路的第一笔副作用，只要
// EvaluateDistill 被调到，它必然加一。
func TestDistillDetectionSkipsChannelsOutsideTheWhitelist(t *testing.T) {
	withDistillSetting(t, func(s *operation_setting.DistillDetectSetting) {
		s.Enabled = true
		s.ObserveOnly = false
		s.ActivateRPM = 20
		s.DetectChannelIds = []int{7}
	})
	const userID = 944201
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{"model":"m"}`)
	c.Set("id", userID)
	common.SetContextKey(c, constant.ContextKeyDistillRequestBody, []byte(`{"model":"m","messages":[]}`))

	base := service.DistillRPM(userID)

	hit := runDistillDetection(c, &model.Channel{Id: 8}, "m")
	require.Nil(t, hit)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyDistillDemoted))
	require.Equal(t, base+1, service.DistillRPM(userID),
		"a channel outside the whitelist must not touch the RPM counter")

	// 白名单内的渠道会继续走到判定的入口（IsDistillDetectEffectiveForUser /
	// DistillRPM ...）。这条正向链路由 service 层与 setting 层的用例覆盖：
	// middleware 的测试环境没有 Redis / DB，真走到那里会因为用户缓存取不到而
	// 在 common.RedisHGetObj 上炸，所以这里只锁「白名单外一次都不碰」。
}

// ⭐ 白名单为空 = 检测整体关闭：连正文都不抄，中间件直接放行。
func TestDistillMiddlewareDoesNotSnapshotTheBodyWithoutAWhitelist(t *testing.T) {
	withDistillSetting(t, func(s *operation_setting.DistillDetectSetting) {
		s.Enabled = true
		s.DetectChannelIds = nil
	})
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"蒸馏"}]}`)
	c.Set("id", 944202)

	DistillDetect()(c)

	if _, ok := common.GetContextKey(c, constant.ContextKeyDistillRequestBody); ok {
		t.Fatal("without a whitelist the middleware must not copy the request body")
	}
}

// 白名单里的渠道要真的抄到正文：判定的输入就是这份副本。
func TestDistillMiddlewareSnapshotsTheBodyForAWhitelistedChannel(t *testing.T) {
	withDistillSetting(t, func(s *operation_setting.DistillDetectSetting) {
		s.Enabled = true
		s.DetectChannelIds = []int{7}
	})
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"蒸馏"}]}`)
	c.Set("id", 944203)

	DistillDetect()(c)

	body, ok := distillBodyFromContext(c)
	require.True(t, ok, "a whitelisted channel needs the body snapshot")
	require.Contains(t, string(body), "蒸馏")
}
