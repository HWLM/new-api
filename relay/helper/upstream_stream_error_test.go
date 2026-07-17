package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGinContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c
}

func TestIsUpstreamErrorEventType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want bool
	}{
		{"error", true},
		{"response.failed", true},
		{"response.error", true},
		{" error ", true},  // 带空格也识别
		{"ERROR", false},   // 大小写敏感，只识别小写小写标准值
		{"message", false}, // 普通事件
		{"response.output_text.delta", false},
		{"ping", false},
		{"", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, IsUpstreamErrorEventType(tc.in), "input=%q", tc.in)
	}
}

func TestExtractUpstreamErrorMessage(t *testing.T) {
	t.Parallel()

	// 空载荷 → 兜底
	assert.Equal(t, unknownUpstreamStreamErrorFallback, ExtractUpstreamErrorMessage(""))
	assert.Equal(t, unknownUpstreamStreamErrorFallback, ExtractUpstreamErrorMessage("   "))

	// 通用 error.message
	assert.Equal(t, "boom",
		ExtractUpstreamErrorMessage(`{"error":{"type":"upstream_error","message":"boom"}}`))

	// anthropic 风格：type=error, error.message
	assert.Equal(t, "rate limited",
		ExtractUpstreamErrorMessage(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))

	// OpenAI Responses 风格：response.error.message
	assert.Equal(t, "content policy violation",
		ExtractUpstreamErrorMessage(`{"type":"response.failed","response":{"error":{"code":"content_filter","message":"content policy violation"}}}`))

	// 无标准字段：兜底
	assert.Equal(t, unknownUpstreamStreamErrorFallback,
		ExtractUpstreamErrorMessage(`{"weird":"payload"}`))

	// 无效 JSON：gjson 返回空字符串，走兜底
	assert.Equal(t, unknownUpstreamStreamErrorFallback,
		ExtractUpstreamErrorMessage("not json"))
}

func TestUpstreamStreamErrorToAPIError_NilAndNormal(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()

	// nil 输入 → nil
	assert.Nil(t, UpstreamStreamErrorToAPIError(c, nil))

	// 正常结束 → nil
	s := relaycommon.NewStreamStatus()
	s.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	assert.Nil(t, UpstreamStreamErrorToAPIError(c, s))

	// EOF 结束（即使有软错误）→ nil，避免把普通网络抖动误升级为上游错误
	s = relaycommon.NewStreamStatus()
	s.RecordError("some soft error")
	s.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	assert.Nil(t, UpstreamStreamErrorToAPIError(c, s))

	// 未命中路径不应写 refund_reason
	assert.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyRefundReason))
}

func TestUpstreamStreamErrorToAPIError_UpstreamErrorEnd(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()
	s := relaycommon.NewStreamStatus()
	s.RecordError("upstream boom")
	s.SetEndReason(relaycommon.StreamEndReasonUpstreamError, nil)

	apiErr := UpstreamStreamErrorToAPIError(c, s)
	require.NotNil(t, apiErr, "端因是 UpstreamError 时必须返回非空错误")

	// 返回的应该是 StatusBadGateway，让上层 controller 走 Refund 路径
	// 而不是直接把非 502 的错误码回给客户端
	assert.Equal(t, 502, apiErr.StatusCode)

	// 错误消息中包含从 StreamStatus 提取的 payload message
	assert.Contains(t, apiErr.Error(), "upstream boom")

	// 命中路径必须把 refund_reason 写入 gin.Context，供 processChannelError 读取
	assert.Equal(t, constant.RefundReasonUpstreamStreamError,
		common.GetContextKeyString(c, constant.ContextKeyRefundReason))
}

func TestUpstreamStreamErrorToAPIError_FallbackMessageWhenEmpty(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()
	// 端因是 UpstreamError 但没记录任何错误消息 → 用 fallback 而不是空串
	s := relaycommon.NewStreamStatus()
	s.SetEndReason(relaycommon.StreamEndReasonUpstreamError, nil)

	apiErr := UpstreamStreamErrorToAPIError(c, s)
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), unknownUpstreamStreamErrorFallback)
	// 明确不会 panic 也不会输出空错误
	assert.NotEmpty(t, apiErr.Error())
	// 返回的类型是 NewAPIError
	var typed *types.NewAPIError
	assert.ErrorAs(t, apiErr, &typed)
}

func TestUpstreamStreamErrorToAPIError_NilContextIsSafe(t *testing.T) {
	t.Parallel()

	// nil gin.Context 时不能 panic：内部使用 SetContextKey 有 nil 保护，
	// 但也应显式验证一下 —— handler 传入 nil c 时至少要能返回 apiErr。
	s := relaycommon.NewStreamStatus()
	s.SetEndReason(relaycommon.StreamEndReasonUpstreamError, nil)

	assert.NotNil(t, UpstreamStreamErrorToAPIError(nil, s))
}
