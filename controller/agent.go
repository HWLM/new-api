// Copyright (C) 2023-2026 QuantumNous
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// For commercial licensing, please contact support@quantumnous.com

package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetAgentInfo 通过代理身份 apikey 查询代理用户的当前状态与账户概览。
// 白名单接口：由 middleware.TokenAuth 完成认证，此处仅校验该 token 必须是代理 apikey。
// 返回：代理身份、用户信息、当前余额（quota）、总消耗额度（used_quota）、总请求数（request_count）。
func GetAgentInfo(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "missing token context",
		})
		return
	}
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !token.IsAgentToken {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "该 apikey 不是代理身份 apikey，无权访问",
		})
		return
	}
	user, err := model.GetUserById(token.UserId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !user.IsAgent {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "对应用户已不是代理身份",
		})
		return
	}

	common.ApiSuccess(c, gin.H{
		"is_agent":      true,
		"user_id":       user.Id,
		"username":      user.Username,
		"display_name":  user.DisplayName,
		"email":         user.Email,
		"group":         user.Group,
		"status":        user.Status,
		"quota":         user.Quota,
		"used_quota":    user.UsedQuota,
		"request_count": user.RequestCount,
	})
}
