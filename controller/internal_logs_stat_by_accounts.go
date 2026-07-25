package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// InternalStatByAccountsRequest 请求体。
//
// account_ids 数量不做上限限制,由 model.SumUsedQuotaByAccountIDs 内部分批查库(默认每批 1000),
// 调用方(sub2api ROI 统计)可以一次把某 platform 全量 account_id 传过来。
//
// start_timestamp / end_timestamp 均为 Unix 秒;传 0 视为不加上/下界,与其它 log/stat 端点一致。
type InternalStatByAccountsRequest struct {
	AccountIDs     []int64 `json:"account_ids"`
	StartTimestamp int64   `json:"start_timestamp"`
	EndTimestamp   int64   `json:"end_timestamp"`
}

// InternalStatByAccountsResponse 响应 data 部分。
//   - total_quota  所有 account_id 的合计 quota
//   - per_account  按 account_id 分组的 quota,只包含 quota > 0 的账号(HAVING 过滤)
type InternalStatByAccountsResponse struct {
	TotalQuota int64                     `json:"total_quota"`
	PerAccount []model.AccountQuotaEntry `json:"per_account"`
}

// GetInternalLogsStatByAccounts 处理 POST /internal/logs/stat-by-accounts。
//
// 服务于 sub2api 的 ROI 统计:sub2api 每天 cron 把 platform 下所有 account_id + 时间窗传过来,
// 拿回该窗口内每个账号的 quota(以及总和)作为「当日收入」入库。
//
// 内部无鉴权,依赖 SetInternalRouter 的部署约束(只挂内网),与其它 /internal/logs/* 端点一致。
func GetInternalLogsStatByAccounts(c *gin.Context) {
	var req InternalStatByAccountsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// 空 account_ids 直接短路,避免走 SQL、保持与 SumUsedQuotaByAccountIDs 的空集语义一致。
	if len(req.AccountIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": InternalStatByAccountsResponse{
				TotalQuota: 0,
				PerAccount: []model.AccountQuotaEntry{},
			},
		})
		return
	}

	total, perAccount, err := model.SumUsedQuotaByAccountIDs(
		c.Request.Context(), req.AccountIDs, req.StartTimestamp, req.EndTimestamp)
	if err != nil {
		common.SysError("internal stat by accounts failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stat failed"})
		return
	}
	if perAccount == nil {
		perAccount = []model.AccountQuotaEntry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": InternalStatByAccountsResponse{
			TotalQuota: total,
			PerAccount: perAccount,
		},
	})
}
