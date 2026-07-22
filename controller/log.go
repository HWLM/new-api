package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := model.GetAllLogs(logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId, upstreamRequestId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	respondUserLogs(c, pageInfo, logs)
	return
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	isAdmin := c.GetInt("role") >= common.RoleAdminUser
	// 管理类日志（type=3）属于管理员审计数据，仅管理员可通过 self 接口查看；
	// 普通用户即使在 URL 上手动拼接 type=3，也直接返回空结果，避免泄露管理员操作记录。
	if logType == model.LogTypeManage && !isAdmin {
		pageInfo.SetTotal(0)
		pageInfo.SetItems([]*model.Log{})
		common.ApiSuccess(c, pageInfo)
		return
	}
	userIds := []int{userId}
	if !isAdmin {
		var err error
		userIds, err = model.GetUserLogScopeIDs(userId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := model.GetUserLogs(userIds, logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId, upstreamRequestId, !isAdmin)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	respondUserLogs(c, pageInfo, logs)
	return
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无效的令牌",
		})
		return
	}
	logs, err := model.GetLogByTokenId(tokenId)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	stat, err := model.SumUsedQuota(nil, logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, "")
	data := gin.H{
		"quota":      stat.Quota,
		"sub_quota":  stat.SubQuota,
		"sub_tokens": stat.SubTokens,
		"rpm":        stat.Rpm,
		"tpm":        stat.Tpm,
	}
	if active, rate := settlementUSDRate(c); active {
		convertMoneyKeys(data, rate, "quota", "sub_quota")
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
	return
}

func GetLogsSelfStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	isAdmin := c.GetInt("role") >= common.RoleAdminUser
	// 与 GetUserLogs 保持一致：管理类日志统计仅向管理员开放。
	if logType == model.LogTypeManage && !isAdmin {
		c.JSON(200, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"quota":      0,
				"sub_quota":  0,
				"sub_tokens": 0,
				"rpm":        0,
				"tpm":        0,
			},
		})
		return
	}
	userId := c.GetInt("id")
	userIds := []int{userId}
	if !isAdmin {
		var err error
		userIds, err = model.GetUserLogScopeIDs(userId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	quotaNum, err := model.SumUsedQuota(userIds, logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, tokenName)
	data := gin.H{
		"quota":      quotaNum.Quota,
		"sub_quota":  quotaNum.SubQuota,
		"sub_tokens": quotaNum.SubTokens,
		"rpm":        quotaNum.Rpm,
		"tpm":        quotaNum.Tpm,
		//"token": tokenNum,
	}
	if active, rate := settlementUSDRate(c); active {
		convertMoneyKeys(data, rate, "quota", "sub_quota")
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
	return
}

// DeleteHistoryLogs is the legacy synchronous log cleanup endpoint (DELETE /api/log/).
// It deletes directly instead of going through the async system task. It is kept only
// for the classic frontend; the default frontend uses POST /api/system-task/log-cleanup.
// TODO: remove this handler (and its route) once the classic frontend is removed.
func DeleteHistoryLogs(c *gin.Context) {
	targetTimestamp, _ := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if targetTimestamp == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}
	count, err := model.DeleteOldLog(c.Request.Context(), targetTimestamp, 100)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
	return
}
