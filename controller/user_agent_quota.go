package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetAgentQuotaInfo returns the quota, used_quota and request_count for the
// user that owns the API key used for authentication. This endpoint is
// designed to be called by downstream systems (e.g. niu-api) using a relay
// API key (sk-xxx) that was issued for an agent user in this system.
//
// Route: GET /api/user/agent/quota-info
// Auth:  TokenAuth (sk-xxx / tokens.key)
func GetAgentQuotaInfo(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid user context",
		})
		return
	}

	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota":         user.Quota,
			"used_quota":    user.UsedQuota,
			"request_count": user.RequestCount,
		},
	})
}

// GetAgentUserQuotaDates returns the quota_data time-series for the user that
// owns the API key used for authentication. Downstream systems call this to
// render "last 24h usage" style widgets on their side without duplicating
// the underlying event stream. Mirrors the shape returned by /api/data/self.
//
// Route: GET /api/user/agent/quota-dates
// Auth:  TokenAuth (sk-xxx / tokens.key)
// Query: start_timestamp, end_timestamp (unix seconds)
func GetAgentUserQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid user context",
		})
		return
	}

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	if endTimestamp-startTimestamp > 2592000 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "时间跨度不能超过 1 个月",
		})
		return
	}

	dates, err := model.GetQuotaDataByUserId(userId, startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
}
