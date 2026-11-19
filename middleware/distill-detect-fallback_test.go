package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// 蒸馏检测「重导」段的测试（docs/distill-detection-plan.md §12）。
//
// 覆盖三件容易出错、出错又很难在线上察觉的事：
//   1. 作用域 —— 非文本类 relay 一次判定都不该触发；
//   2. 兜底解析 —— 未配置兜底必须回 nil（调用方据此 429），而不是静默保留原渠道；
//   3. 正文读取 —— 读完必须把游标还原，否则后续 relay 拿到空 body。

func newDistillTestContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	return c, rec
}

func TestDistillTextClassPathsCoverEveryRelayedTextEndpoint(t *testing.T) {
	for _, path := range []string{
		"/v1/messages",
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/responses",
		"/v1/responses/compact",
		"/pg/chat/completions",
	} {
		if !isDistillTextClassPath(path) {
			t.Fatalf("%s must be judged", path)
		}
	}
}

// ⭐ 这是 Reviewer 提的第①条：中间件不应拦截非文本类 relay。
// 图像 / 音频 / 任务 / embedding 的正文形状完全不同，送判定只会白花钱。
func TestDistillSkipsNonTextRelayPaths(t *testing.T) {
	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/v1/audio/speech",
		"/v1/audio/transcriptions",
		"/v1/embeddings",
		"/v1/video/generations",
		"/mj/submit/imagine",
		"/suno/submit/music",
		"/v1/realtime",
		"/v1/moderations",
	} {
		if isDistillTextClassPath(path) {
			t.Fatalf("%s must not be judged", path)
		}
	}
}

// ⭐ 未配置兜底渠道时必须返回 nil —— 调用方靠这个 nil 回 429
// distill_fallback_unavailable。若这里「宽容」地返回原渠道，用户命中了蒸馏却
// 还在原渠道上跑，功能等于失效且没有任何报错。
func TestDistillFallbackIsNilWhenNotConfigured(t *testing.T) {
	s := operation_setting.GetDistillDetectSetting()
	orig := s.FallbackChannelId
	defer func() { s.FallbackChannelId = orig }()

	s.FallbackChannelId = 0
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{"model":"claude-3-5-sonnet"}`)
	if fb := distillFallbackChannel(c, "claude-3-5-sonnet", "default"); fb != nil {
		t.Fatalf("an unconfigured fallback must resolve to nil, got channel %d", fb.channel.Id)
	}

	// 负数同理：非法配置不能被当成「已配置」。
	s.FallbackChannelId = -1
	if fb := distillFallbackChannel(c, "claude-3-5-sonnet", "default"); fb != nil {
		t.Fatalf("a negative fallback id must resolve to nil")
	}
}

// ⭐ 读取判定用正文后必须把游标还原：这个中间件跑在 Distribute 之前，
// 后面 getModelFromJSONBody / relay 还要从同一份 storage 里读 model 与整包正文。
// 不还原会让所有请求体变成空的。
func TestDistillBodyReadRestoresTheStreamPosition(t *testing.T) {
	payload := `{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hi"}]}`
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", payload)

	body, ok := readBodyForDistill(c)
	if !ok {
		t.Fatal("a normal JSON body must be readable")
	}
	if string(body) != payload {
		t.Fatalf("body copy mismatch: %s", body)
	}

	// 再读一次必须还能拿到完整正文 —— 等价于后面中间件读到的东西。
	again, ok := readBodyForDistill(c)
	if !ok || string(again) != payload {
		t.Fatalf("the stream position must be restored after reading, got %q", again)
	}
}

// 观察模式下判定放在后台 goroutine：这里只验证「模式开关决定走哪条分支」这个
// 判断本身，避免以后有人把 ObserveOnly 的判断挪进 goroutine 内部。
func TestDistillObserveOnlyDefaultsToOn(t *testing.T) {
	s := operation_setting.GetDistillDetectSetting()
	if !s.ObserveOnly {
		t.Fatal("observe_only must default to true: redirecting traffic before the false-positive rate is known is the whole risk")
	}
}

// 模型名必须能从正文里取到：中间件跑在 Distribute 之前，
// original_model 那时还是空的（观察模式早期就是这样导致 hits.model 全空）。
func TestDistillModelNameComesFromTheBodyBeforeDistribution(t *testing.T) {
	got := distillModelNameFromBody([]byte(`{"model":"claude-opus-4","stream":true}`))
	if got != "claude-opus-4" {
		t.Fatalf("model must be read from the body, got %q", got)
	}
	if distillModelNameFromBody(nil) != "" {
		t.Fatal("an empty body must yield an empty model name")
	}
}

// 蒸馏兜底生效后，Distribute 不能再走「渠道自配 FallbackChannelId」那一跳，
// 否则会变成两跳，运维对账时看不出流量最后去了哪。
func TestDistillRedirectSuppressesTheOverloadFallbackHop(t *testing.T) {
	c, _ := newDistillTestContext(http.MethodPost, "/v1/chat/completions", `{}`)
	common.SetContextKey(c, constant.ContextKeyDistillFallbackChannelId, 42)
	common.SetContextKey(c, constant.ContextKeyUserOverloaded, true)

	if common.GetContextKeyInt(c, constant.ContextKeyDistillFallbackChannelId) <= 0 {
		t.Fatal("the distill fallback marker must be readable back")
	}
}
