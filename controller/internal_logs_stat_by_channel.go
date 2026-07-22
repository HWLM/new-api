package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// InternalStatByChannelRequest 请求体。
//
// channel_id 必填且 > 0 —— 传 0 会绕过 model.SumChannelQuota 内部的 WHERE 过滤(GORM
// 语义上"0 值不生效"是常见坑),导致全库聚合泄漏。这里显式拒绝。
//
// type 必填且 > 0 —— 拒绝 LogTypeUnknown(=0),避免误传出现"什么也过滤不到"的语义模糊。
// 常见值:2 = LogTypeConsume(消耗),6 = LogTypeRefund(退款)。
//
// start_timestamp / end_timestamp 均为 Unix 秒;传 0 视为不加上/下界,与其它
// /internal/logs/* 端点保持一致。
type InternalStatByChannelRequest struct {
	ChannelID      int   `json:"channel_id"`
	LogType        int   `json:"type"`
	StartTimestamp int64 `json:"start_timestamp"`
	EndTimestamp   int64 `json:"end_timestamp"`
}

// InternalStatByChannelResponse 响应 data 部分,只返回聚合 quota。
// sub2api 的 ROI 上游分支会对同一 channel 分别用 type=2 / type=6 调 2 次,
// 由调用方相减得到「净收入」= consume - refund。
type InternalStatByChannelResponse struct {
	Quota int64 `json:"quota"`
}

// GetInternalLogsStatByChannel 处理 POST /internal/logs/stat-by-channel。
//
// 服务于 sub2api ROI 上游分支:按 channel_id + type + 时间窗聚合 logs.quota。
// 与老对外接口 /api/log/stat(controller.GetLogsStat)不同 —— 那个接口的 type 参数
// 在 model.SumUsedQuota 里是死代码(硬编码 WHERE type = LogTypeConsume),
// 本接口调 model.SumChannelQuota,type 参数真正生效,才能分别拉 consume / refund。
//
// 内部无鉴权,依赖 SetInternalRouter 的部署约束(只挂内网),与其它 /internal/logs/* 端点一致。
func GetInternalLogsStatByChannel(c *gin.Context) {
	var req InternalStatByChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	if req.ChannelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_id must be positive"})
		return
	}
	if req.LogType <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be positive"})
		return
	}

	quota, err := model.SumChannelQuota(
		c.Request.Context(), req.ChannelID, req.LogType, req.StartTimestamp, req.EndTimestamp)
	if err != nil {
		common.SysError("internal stat by channel failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stat failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    InternalStatByChannelResponse{Quota: quota},
	})
}
