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
		}
	}
}
