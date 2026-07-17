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
// 提取为常量供单测精确断言。
const clientAbortedNoDataMessage = "client disconnected before any upstream data received"

// ClientAbortedBeforeAnyDataAPIError 判定「客户端在收到任何上游 SSE 数据前就断开」，
// 命中时返回一个 *types.NewAPIError 以豁免本地 token 估算兜底扣费；否则返回 nil。
//
// 判定条件（同时满足）：
//   - info.StreamStatus.EndReason == StreamEndReasonClientGone（客户端主动断开）
//   - info.ReceivedResponseCount == 0（scanner 从未收到任何上游 data 帧）
//
// 背景：这类失败上游可能已完整处理请求并对 sub2api 侧计费，但 newApi 侧
// 什么字节都没拿到。原有的 estimation 兜底会用 info.GetEstimatePromptTokens()
// 凑一个 usage，然后按估算的 prompt tokens 扣费。这对客户端不合理（他们没
// 看到任何字节），也让跨系统对账极难对齐 —— 应豁免。
//
// 副作用：在 gin.Context 上记录 ContextKeyRefundReason=client_aborted_no_data，
// 供 controller.processChannelError 写入 logs.other.refund_reason 字段，
// 便于 sub2api 侧对账脚本反查 newApi 是否真的退了费。
//
// 使用约定：所有复用 helper.StreamScannerHandler 的 stream handler，应在
// UpstreamStreamErrorToAPIError 检查之后、estimation/HandleFinalResponse
// 之前调用一次。示例：
//
//	if apiErr := helper.UpstreamStreamErrorToAPIError(info.StreamStatus); apiErr != nil {
//	    return nil, apiErr
//	}
//	if apiErr := helper.ClientAbortedBeforeAnyDataAPIError(c, info); apiErr != nil {
//	    return nil, apiErr
//	}
//	// ↓ 只有真的收到了数据才走 estimation 兜底
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
	if info.ReceivedResponseCount > 0 {
		// 已经收到部分上游数据（哪怕只有一帧），按截断内容做 estimation 是合理的：
		// 上游生成的部分成本真实存在，此时不豁免、走原有兜底路径。
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
