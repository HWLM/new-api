package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientAbortedBeforeAnyDataAPIError_NotClientGone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		endReason relaycommon.StreamEndReason
	}{
		{"done", relaycommon.StreamEndReasonDone},
		{"eof", relaycommon.StreamEndReasonEOF},
		{"handler_stop", relaycommon.StreamEndReasonHandlerStop},
		{"timeout", relaycommon.StreamEndReasonTimeout},
		{"scanner_error", relaycommon.StreamEndReasonScannerErr},
		{"upstream_error", relaycommon.StreamEndReasonUpstreamError},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newTestGinContext()
			info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
			info.StreamStatus.SetEndReason(tc.endReason, nil)

			assert.Nil(t, ClientAbortedBeforeAnyDataAPIError(c, info),
				"非 ClientGone 端因不该命中，避免误伤正常/其他失败路径")
			assert.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyRefundReason),
				"未命中路径不应写 refund_reason")
		})
	}
}

func TestClientAbortedBeforeAnyDataAPIError_ClientGoneWithData(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()
	info := &relaycommon.RelayInfo{
		StreamStatus:          relaycommon.NewStreamStatus(),
		ReceivedResponseCount: 3, // 已经收到部分上游数据
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)

	assert.Nil(t, ClientAbortedBeforeAnyDataAPIError(c, info),
		"客户端断开但已收到部分上游数据 → 走 estimation 兜底而不是完全豁免，因为部分成本已发生")
	assert.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyRefundReason))
}

func TestClientAbortedBeforeAnyDataAPIError_ClientGoneNoData(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()
	info := &relaycommon.RelayInfo{
		StreamStatus:          relaycommon.NewStreamStatus(),
		ReceivedResponseCount: 0, // 上游一帧都没吐
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)

	apiErr := ClientAbortedBeforeAnyDataAPIError(c, info)
	require.NotNil(t, apiErr, "客户端在收到任何上游数据前就断开时，必须返回错误以触发退费")

	// 状态码应该反映「客户端主动关闭」而不是「上游故障」，避免误触发渠道禁用/重试逻辑。
	assert.Equal(t, 499, apiErr.StatusCode,
		"用 nginx 惯用的 499 表达 client closed request；不用 502 避免被误当作上游故障")

	// SkipRetry 必须置位：客户端已断，重试是浪费。
	assert.True(t, types.IsSkipRetryError(apiErr),
		"客户端已经断了，任何重试都是浪费且可能对下游二次扣费")

	// 错误消息包含固定 message，供日志与断言使用。
	assert.Contains(t, apiErr.Error(), clientAbortedNoDataMessage)

	// refund_reason 必须写到 gin.Context，供 processChannelError 落到
	// logs.other.refund_reason，供 sub2api 对账脚本反查。
	assert.Equal(t, constant.RefundReasonClientAbortedNoData,
		common.GetContextKeyString(c, constant.ContextKeyRefundReason),
		"必须写 refund_reason 供跨系统对账")
}

func TestClientAbortedBeforeAnyDataAPIError_NilInputsAreSafe(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()

	assert.Nil(t, ClientAbortedBeforeAnyDataAPIError(c, nil),
		"nil RelayInfo 应安全返回 nil")

	info := &relaycommon.RelayInfo{StreamStatus: nil}
	assert.Nil(t, ClientAbortedBeforeAnyDataAPIError(c, info),
		"nil StreamStatus 应安全返回 nil")

	// nil gin.Context 不应 panic，且仍应正常返回 apiErr
	info = &relaycommon.RelayInfo{
		StreamStatus:          relaycommon.NewStreamStatus(),
		ReceivedResponseCount: 0,
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)
	assert.NotNil(t, ClientAbortedBeforeAnyDataAPIError(nil, info),
		"nil gin.Context 只跳过写 refund_reason，仍需返回 apiErr 以触发退费")
}
