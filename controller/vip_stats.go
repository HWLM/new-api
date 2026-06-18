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

// GetVipStatsTrend 单个 VIP 用户的"两周期对比"查询。
//
// GET /api/vip_stats/trend?user_id=&granularity=day|hour
//
//	&current_start=YYYY-MM-DD&current_end=YYYY-MM-DD
//	&compare_start=YYYY-MM-DD&compare_end=YYYY-MM-DD
//	[&current_start_hour=&current_end_hour=&compare_start_hour=&compare_end_hour=]
//
// 公开接口（与 detail 一样要求 X-VIP-Stats-Password header）。
func GetVipStatsTrend(c *gin.Context) {
	if !checkVipStatsPassword(c) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "password required or incorrect",
		})
		return
	}
	userId, _ := strconv.Atoi(c.Query("user_id"))
	if userId <= 0 {
		common.ApiErrorMsg(c, "user_id required")
		return
	}
	granularity := c.Query("granularity")
	if granularity == "" {
		granularity = "day"
	}
	currStart := c.Query("current_start")
	currEnd := c.Query("current_end")
	compStart := c.Query("compare_start")
	compEnd := c.Query("compare_end")
	if currStart == "" || currEnd == "" || compStart == "" || compEnd == "" {
		common.ApiErrorMsg(c, "current_start/current_end/compare_start/compare_end required")
		return
	}
	// 小时范围（仅 granularity=hour 用；day 时建议传 0/23）
	currStartHour, _ := strconv.Atoi(c.DefaultQuery("current_start_hour", "0"))
	currEndHour, _ := strconv.Atoi(c.DefaultQuery("current_end_hour", "23"))
	compStartHour, _ := strconv.Atoi(c.DefaultQuery("compare_start_hour", "0"))
	compEndHour, _ := strconv.Atoi(c.DefaultQuery("compare_end_hour", "23"))

	currQ := model.TrendQuery{
		UserId:      userId,
		Granularity: granularity,
		StartDate:   currStart,
		EndDate:     currEnd,
		StartHour:   currStartHour,
		EndHour:     currEndHour,
	}
	compQ := model.TrendQuery{
		UserId:      userId,
		Granularity: granularity,
		StartDate:   compStart,
		EndDate:     compEnd,
		StartHour:   compStartHour,
		EndHour:     compEndHour,
	}
	resp, err := model.GetVipStatsTrend(currQ, compQ)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, resp)
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

// BackfillVipHourlyStats 手动回填指定日期区间的小时统计数据（仅管理员）。
// 写入表：vip_hourly_consumption。
//
// 用法：POST /api/user/vip_stats/backfill_hourly?start=YYYY-MM-DD&end=YYYY-MM-DD
//
// 闭区间，每天回填 0~23 共 24 个小时桶；最多 90 天。
// 同 daily 接口，所有回填以"调用时刻的当前 VIP 客户"为口径。
func BackfillVipHourlyStats(c *gin.Context) {
	start := c.Query("start")
	end := c.Query("end")
	if start == "" || end == "" {
		common.ApiErrorMsg(c, "start and end (YYYY-MM-DD) required")
		return
	}
	result, err := service.BackfillVipHourlyStats(start, end)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, gin.H{
		"start":      start,
		"end":        end,
		"per_bucket": result,
	})
}
