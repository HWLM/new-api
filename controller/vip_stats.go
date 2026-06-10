/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	// OptionKeyVipStatsAccessPassword 公开 vip-stats 页面的访问密码（明文存 options 表）
	OptionKeyVipStatsAccessPassword = "vip_stats_access_password"

	// HeaderVipStatsPassword 访问公开接口时，前端把密码塞进这个 header
	HeaderVipStatsPassword = "X-VIP-Stats-Password"
)

// checkVipStatsPassword 校验请求 header 中的密码是否匹配 options 表里配置的值。
// 配置为空时拒绝所有请求（保护性默认，避免未配置导致数据裸奔）。
func checkVipStatsPassword(c *gin.Context) bool {
	saved := model.GetOptionString(OptionKeyVipStatsAccessPassword)
	if saved == "" {
		return false
	}
	got := c.Request.Header.Get(HeaderVipStatsPassword)
	return got != "" && got == saved
}

// GetVipStatsDetail 重点客户消耗明细页接口（公开，但要求 header X-VIP-Stats-Password）
func GetVipStatsDetail(c *gin.Context) {
	if !checkVipStatsPassword(c) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "password required or incorrect",
		})
		return
	}
	resp, err := model.GetVipStatsDetail()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

type verifyVipStatsPasswordReq struct {
	Password string `json:"password"`
}

// VerifyVipStatsPassword 公开校验密码接口。
// 前端打开页面时先调这个，校验通过才把密码存到 sessionStorage 用于后续 detail 调用。
func VerifyVipStatsPassword(c *gin.Context) {
	var req verifyVipStatsPasswordReq
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	saved := model.GetOptionString(OptionKeyVipStatsAccessPassword)
	if saved == "" {
		common.ApiErrorMsg(c, "access password not configured")
		return
	}
	if req.Password == "" || req.Password != saved {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "password incorrect",
		})
		return
	}
	common.ApiSuccess(c, nil)
}

// BackfillVipDailyStats 手动回填过去 N 天的统计数据（仅管理员）。
//
// 用法：POST /api/user/vip_stats/backfill?days=7
//
// 由于业务无 VIP 历史快照，所有回填以"调用时刻的当前 VIP 客户"为口径。
func BackfillVipDailyStats(c *gin.Context) {
	days, _ := strconv.Atoi(c.Query("days"))
	if days <= 0 {
		days = 7
	}
	result, err := service.BackfillVipDailyStats(days)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, gin.H{
		"days":     days,
		"per_date": result,
	})
}
