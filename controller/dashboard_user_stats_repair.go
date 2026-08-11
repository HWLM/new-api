/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
package controller

// 「数据看板 -> 新用户统计」历史 token 口径修复接口（admin only）。
//
//   POST /api/user_stats/repair_tokens
//   body: {"start_date": "YYYY-MM-DD", "end_date": "YYYY-MM-DD"}
//
// 背景：Claude 语义请求的 logs.prompt_tokens 是净输入（不含缓存），而 OpenAI 语义
// 已含缓存。vip_daily_consumption / vip_hourly_consumption 落盘时统一用
// prompt_tokens + completion_tokens，导致 Claude 请求的缓存读取/写入 token 被漏计。
// 本接口对指定日期区间逐天重跑落盘聚合（复用 RunVipDailyStat / RunVipHourlyStat，
// 其内部已补加 Claude 缓存 token），幂等 upsert 覆盖历史数据。

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type repairTokensReq struct {
	StartDate string `json:"start_date" binding:"required"`
	EndDate   string `json:"end_date" binding:"required"`
}

type repairTokensResult struct {
	Date   string `json:"date"`
	Daily  int    `json:"daily_rows"`
	Hourly int    `json:"hourly_rows"`
}

// RepairUserStatsTokens 重算 [start_date, end_date] 区间内 vip_daily_consumption /
// vip_hourly_consumption 的 tokens 口径，修复历史数据。
func RepairUserStatsTokens(c *gin.Context) {
	var req repairTokensReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "start_date/end_date required (YYYY-MM-DD)"})
		return
	}
	loc := time.Now().Location()
	start, err := time.ParseInLocation("2006-01-02", req.StartDate, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid start_date"})
		return
	}
	end, err := time.ParseInLocation("2006-01-02", req.EndDate, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid end_date"})
		return
	}
	if end.Before(start) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_date earlier than start_date"})
		return
	}

	var results []repairTokensResult
	totalDaily, totalHourly := 0, 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		res := repairTokensResult{Date: date}
		n, err := model.RunVipDailyStat(date)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "daily repair failed at " + date + ": " + err.Error()})
			return
		}
		res.Daily = n
		totalDaily += n
		for h := 0; h < 24; h++ {
			n, err := model.RunVipHourlyStat(date, h)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "hourly repair failed at " + date + " hour " + strconv.Itoa(h) + ": " + err.Error()})
				return
			}
			res.Hourly += n
			totalHourly += n
		}
		results = append(results, res)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"start_date":        req.StartDate,
			"end_date":          req.EndDate,
			"results":           results,
			"total_daily_rows":  totalDaily,
			"total_hourly_rows": totalHourly,
		},
	})
}
