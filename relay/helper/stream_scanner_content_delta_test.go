package helper

import (
	"strings"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
)

// TestStreamScannerHandler_SawStreamContentDelta_ChatContent 用 chat completions
// 风格 SSE（无 event: 行，data: 里 choices[0].delta.content 有值）验证 scanner
// 能把 SawStreamContentDelta 拉起。
func TestStreamScannerHandler_SawStreamContentDelta_ChatContent(t *testing.T) {
	t.Parallel()

	body := `data: {"id":"x","choices":[{"delta":{"content":"Hi"}}]}

data: [DONE]

`
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var called atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		called.Add(1)
	})

	assert.True(t, info.SawStreamContentDelta,
		"chat delta.content 非空 → SawStreamContentDelta 必须为 true")
	assert.GreaterOrEqual(t, info.ReceivedResponseCount, 1)
}

// TestStreamScannerHandler_SawStreamContentDelta_AnthropicMetadataOnly 是本次
// 修复的核心回归：Anthropic 风格 message_start 已经吐了一帧，但没有 content_block_delta
// 就断流。SawStreamContentDelta 必须保持 false，让退款兜底能够生效。
func TestStreamScannerHandler_SawStreamContentDelta_AnthropicMetadataOnly(t *testing.T) {
	t.Parallel()

	body := `event: message_start
data: {"type":"message_start","message":{"id":"msg_x","usage":{"input_tokens":10}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: ping
data: {"type":"ping"}

`
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var called atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		called.Add(1)
	})

	assert.False(t, info.SawStreamContentDelta,
		"只收到 message_start / content_block_start / ping 元数据帧 → SawStreamContentDelta 必须为 false，否则 ClientAbortedBeforeAnyDataAPIError 会漏放退款")
	assert.GreaterOrEqual(t, info.ReceivedResponseCount, int(called.Load()),
		"元数据帧也应计入 ReceivedResponseCount")
}

// TestStreamScannerHandler_SawStreamContentDelta_AnthropicWithDelta 完整的
// Anthropic 流：message_start + content_block_start + 真实 content_block_delta。
// SawStreamContentDelta 必须转为 true。
func TestStreamScannerHandler_SawStreamContentDelta_AnthropicWithDelta(t *testing.T) {
	t.Parallel()

	body := `event: message_start
data: {"type":"message_start","message":{"id":"msg_x"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

`
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	assert.True(t, info.SawStreamContentDelta,
		"Anthropic 收到 content_block_delta.text_delta 后 SawStreamContentDelta 必须为 true")
}

// TestStreamScannerHandler_SawStreamContentDelta_UpstreamErrorEvent 上游发
// event: error → scanner 应把端因设为 UpstreamError；同时 SawStreamContentDelta
// 不应被这个错误帧误置为 true（错误消息不是「实质生成内容」）。
func TestStreamScannerHandler_SawStreamContentDelta_UpstreamErrorEvent(t *testing.T) {
	t.Parallel()

	body := `event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"upstream busy"}}

`
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	assert.Equal(t, relaycommon.StreamEndReasonUpstreamError, info.StreamStatus.EndReason,
		"event: error 必须触发 UpstreamError 端因")
	// 错误帧本身不属于「实质生成内容」；退款走 UpstreamStreamErrorToAPIError 而非 ClientAborted。
	assert.False(t, info.SawStreamContentDelta,
		"上游错误 event 帧不应被误标为实质生成内容")
}

// 复用 setupStreamTest 与 relay/helper 包里 stream_scanner_test.go 的定义。
