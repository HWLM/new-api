package router

import (
	"github.com/QuantumNous/new-api/controller"

	"github.com/gin-gonic/gin"
)

// SetInternalRouter 注册 /internal/* 路由。
//
// 这些端点面向**内网调用方**（例如 sub2api 的对账任务）暴露，故意不带
// 用户鉴权 / token 校验：
//   - 只做只读查询、不做任何写入或状态变更；
//   - 只暴露对账最少字段（type / quota / refund_reason），不返回 prompt/ip/username；
//   - 单次请求最多 500 个 request_id，避免超大响应体。
//
// 部署时应把 /internal/* 绑定在**内网监听 / K8s ClusterIP / 反向代理白名单** 之内，
// 禁止公网直连。若未来需要公开，应在此处加 IP 白名单 / 内部 JWT / mTLS。
func SetInternalRouter(router *gin.Engine) {
	internalRouter := router.Group("/internal")
	{
		logsGroup := internalRouter.Group("/logs")
		{
			// POST /internal/logs/refund-status
			// Body: {"request_ids": ["...", "..."]}
			// 返回每个 request_id 对应的 log_type / quota / refund_reason，供下游做退费对账。
			logsGroup.POST("/refund-status", controller.GetInternalRefundStatus)

			// POST /internal/logs/patch-account
			// Body: {"items": [{"request_id": "...", "account_id": 123}, ...]}
			// 由 sub2api 的 push_account_to_newapi 任务批量回填 logs.account_id。
			// 只做写入（UPDATE），不涉及任何计费/额度变动。ClickHouse 日志库不支持。
			logsGroup.POST("/patch-account", controller.PatchInternalLogAccountIDs)

			// POST /internal/logs/stat-by-accounts
			// Body: {"account_ids": [...], "start_timestamp": ..., "end_timestamp": ...}
			// 供 sub2api 的 ROI 统计使用：按 account_id 精确聚合 [start, end] 窗口内的 quota，
			// 返回 total_quota + per_account 明细。account_ids 数量无外部上限，
			// controller/model 内部自动分批（每批 1000）查库并合并。
			logsGroup.POST("/stat-by-accounts", controller.GetInternalLogsStatByAccounts)

			// POST /internal/logs/stat-by-channel
			// Body: {"channel_id": ..., "type": ..., "start_timestamp": ..., "end_timestamp": ...}
			// 供 sub2api 的 ROI 上游分支使用：按 channel_id + type 精确聚合 quota。
			// 与老对外接口 /api/log/stat 不同,本接口的 type 参数真正生效,允许调用方
			// 分别拉 consume(type=2)和 refund(type=6),做「净收入 = 消耗 - 退款」。
			logsGroup.POST("/stat-by-channel", controller.GetInternalLogsStatByChannel)
		}
	}
}
