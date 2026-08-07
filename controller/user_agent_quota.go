package controller

import (
	"net/http"

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
