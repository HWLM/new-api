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

// TestClientAbortedBeforeAnyDataAPIError_ClientGoneWithContent 客户端断开且上游
// 已经开始吐实质内容 → 走 estimation 兜底，不豁免。
func TestClientAbortedBeforeAnyDataAPIError_ClientGoneWithContent(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()
	info := &relaycommon.RelayInfo{
		StreamStatus:          relaycommon.NewStreamStatus(),
		ReceivedResponseCount: 5,    // 计数不影响判定
		SawStreamContentDelta: true, // 关键：上游已开始生成实质内容
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)

	assert.Nil(t, ClientAbortedBeforeAnyDataAPIError(c, info),
		"客户端断开但上游已吐出实质生成内容 → 走 estimation 兜底而不是完全豁免，因为部分成本已发生")
	assert.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyRefundReason))
}

// TestClientAbortedBeforeAnyDataAPIError_ClientGoneOnlyMetadata 是本次修复的
// 核心用例：客户端断开时 scanner 已经收到过帧（比如 message_start / ping），
// 但从来没看到 content_block_delta 等实质生成帧 → 必须豁免全额扣费。
//
// 早期版本（3fdc5e41）的判定 `ReceivedResponseCount == 0` 在这里会失败：
// message_start 会让计数 ≥ 1 → 兜底扣费 → 用户被错扣。
func TestClientAbortedBeforeAnyDataAPIError_ClientGoneOnlyMetadata(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()
	info := &relaycommon.RelayInfo{
		StreamStatus:          relaycommon.NewStreamStatus(),
		ReceivedResponseCount: 3,     // 收到了 message_start + 2 帧 ping / content_block_start
		SawStreamContentDelta: false, // 关键：没看到任何实质生成 delta
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)

	apiErr := ClientAbortedBeforeAnyDataAPIError(c, info)
	require.NotNil(t, apiErr,
		"客户端断开时只收到元数据帧（没有 content_block_delta / .delta 等实质生成）→ 必须返回错误以触发退费")

	assert.Equal(t, 499, apiErr.StatusCode,
		"用 nginx 惯用的 499 表达 client closed request；不用 502 避免被误当作上游故障")
	assert.True(t, types.IsSkipRetryError(apiErr),
		"客户端已经断了，任何重试都是浪费且可能对下游二次扣费")
	assert.Contains(t, apiErr.Error(), clientAbortedNoDataMessage)
	assert.Equal(t, constant.RefundReasonClientAbortedNoData,
		common.GetContextKeyString(c, constant.ContextKeyRefundReason),
		"必须写 refund_reason 供跨系统对账")
}

// TestClientAbortedBeforeAnyDataAPIError_ClientGoneNoData 原有的极端场景：
// 一帧都没收到就断开。修复后依旧命中豁免（SawStreamContentDelta 默认为 false）。
func TestClientAbortedBeforeAnyDataAPIError_ClientGoneNoData(t *testing.T) {
	t.Parallel()

	c := newTestGinContext()
	info := &relaycommon.RelayInfo{
		StreamStatus:          relaycommon.NewStreamStatus(),
		ReceivedResponseCount: 0,     // 上游一帧都没吐
		SawStreamContentDelta: false, // （默认值）
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)

	apiErr := ClientAbortedBeforeAnyDataAPIError(c, info)
	require.NotNil(t, apiErr, "客户端在收到任何上游数据前就断开时，必须返回错误以触发退费")

	assert.Equal(t, 499, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Contains(t, apiErr.Error(), clientAbortedNoDataMessage)
	assert.Equal(t, constant.RefundReasonClientAbortedNoData,
		common.GetContextKeyString(c, constant.ContextKeyRefundReason))
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
		SawStreamContentDelta: false,
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, nil)
	assert.NotNil(t, ClientAbortedBeforeAnyDataAPIError(nil, info),
		"nil gin.Context 只跳过写 refund_reason，仍需返回 apiErr 以触发退费")
}
