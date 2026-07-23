package helper

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// clientAbortedNoDataMessage 是 ClientAbortedBeforeAnyDataAPIError 返回错误的对客消息。
// 提取为常量供单测精确断言。保留旧命名（"no data"）以兼容既有引用；
// 新语义覆盖「未收到任何数据」以及「只收到 message_start / ping 等元数据、未收到实质生成内容」两种情况。
const clientAbortedNoDataMessage = "client disconnected before any upstream content received"

// ClientAbortedBeforeAnyDataAPIError 判定「客户端断开时，上游还没吐出任何实质生成内容」，
// 命中时返回一个 *types.NewAPIError 以豁免本地 token 估算兜底扣费；否则返回 nil。
//
// 判定条件（同时满足）：
//   - info.StreamStatus.EndReason == StreamEndReasonClientGone（客户端主动断开）
//   - info.SawStreamContentDelta == false（scanner 从未看到 content_block_delta /
//     response.*.delta / chat delta.content 等实质生成帧；只收到 message_start /
//     ping / content_block_start 等元数据帧也算）
//
// 背景：这类失败上游可能已完整处理请求并对 sub2api 侧计费，但 newApi 侧对客户端
// 而言只吐出了元数据（没有任何用户可见的生成字符）。原有的 estimation 兜底会用
// info.GetEstimatePromptTokens() 凑一个 usage，按估算的 prompt tokens 全额扣费。
// 这对客户端不合理（他们没看到任何实质内容），也让跨系统对账极难对齐 —— 应豁免。
//
// 早期版本（3fdc5e41）的判定用 ReceivedResponseCount == 0，实测上线 7 天内一次未
// 命中：sub2api 的 Anthropic 桥在客户端断开前基本都已发出 message_start，导致
// ReceivedResponseCount ≥ 1，全部走 estimation 兜底扣费。改用 SawStreamContentDelta
// 后，只有真正开始生成用户可见内容才计费。
//
// 副作用：在 gin.Context 上记录 ContextKeyRefundReason=client_aborted_no_data，
// 供 controller.processChannelError 写入 logs.other.refund_reason 字段，
// 便于 sub2api 侧对账脚本反查 newApi 是否真的退了费。
//
// 使用约定：所有复用 helper.StreamScannerHandler 的 stream handler，应在
// UpstreamStreamErrorToAPIError 检查之后、estimation/HandleFinalResponse
// 之前调用一次。示例：
//
//	if apiErr := helper.UpstreamStreamErrorToAPIError(c, info.StreamStatus); apiErr != nil {
//	    return nil, apiErr
//	}
//	if apiErr := helper.ClientAbortedBeforeAnyDataAPIError(c, info); apiErr != nil {
//	    return nil, apiErr
//	}
//	// ↓ 只有真的收到了实质生成内容才走 estimation 兜底
//	if !containStreamUsage {
//	    usage = service.ResponseText2Usage(...)
//	}
func ClientAbortedBeforeAnyDataAPIError(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil || info.StreamStatus == nil {
		return nil
	}
	if info.StreamStatus.EndReason != relaycommon.StreamEndReasonClientGone {
		return nil
	}
	if info.SawStreamContentDelta {
		// 上游已经开始吐出实质生成内容（text delta / tool_calls args / thinking 等），
		// 客户端断开时按已生成部分做 estimation 兜底是合理的：上游生成成本真实存在。
		return nil
	}

	if c != nil {
		common.SetContextKey(c, constant.ContextKeyRefundReason, constant.RefundReasonClientAbortedNoData)
	}

	return types.NewOpenAIError(
		fmt.Errorf(clientAbortedNoDataMessage),
		types.ErrorCodeClientAbortedNoData,
		// 499 Client Closed Request（nginx 惯用非标状态码），语义上是「客户端主动
		// 关闭连接」。选它是因为：
		//   - 502/504 会误导为「上游故障」，可能触发 ShouldRetry 与 ShouldDisableChannel；
		//   - 客户端已断开，本次响应的实际状态码对客户端不可见，仅用于内部日志/统计。
		// 配合 ErrOptionWithSkipRetry() 明确跳过重试和渠道禁用检查。
		499,
		types.ErrOptionWithSkipRetry(),
	)
}
