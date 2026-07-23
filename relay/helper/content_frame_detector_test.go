package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsContentBearingFrame_Anthropic 覆盖 Anthropic Messages 事件白名单。
func TestIsContentBearingFrame_Anthropic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		eventType string
		data      string
		want      bool
	}{
		// 实质生成 —— content_block_delta 且带真实 delta 字段
		{
			name:      "text_delta 有内容",
			eventType: "content_block_delta",
			data:      `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			want:      true,
		},
		{
			name:      "thinking_delta 有内容",
			eventType: "content_block_delta",
			data:      `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"analyzing..."}}`,
			want:      true,
		},
		{
			name:      "input_json_delta（tool_use 参数）",
			eventType: "content_block_delta",
			data:      `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"loc\":"}}`,
			want:      true,
		},

		// 元数据/生命周期帧 —— 必须为 false，触发退款豁免
		{
			name:      "message_start 只是元数据",
			eventType: "message_start",
			data:      `{"type":"message_start","message":{"id":"msg_x","usage":{"input_tokens":10}}}`,
			want:      false,
		},
		{
			name:      "message_delta 用来带 usage / stop_reason，没有生成内容",
			eventType: "message_delta",
			data:      `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			want:      false,
		},
		{
			name:      "message_stop",
			eventType: "message_stop",
			data:      `{"type":"message_stop"}`,
			want:      false,
		},
		{
			name:      "content_block_start（开始一个 block，还没内容）",
			eventType: "content_block_start",
			data:      `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			want:      false,
		},
		{
			name:      "content_block_stop",
			eventType: "content_block_stop",
			data:      `{"type":"content_block_stop","index":0}`,
			want:      false,
		},
		{
			name:      "ping",
			eventType: "ping",
			data:      `{"type":"ping"}`,
			want:      false,
		},

		// content_block_delta 但 delta 字段为空 —— 不算实质内容
		{
			name:      "content_block_delta 空 delta",
			eventType: "content_block_delta",
			data:      `{"type":"content_block_delta","index":0,"delta":{}}`,
			want:      false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsContentBearingFrame(tc.eventType, tc.data)
			assert.Equal(t, tc.want, got, "eventType=%q data=%q", tc.eventType, tc.data)
		})
	}
}

// TestIsContentBearingFrame_OpenAIResponses 覆盖 OpenAI Responses API 事件。
func TestIsContentBearingFrame_OpenAIResponses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		eventType string
		want      bool
	}{
		// response.*.delta —— 全部视作实质生成
		{"output_text.delta", "response.output_text.delta", true},
		{"reasoning_summary_text.delta", "response.reasoning_summary_text.delta", true},
		{"function_call_arguments.delta", "response.function_call_arguments.delta", true},
		{"audio.delta", "response.audio.delta", true},
		{"mcp_call.output.delta", "response.mcp_call.output.delta", true},

		// 生命周期事件 —— 元数据，不算实质
		{"response.created", "response.created", false},
		{"response.in_progress", "response.in_progress", false},
		{"response.completed", "response.completed", false},
		{"response.failed", "response.failed", false},
		{"response.incomplete", "response.incomplete", false},
		{"response.cancelled", "response.cancelled", false},
		{"response.queued", "response.queued", false},
		{"response.output_item.added", "response.output_item.added", false},
		{"response.output_item.done", "response.output_item.done", false},
		{"response.content_part.added", "response.content_part.added", false},
		{"response.content_part.done", "response.content_part.done", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsContentBearingFrame(tc.eventType, `{"anything":"ignored"}`)
			assert.Equal(t, tc.want, got, "eventType=%q", tc.eventType)
		})
	}
}

// TestIsContentBearingFrame_OpenAIChat 覆盖 OpenAI Chat Completions（无 event: 行）。
func TestIsContentBearingFrame_OpenAIChat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data string
		want bool
	}{
		// 实质生成
		{
			name: "delta.content 有内容",
			data: `{"id":"x","choices":[{"index":0,"delta":{"content":"Hi"}}]}`,
			want: true,
		},
		{
			name: "delta.reasoning_content（DeepSeek/o1 风格）",
			data: `{"choices":[{"delta":{"reasoning_content":"thinking..."}}]}`,
			want: true,
		},
		{
			name: "delta.reasoning（另一种拼写）",
			data: `{"choices":[{"delta":{"reasoning":"analyzing"}}]}`,
			want: true,
		},
		{
			name: "delta.tool_calls 带 function.arguments",
			data: `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc\":"}}]}}]}`,
			want: true,
		},
		{
			name: "delta.tool_calls 带 function.name",
			data: `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"get_weather"}}]}}]}`,
			want: true,
		},
		{
			name: "delta.audio.data",
			data: `{"choices":[{"delta":{"audio":{"data":"AAA="}}}]}`,
			want: true,
		},

		// 元数据帧
		{
			name: "只带 role 的首帧",
			data: `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
			want: false,
		},
		{
			name: "全空 delta",
			data: `{"choices":[{"delta":{}}]}`,
			want: false,
		},
		{
			name: "finish_reason=stop + 空 delta（收尾帧）",
			data: `{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			want: false,
		},
		{
			name: "finish_reason=length + 空 delta（截断收尾帧）",
			data: `{"choices":[{"delta":{},"finish_reason":"length"}]}`,
			want: false,
		},

		// 非 chat 结构 —— 保守当作有内容，避免对未知格式误退款
		{
			name: "非 chat JSON（保守判定）",
			data: `{"foo":"bar"}`,
			want: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsContentBearingFrame("", tc.data)
			assert.Equal(t, tc.want, got, "data=%q", tc.data)
		})
	}
}

// TestIsContentBearingFrame_UpstreamErrorEvents 上游错误终止事件白名单帧
// （event: error / response.failed / response.error）属于错误通知，不是用户可见的
// 生成内容。这些帧走 UpstreamStreamErrorToAPIError 单独退款路径，
// SawStreamContentDelta 不应被它们污染，否则 ClientAborted... 会漏放退款。
func TestIsContentBearingFrame_UpstreamErrorEvents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		eventType string
		data      string
	}{
		{"error", `{"type":"error","error":{"message":"upstream busy"}}`},
		{"response.failed", `{"type":"response.failed","response":{"error":{"message":"x"}}}`},
		{"response.error", `{"error":{"message":"boom"}}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.eventType, func(t *testing.T) {
			t.Parallel()
			assert.False(t, IsContentBearingFrame(tc.eventType, tc.data),
				"上游错误 event 帧不是「实质生成内容」")
		})
	}
}

// TestIsContentBearingFrame_EdgeCases 覆盖边界输入。
func TestIsContentBearingFrame_EdgeCases(t *testing.T) {
	t.Parallel()

	// 空 data 一律 false —— 没内容就没内容
	assert.False(t, IsContentBearingFrame("", ""))
	assert.False(t, IsContentBearingFrame("content_block_delta", ""))
	assert.False(t, IsContentBearingFrame("response.output_text.delta", "   "))

	// 未知 event 类型 + 非 chat JSON → 保守当作有内容
	assert.True(t, IsContentBearingFrame("custom.event", `{"weird":"payload"}`))

	// eventType 带首尾空格也能识别
	assert.True(t, IsContentBearingFrame(" content_block_delta ",
		`{"delta":{"text":"Hi"}}`))
	assert.False(t, IsContentBearingFrame(" ping ", `{"type":"ping"}`))
}
