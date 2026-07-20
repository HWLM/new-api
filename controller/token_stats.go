package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// dayStartTimestamp 返回 t 当天 0 点的 Unix 秒级时间戳（本地时区）
func dayStartTimestamp(t time.Time) int64 {
	t = t.Local()
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).Unix()
}

// GetTokenStatsSummary 4 张汇总卡片：启用密钥数、今日消耗、昨日消耗（用于环比）、近30天累计、剩余总额
func GetTokenStatsSummary(c *gin.Context) {
	userId := c.GetInt("id")
	now := time.Now()
	todayStart := dayStartTimestamp(now)
	yesterdayStart := todayStart - 86400
	last30Start := todayStart - 30*86400
	tomorrowStart := todayStart + 86400

	agg, err := model.GetUserTokenAggregate(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	todayQuota, err := model.GetUserTokenQuotaSum(userId, todayStart, tomorrowStart)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	yesterdayQuota, err := model.GetUserTokenQuotaSum(userId, yesterdayStart, todayStart)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	last30Quota, err := model.GetUserTokenQuotaSum(userId, last30Start, tomorrowStart)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	data := gin.H{
		"enabled_count":   agg.EnabledCount,
		"remain_total":    agg.RemainTotal,
		"today_quota":     todayQuota,
		"yesterday_quota": yesterdayQuota,
		"last30_quota":    last30Quota,
	}
	if active, rate := settlementUSDRate(c); active {
		convertMoneyKeys(data, rate, "remain_total", "today_quota", "yesterday_quota", "last30_quota")
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

// GetTokenStatsTop 今日 Top N（默认 10）
func GetTokenStatsTop(c *gin.Context) {
	userId := c.GetInt("id")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	now := time.Now()
	todayStart := dayStartTimestamp(now)
	tomorrowStart := todayStart + 86400

	items, err := model.GetUserTokenTop(userId, todayStart, tomorrowStart, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var data interface{} = items
	if active, rate := settlementUSDRate(c); active {
		data = convertStructsForSettlement(items, rate, "quota")
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

// GetTokenStatsExhausting 即将耗尽密钥分页
func GetTokenStatsExhausting(c *gin.Context) {
	userId := c.GetInt("id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	items, total, err := model.ListExhaustingByUser(userId, page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var itemsData interface{} = items
	if active, rate := settlementUSDRate(c); active {
		itemsData = convertStructsForSettlement(items, rate, "used_quota", "remain_quota")
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items": itemsData,
			"total": total,
		},
	})
}

// GetTokenStatsDaily 每日明细
func GetTokenStatsDaily(c *gin.Context) {
	userId := c.GetInt("id")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	sortBy := c.Query("sort_by")
	sortOrder := c.Query("sort_order")

	startDate, _ := strconv.ParseInt(c.Query("start_date"), 10, 64)
	endDate, _ := strconv.ParseInt(c.Query("end_date"), 10, 64)
	// 前端传的 end_date 是用户选定「截止日」的 0 点（本地时区）。
	// 为了让该日的数据落在半开区间 [start, end) 内，统一 +86400。
	if endDate > 0 {
		endDate += 86400
	}

	filters := model.TokenDailyDetailFilters{
		StartDate: startDate,
		EndDate:   endDate,
		GroupName: c.Query("group"),
		TokenName: c.Query("token_name"),
	}
	if statusStr := c.Query("status"); statusStr != "" {
		if s, err := strconv.Atoi(statusStr); err == nil {
			filters.Status = &s
		}
	}

	items, total, err := model.GetUserTokenDailyDetail(userId, filters, page, pageSize, sortBy, sortOrder)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var itemsData interface{} = items
	if active, rate := settlementUSDRate(c); active {
		itemsData = convertStructsForSettlement(items, rate, "daily_quota", "cumulative_quota")
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"items":     itemsData,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}
