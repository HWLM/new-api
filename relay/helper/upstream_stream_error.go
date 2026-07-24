package helper

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// SSE event 白名单：识别为「上游流已开始后明确失败」的终止事件类型。
// - event: error                 通用 SSE 错误（chat completions / anthropic 兼容格式常用）
// - event: response.failed       OpenAI Responses API 严格终止事件（Codex CLI 等严格 SDK 依赖）
// - event: response.error        OpenAI Responses API 错误终止事件的另一种拼写（兜底覆盖）
//
// 保守白名单：其他 event 类型（ping / message_stop / content_block_stop 等）
// 保持现有丢弃行为，不影响正常流。
const (
	UpstreamSSEEventError              = "error"
	UpstreamSSEEventResponseFailed     = "response.failed"
	UpstreamSSEEventResponseError      = "response.error"
	unknownUpstreamStreamErrorFallback = "upstream returned SSE error event"
)

// IsUpstreamErrorEventType 判断 SSE `event:` 头是否属于上游错误终止事件。
// 仅识别白名单类型，避免误伤自定义 event 协议（如 dify）。
func IsUpstreamErrorEventType(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case UpstreamSSEEventError, UpstreamSSEEventResponseFailed, UpstreamSSEEventResponseError:
		return true
	default:
		return false
	}
}

// ExtractUpstreamErrorMessage 从上游错误 SSE 事件的 data 载荷中提取对客错误消息。
// 兼容三种常见 JSON 形状（覆盖 chat completions / responses / anthropic messages）：
//   - {"error":{"message":"...","type":"..."}}                通用
//   - {"type":"error","error":{"message":"..."}}              anthropic messages
//   - {"type":"response.failed","response":{"error":{"message":"..."}}}  responses api
//
// 全部落空时返回 unknownUpstreamStreamErrorFallback，避免下游拿到空 message。
func ExtractUpstreamErrorMessage(dataPayload string) string {
	if strings.TrimSpace(dataPayload) == "" {
		return unknownUpstreamStreamErrorFallback
	}
	// error.message
	if msg := strings.TrimSpace(gjson.Get(dataPayload, "error.message").String()); msg != "" {
		return msg
	}
	// response.error.message (OpenAI Responses)
	if msg := strings.TrimSpace(gjson.Get(dataPayload, "response.error.message").String()); msg != "" {
		return msg
	}
	// message 顶层字段
	if msg := strings.TrimSpace(gjson.Get(dataPayload, "message").String()); msg != "" {
		return msg
	}
	return unknownUpstreamStreamErrorFallback
}

// UpstreamStreamErrorToAPIError 若 StreamStatus 观察到过上游错误终止事件，
// 返回对应的 *types.NewAPIError（对客固定 502 upstream_error）；否则返回 nil。
//
// 判定用 StreamStatus.ObservedUpstreamError()：
//   - 严格路径：EndReason == StreamEndReasonUpstreamError（scanner 抢先设置）
//   - 兜底路径：EndReason == StreamEndReasonClientGone 且 HasErrors() 为真
//     （scanner 收到过 event: error 但 context.Done 抢跑了 endOnce.Do）
//
// 兜底路径是本次修复引入：实测上线 7 天 upstream_error 计数为 0，全部被 client_gone
// 抢跑；改用 ObservedUpstreamError() 后，只要 scanner 实际观察到错误帧就走退款，
// 修正跨系统对账的错扣。
//
// 副作用：命中时在 gin.Context 上记录 ContextKeyRefundReason=upstream_stream_error，
// 供 controller.processChannelError 写入 logs.other.refund_reason 字段，
// 便于 sub2api 侧对账脚本反查 newApi 是否真的退了费。
//
// 所有复用 helper.StreamScannerHandler 的 stream handler，应在函数尾部
// 「本地估算 usage / HandleFinalResponse / return 空错误」之前，先调用此函数：
// 命中时直接把该错误返回给调用方，触发 controller 层的 Refund 逻辑，
// 避免估算的 prompt token 被写入 logs 表并从用户额度扣除。
func UpstreamStreamErrorToAPIError(c *gin.Context, status *relaycommon.StreamStatus) *types.NewAPIError {
	if status == nil || !status.ObservedUpstreamError() {
		return nil
	}
	msg := status.FirstErrorMessage()
	if msg == "" {
		msg = unknownUpstreamStreamErrorFallback
	}
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyRefundReason, constant.RefundReasonUpstreamStreamError)
	}
	return types.NewOpenAIError(
		fmt.Errorf("upstream stream terminated with error: %s", msg),
		types.ErrorCodeBadResponseBody,
		http.StatusBadGateway,
	)
}
