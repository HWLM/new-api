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
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// requireBusinessAccount 校验调用者是商务账号（business_channel 非空）。
// 用于"邀请用户统计"中图表 + 表格接口的权限闸门。
//
// 卡片接口允许所有非 admin 用户调用（普通用户也能看 4 张卡片）。
func requireBusinessAccount(c *gin.Context) bool {
	id := c.GetInt("id")
	if id == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "unauthorized",
		})
		return false
	}
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "user not found",
		})
		return false
	}
	if user.BusinessChannel == "" {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "business account only",
		})
		return false
	}
	return true
}

// GetInviterStatCards 顶部 4 张卡片（普通用户均可调用）
func GetInviterStatCards(c *gin.Context) {
	myId := c.GetInt("id")
	if myId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "unauthorized"})
		return
	}
	cards, err := model.GetInviterStatCards(myId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, cards)
}

// GetInviterCharts 3 张图表（仅商务账号）
func GetInviterCharts(c *gin.Context) {
	if !requireBusinessAccount(c) {
		return
	}
	myId := c.GetInt("id")
	startTs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	resp, err := model.GetInviterCharts(myId, startTs, endTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

// GetInviterSummary 汇总表格（仅商务账号）
//
// Query: last_consumed_start, last_consumed_end, remaining_op, remaining_value,
// username, sort_by, sort_order
func GetInviterSummary(c *gin.Context) {
	if !requireBusinessAccount(c) {
		return
	}
	myId := c.GetInt("id")
	startTs, _ := strconv.ParseInt(c.Query("last_consumed_start"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("last_consumed_end"), 10, 64)
	remainingValue, _ := strconv.ParseInt(c.Query("remaining_value"), 10, 64)
	filter := model.InviterSummaryFilter{
		LastConsumedStart: startTs,
		LastConsumedEnd:   endTs,
		RemainingOp:       c.Query("remaining_op"),
		RemainingValue:    remainingValue,
		UsernameKeyword:   c.Query("username"),
		SortBy:            c.Query("sort_by"),
		SortOrder:         c.Query("sort_order"),
	}
	rows, err := model.GetInviterSummary(myId, filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

// GetInviterDaily 按天表格（仅商务账号）
//
// Query: start_timestamp, end_timestamp, username, sort_by, sort_order
func GetInviterDaily(c *gin.Context) {
	if !requireBusinessAccount(c) {
		return
	}
	myId := c.GetInt("id")
	startTs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	filter := model.InviterDailyFilter{
		StartTs:         startTs,
		EndTs:           endTs,
		UsernameKeyword: c.Query("username"),
		SortBy:          c.Query("sort_by"),
		SortOrder:       c.Query("sort_order"),
	}
	rows, err := model.GetInviterDaily(myId, filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}
