package helper

import (
	"strings"

	"github.com/tidwall/gjson"
)

// IsContentBearingFrame 判断一条 SSE data 帧是否为用户可见的实质生成内容
// （text delta / tool_calls args delta / thinking delta / audio delta 等），
// 相对的是元数据/生命周期帧（message_start / ping / response.created /
// content_block_start / 空 delta 只带 role 的帧等）。
//
// 供 stream_scanner 设置 RelayInfo.SawStreamContentDelta 使用；后者驱动
// ClientAbortedBeforeAnyDataAPIError 的退款判定：
//   - 客户端断开时 scanner 从未收到实质内容 → 豁免全额估算扣费
//   - 客户端断开时已经开始收到实质内容 → 按已生成部分做 estimation 兜底
//
// eventType 是紧邻本帧的 `event:` 行的值（chat completions 通常没有 event: 行，
// 传空串）；data 是紧跟 `data:` 的载荷。
//
// 保守默认：无法高置信度分类的 event 类型返回 true（当作有内容），
// 保留旧的计费行为，避免对不熟悉的上游格式引入过度退款。
func IsContentBearingFrame(eventType, data string) bool {
	eventType = strings.TrimSpace(eventType)
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}

	// 上游错误终止事件白名单（event: error / response.failed / response.error）
	// 属于错误通知，不是用户可见的生成内容。这些帧走 UpstreamStreamErrorToAPIError
	// 单独退款路径，不应污染 SawStreamContentDelta 让 ClientAbortedBefore... 漏放退款。
	if IsUpstreamErrorEventType(eventType) {
		return false
	}

	switch eventType {
	// --- Anthropic Messages 事件白名单 ---
	case "content_block_delta":
		// content_block_delta 的 delta 里带 text_delta / thinking_delta /
		// input_json_delta 等；只要有实质字符就算生成内容。
		if gjson.Get(trimmed, "delta.text").String() != "" {
			return true
		}
		if gjson.Get(trimmed, "delta.thinking").String() != "" {
			return true
		}
		if gjson.Get(trimmed, "delta.partial_json").String() != "" {
			return true
		}
		if gjson.Get(trimmed, "delta.input_json_delta").String() != "" {
			return true
		}
		return false
	case "message_start",
		"message_delta",
		"message_stop",
		"content_block_start",
		"content_block_stop",
		"ping":
		// Anthropic 侧显式的元数据/生命周期事件 —— 收到这些不代表已经生成内容。
		return false

	// --- OpenAI Responses API 事件 ---
	case "response.created",
		"response.in_progress",
		"response.completed",
		"response.failed",
		"response.incomplete",
		"response.cancelled",
		"response.queued",
		"response.output_item.added",
		"response.output_item.done",
		"response.content_part.added",
		"response.content_part.done",
		"response.function_call.added",
		"response.function_call.done":
		return false
	}

	// OpenAI Responses：任意 response.*.delta 事件都是实质生成
	// （output_text / reasoning_summary_text / function_call_arguments / audio / image / mcp_call.* 等）。
	if strings.HasPrefix(eventType, "response.") && strings.HasSuffix(eventType, ".delta") {
		return true
	}

	// --- OpenAI Chat Completions（大多不带 event: 行，直接 data: ）---
	if eventType == "" {
		choices := gjson.Get(trimmed, "choices")
		if !choices.IsArray() {
			// 非 chat 结构：保守当作有内容，避免对未知格式误退款。
			return true
		}
		delta := gjson.Get(trimmed, "choices.0.delta")
		if delta.Get("content").String() != "" {
			return true
		}
		if delta.Get("reasoning_content").String() != "" {
			return true
		}
		if delta.Get("reasoning").String() != "" {
			return true
		}
		if tcArr := delta.Get("tool_calls"); tcArr.IsArray() {
			for _, tc := range tcArr.Array() {
				if tc.Get("function.arguments").String() != "" ||
					tc.Get("function.name").String() != "" {
					return true
				}
			}
		}
		if delta.Get("audio.data").String() != "" {
			return true
		}
		// finish_reason 非空 + delta 空 → 结束帧，不是实质内容。
		finish := gjson.Get(trimmed, "choices.0.finish_reason").String()
		if finish != "" && finish != "null" {
			return false
		}
		// 剩余情况：只带 role / 全空 delta → 元数据帧。
		return false
	}

	// 未知的自定义 event 类型 → 保守当作有内容，保留旧行为。
	return true
}
