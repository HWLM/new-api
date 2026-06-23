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

// 「数据看板 -> 新用户统计」专用接口。
// 全部要求管理员权限（在路由层 middleware.AdminAuth() 拦截）。
//
// 四个接口：
//   GET /api/user_stats/cards            —— 顶部 12 张卡片（不受筛选影响）
//   GET /api/user_stats/top_users        —— 用户消耗排行 top10
//   GET /api/user_stats/recharge_trend   —— 充值折线图
//   GET /api/user_stats/channel_pie      —— 渠道客户消耗占比 top10（按 users.business_channel 分组）
//
// 关键单位约定：
//   - quota（int64）单位为 QuotaPerUnit/USD：USD = quota / QuotaPerUnit
//   - logs.recharge_input_amount（float64）单位为人民币 ¥（管理员页面录入即是 ¥）

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// userStatsCardSection 单组卡片（全用户 / 正式用户共用结构）
type userStatsCardSection struct {
	TotalUsers            int64    `json:"total_users"`              // 用户数（or 正式用户数）
	TodayActiveUsers      int64    `json:"today_active_users"`       // 今日使用数
	TotalRechargeCny      float64  `json:"total_recharge_cny"`       // 累计充值（¥）
	TotalConsumedUsd      float64  `json:"total_consumed_usd"`       // 累计消耗（$）
	TodayRechargeCny      float64  `json:"today_recharge_cny"`       // 今日充值（¥）
	TodayRechargeCnyDelta *float64 `json:"today_recharge_cny_delta"` // 较昨日百分比；nil 表示无法对比（凌晨 / 昨日为 0）
	TodayConsumedUsd      float64  `json:"today_consumed_usd"`       // 今日消耗（$）
	TodayConsumedUsdDelta *float64 `json:"today_consumed_usd_delta"`
	TotalRemainingUsd     float64  `json:"total_remaining_usd"` // 总剩余额度（$）
}

type userStatsCardsResp struct {
	All      userStatsCardSection `json:"all"`
	Official userStatsCardSection `json:"official"`
}

// GetUserStatsCards 顶部 12 张卡片。不受筛选区影响 —— 永远返回当前最新值。
func GetUserStatsCards(c *gin.Context) {
	now := time.Now()
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
	todayEnd := now.Unix()
	todayStr := now.Format("2006-01-02")
	yesterdayStr := now.AddDate(0, 0, -1).Format("2006-01-02")

	officialIds, err := model.GetOfficialUserIds()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	all, err := buildCardSection(todayStart, todayEnd, todayStr, yesterdayStr, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	official, err := buildCardSection(todayStart, todayEnd, todayStr, yesterdayStr, officialIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 正式用户人数另外覆盖（全用户数走 buildCardSection 的全量 COUNT，正式走 officialIds 长度）
	if officialIds == nil {
		official.TotalUsers = 0
	} else {
		official.TotalUsers = int64(len(officialIds))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": userStatsCardsResp{
			All:      all,
			Official: official,
		},
	})
}

// buildCardSection 构造单组卡片数据。
//
//	officialIds == nil 表示「全用户」（不做 IN 过滤）；
//	officialIds != nil 表示「正式用户」（按 user_id IN officialIds 过滤；空切片表示没有正式用户，全 0）。
func buildCardSection(todayStart, todayEnd int64, todayStr, yesterdayStr string, officialIds []int) (userStatsCardSection, error) {
	var section userStatsCardSection

	// 1. 总用户数（全用户走 users 表全量；正式用户由调用方覆盖）
	if officialIds == nil {
		var total int64
		if err := model.DB.Model(&model.User{}).Count(&total).Error; err != nil {
			return section, err
		}
		section.TotalUsers = total
	}

	// 2. 今日使用数 = 今天 type=consume 的去重 user_id
	activeUsers, err := countTodayActiveUsers(todayStart, todayEnd, officialIds)
	if err != nil {
		return section, err
	}
	section.TodayActiveUsers = activeUsers

	// 3. 总剩余额度（USD）= SUM(users.quota) / QuotaPerUnit
	totalRemaining, err := sumUserRemaining(officialIds)
	if err != nil {
		return section, err
	}
	section.TotalRemainingUsd = quotaToUSD(totalRemaining)

	// 4. 累计列：daily_summary 最新一行（截至昨天） + 今天实时
	prev, err := model.GetPrevDailySummary(todayStr) // stat_date < today 最近一条
	if err != nil {
		return section, err
	}
	todayQuota, todayRecharge, err := sumTodayConsumeRecharge(todayStart, todayEnd, officialIds)
	if err != nil {
		return section, err
	}
	var cumQuota int64
	var cumRecharge float64
	if officialIds == nil {
		cumQuota = prev.CumQuota
		cumRecharge = prev.CumRechargeAmount
	} else {
		cumQuota = prev.CumOfficialQuota
		cumRecharge = prev.CumOfficialRechargeAmount
	}
	section.TotalRechargeCny = cumRecharge + todayRecharge
	section.TotalConsumedUsd = quotaToUSD(cumQuota + todayQuota)

	// 5. 今日值 + 较昨日对比
	section.TodayRechargeCny = todayRecharge
	section.TodayConsumedUsd = quotaToUSD(todayQuota)

	// 昨日整天值：尝试从 daily_summary 拿 stat_date = yesterday 那行
	yRow, hasY, err := getDailySummaryByDate(yesterdayStr)
	if err != nil {
		return section, err
	}
	if hasY {
		var yQuota int64
		var yRecharge float64
		if officialIds == nil {
			yQuota = yRow.Quota
			yRecharge = yRow.RechargeAmount
		} else {
			yQuota = yRow.OfficialQuota
			yRecharge = yRow.OfficialRechargeAmount
		}
		section.TodayRechargeCnyDelta = computeDeltaPct(todayRecharge, yRecharge)
		section.TodayConsumedUsdDelta = computeDeltaPct(float64(todayQuota), float64(yQuota))
	}
	// 拿不到昨日数据时 delta 保持 nil → 前端显示 "--"

	return section, nil
}

func quotaToUSD(quota int64) float64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	return float64(quota) / common.QuotaPerUnit
}

// computeDeltaPct 返回 (cur - base) / base * 100；base = 0 时返回 nil（前端显示 "--"）。
func computeDeltaPct(cur, base float64) *float64 {
	if base == 0 {
		return nil
	}
	pct := (cur - base) / base * 100
	return &pct
}

// countTodayActiveUsers 今天有 type=consume 日志的去重 user_id 数。
func countTodayActiveUsers(startTs, endTs int64, userIdFilter []int) (int64, error) {
	if userIdFilter != nil && len(userIdFilter) == 0 {
		return 0, nil
	}
	tx := model.LOG_DB.Model(&model.Log{}).
		Where("type = ?", model.LogTypeConsume).
		Where("user_id > 0").
		Where("created_at >= ? AND created_at <= ?", startTs, endTs)
	if userIdFilter != nil {
		tx = tx.Where("user_id IN ?", userIdFilter)
	}
	var n int64
	err := tx.Distinct("user_id").Count(&n).Error
	return n, err
}

// sumUserRemaining 求 users.quota 总和；userIds 过滤可空。
func sumUserRemaining(userIds []int) (int64, error) {
	if userIds != nil && len(userIds) == 0 {
		return 0, nil
	}
	type row struct {
		Total int64
	}
	var r row
	tx := model.DB.Model(&model.User{})
	if userIds != nil {
		tx = tx.Where("id IN ?", userIds)
	}
	err := tx.Select("COALESCE(SUM(quota), 0) AS total").Scan(&r).Error
	return r.Total, err
}

// sumTodayConsumeRecharge 实时聚合「今天」logs 的消耗 quota + 管理员充值（¥）。
// userIds 过滤可空（nil = 全量，空切片 = 没有任何用户即全 0）。
func sumTodayConsumeRecharge(startTs, endTs int64, userIds []int) (int64, float64, error) {
	if userIds != nil && len(userIds) == 0 {
		return 0, 0, nil
	}
	// 消耗
	var quota int64
	{
		tx := model.LOG_DB.Model(&model.Log{}).
			Where("type = ?", model.LogTypeConsume).
			Where("user_id > 0").
			Where("created_at >= ? AND created_at <= ?", startTs, endTs)
		if userIds != nil {
			tx = tx.Where("user_id IN ?", userIds)
		}
		if err := tx.Select("COALESCE(SUM(quota), 0)").Scan(&quota).Error; err != nil {
			return 0, 0, err
		}
	}
	// 充值
	var recharge float64
	{
		tx := model.LOG_DB.Model(&model.Log{}).
			Where("type = ?", model.LogTypeManage).
			Where("operation_type = ?", model.OperationTypeQuota).
			Where("quota_type = ?", model.QuotaTypeRecharge).
			Where("user_id > 0").
			Where("created_at >= ? AND created_at <= ?", startTs, endTs)
		if userIds != nil {
			tx = tx.Where("user_id IN ?", userIds)
		}
		if err := tx.Select("COALESCE(SUM(recharge_input_amount), 0)").Scan(&recharge).Error; err != nil {
			return 0, 0, err
		}
	}
	return quota, recharge, nil
}

// getDailySummaryByDate 按 stat_date 精确查 daily_summary；不存在返回 (zero, false, nil)。
func getDailySummaryByDate(date string) (model.DailySummary, bool, error) {
	var rows []model.DailySummary
	err := model.DB.Model(&model.DailySummary{}).
		Where("stat_date = ?", date).
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return model.DailySummary{}, false, err
	}
	if len(rows) == 0 {
		return model.DailySummary{}, false, nil
	}
	return rows[0], true, nil
}

// =========================================================
// 筛选区入参 + 共享 helper
// =========================================================

type chartFilter struct {
	startDate string
	endDate   string
	startTs   int64
	endTs     int64
	username  string
	channels  []string // 渠道（business_channel）多选，前端逗号分隔传入
	sales     []string // 销售（display_name）多选；筛选时同时要求 business_channel 非空
	isVip     *bool
	// 筛选后的 user_id 集合；nil = 无任何 user 维度过滤
	userIds []int
}

// splitCSV 解析逗号分隔字符串为去空 trim 后的 string 切片。
func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseChartFilter 解析公共筛选参数 + 把 username/channel/is_vip 转成 user_ids（一次性查 users 表）。
//
// 入参约定：
//   - start_date / end_date：YYYY-MM-DD，闭区间。不传时默认近 7 天（含今天）。
//   - username：模糊匹配
//   - channel：business_channel 精确匹配
//   - is_vip：0/1（不传则不过滤）
//
// 返回的 chartFilter.userIds：
//   - nil 表示「无 user 维度过滤」
//   - 长度为 0 的切片表示「过滤命中 0 个用户」，调用方应直接返回空结果
func parseChartFilter(c *gin.Context) (*chartFilter, error) {
	now := time.Now()
	loc := now.Location()
	endDate := c.Query("end_date")
	startDate := c.Query("start_date")
	if endDate == "" {
		endDate = now.Format("2006-01-02")
	}
	if startDate == "" {
		startDate = now.AddDate(0, 0, -6).Format("2006-01-02") // 近 7 天含今天
	}
	startT, err := time.ParseInLocation("2006-01-02", startDate, loc)
	if err != nil {
		return nil, fmt.Errorf("invalid start_date: %w", err)
	}
	endT, err := time.ParseInLocation("2006-01-02", endDate, loc)
	if err != nil {
		return nil, fmt.Errorf("invalid end_date: %w", err)
	}
	if endT.Before(startT) {
		return nil, fmt.Errorf("end_date earlier than start_date")
	}

	f := &chartFilter{
		startDate: startDate,
		endDate:   endDate,
		startTs:   startT.Unix(),
		endTs:     endT.Add(24 * time.Hour).Add(-time.Nanosecond).Unix(),
		username:  strings.TrimSpace(c.Query("username")),
		channels:  splitCSV(c.Query("channel")),
		sales:     splitCSV(c.Query("sales")),
	}
	if v := c.Query("is_vip"); v != "" {
		b, e := strconv.ParseBool(v)
		if e == nil {
			f.isVip = &b
		}
	}

	// 四个 user 维度的过滤：username / channels / sales / is_vip。任何一个非空就需要查 users 拿 ids
	if f.username != "" || len(f.channels) > 0 || len(f.sales) > 0 || f.isVip != nil {
		tx := model.DB.Model(&model.User{})
		if f.username != "" {
			tx = tx.Where("username LIKE ?", "%"+f.username+"%")
		}
		if len(f.channels) > 0 {
			tx = tx.Where("business_channel IN ?", f.channels)
		}
		if len(f.sales) > 0 {
			// 销售：业务规则要求 business_channel 非空 + display_name IN
			tx = tx.Where("business_channel <> ''").Where("display_name IN ?", f.sales)
		}
		if f.isVip != nil {
			tx = tx.Where("is_vip_customer = ?", *f.isVip)
		}
		var ids []int
		if err := tx.Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
		if ids == nil {
			ids = []int{}
		}
		f.userIds = ids
	}
	return f, nil
}

// =========================================================
// 筛选下拉的可选值
// =========================================================

type userStatsFilterOptions struct {
	Channels []string `json:"channels"`
	Sales    []string `json:"sales"`
}

// GetUserStatsFilterOptions 返回筛选下拉数据：
//
//	channels = users.business_channel 不为空的 distinct 值
//	sales    = users.business_channel 不为空的用户的 display_name distinct 值
//
// 用 GROUP BY 去重而非 DISTINCT —— GROUP BY 跨 DB（SQLite / MySQL / PostgreSQL）行为更一致，
// 且 ORDER BY + Pluck 组合不需要担心 ORDER BY 列不在 SELECT 子句的问题。
// 过滤同时排除 NULL 和 ”（业务上等价，防御性处理）。
func GetUserStatsFilterOptions(c *gin.Context) {
	out := userStatsFilterOptions{
		Channels: []string{},
		Sales:    []string{},
	}

	if err := model.DB.Model(&model.User{}).
		Where("business_channel IS NOT NULL").
		Where("business_channel <> ''").
		Group("business_channel").
		Order("business_channel ASC").
		Pluck("business_channel", &out.Channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Model(&model.User{}).
		Where("business_channel IS NOT NULL").
		Where("business_channel <> ''").
		Where("display_name IS NOT NULL").
		Where("display_name <> ''").
		Group("display_name").
		Order("display_name ASC").
		Pluck("display_name", &out.Sales).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// =========================================================
// 用户消耗排行 top10
// =========================================================

type topUserRow struct {
	UserId      int     `json:"user_id"`
	Username    string  `json:"username"`
	ConsumedUsd float64 `json:"consumed_usd"`
}

// GetUserStatsTopUsers 按 quota 消耗排前 10 的用户。
//
// 数据源：
//   - 历史天数（不含今天）→ 读 vip_daily_consumptions
//   - 含今天 → 拼上今天实时聚合 logs 的部分
func GetUserStatsTopUsers(c *gin.Context) {
	f, err := parseChartFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if f.userIds != nil && len(f.userIds) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []topUserRow{}})
		return
	}
	perUser, err := aggregateUserConsumption(f)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rows := make([]topUserRow, 0, len(perUser))
	for uid, q := range perUser {
		rows = append(rows, topUserRow{UserId: uid, ConsumedUsd: quotaToUSD(q)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ConsumedUsd > rows[j].ConsumedUsd })
	if len(rows) > 10 {
		rows = rows[:10]
	}
	// 拿 username
	if len(rows) > 0 {
		ids := make([]int, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.UserId)
		}
		nameMap, _ := batchGetUsernames(ids)
		for i := range rows {
			rows[i].Username = nameMap[rows[i].UserId]
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

// aggregateUserConsumption 在筛选范围内按 user_id 聚合 quota 消耗。
// 历史从 vip_daily_consumptions 读，今天实时聚合 logs。
func aggregateUserConsumption(f *chartFilter) (map[int]int64, error) {
	result := make(map[int]int64)
	now := time.Now()
	todayStr := now.Format("2006-01-02")

	// 历史段：[startDate, min(endDate, yesterday)]
	histEnd := f.endDate
	if histEnd >= todayStr {
		histEnd = now.AddDate(0, 0, -1).Format("2006-01-02")
	}
	if f.startDate <= histEnd {
		tx := model.DB.Model(&model.VipDailyConsumption{}).
			Where("stat_date >= ? AND stat_date <= ?", f.startDate, histEnd)
		if f.userIds != nil {
			tx = tx.Where("user_id IN ?", f.userIds)
		}
		type row struct {
			UserId int
			Total  int64
		}
		var rows []row
		if err := tx.
			Select("user_id, COALESCE(SUM(quota), 0) AS total").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			result[r.UserId] += r.Total
		}
	}

	// 今天段：start_date <= today <= end_date 才需要拼今天
	if f.startDate <= todayStr && f.endDate >= todayStr {
		loc := now.Location()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
		todayEnd := now.Unix()
		tx := model.LOG_DB.Model(&model.Log{}).
			Where("type = ?", model.LogTypeConsume).
			Where("user_id > 0").
			Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd)
		if f.userIds != nil {
			tx = tx.Where("user_id IN ?", f.userIds)
		}
		type row struct {
			UserId int
			Total  int64
		}
		var rows []row
		if err := tx.
			Select("user_id, COALESCE(SUM(quota), 0) AS total").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			result[r.UserId] += r.Total
		}
	}
	return result, nil
}

func batchGetUsernames(userIds []int) (map[int]string, error) {
	res := make(map[int]string, len(userIds))
	if len(userIds) == 0 {
		return res, nil
	}
	type row struct {
		Id       int
		Username string
	}
	var rows []row
	if err := model.DB.Model(&model.User{}).
		Select("id, username").
		Where("id IN ?", userIds).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		res[r.Id] = r.Username
	}
	return res, nil
}

// =========================================================
// 充值折线图
// =========================================================

type rechargeTrendRow struct {
	Date        string  `json:"date"`
	RechargeCny float64 `json:"recharge_cny"`
}

// GetUserStatsRechargeTrend 按天的充值金额（¥）序列。
//
// 没有用户维度筛选时直接读 daily_summary.recharge_amount；
// 有筛选时只能实时从 logs 聚合。
func GetUserStatsRechargeTrend(c *gin.Context) {
	f, err := parseChartFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if f.userIds != nil && len(f.userIds) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": emptyDateSeries(f)})
		return
	}

	// 按天初始化结果（覆盖完整时间区间，缺失补 0）
	now := time.Now()
	loc := now.Location()
	start, _ := time.ParseInLocation("2006-01-02", f.startDate, loc)
	end, _ := time.ParseInLocation("2006-01-02", f.endDate, loc)
	dateMap := make(map[string]float64)
	dates := make([]string, 0)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		s := d.Format("2006-01-02")
		dateMap[s] = 0
		dates = append(dates, s)
	}

	todayStr := now.Format("2006-01-02")
	histEnd := f.endDate
	if histEnd >= todayStr {
		histEnd = now.AddDate(0, 0, -1).Format("2006-01-02")
	}

	// 历史段
	if f.startDate <= histEnd {
		if f.userIds == nil {
			// 无 user 过滤：直接读 daily_summary
			var rows []model.DailySummary
			if err := model.DB.Model(&model.DailySummary{}).
				Where("stat_date >= ? AND stat_date <= ?", f.startDate, histEnd).
				Find(&rows).Error; err != nil {
				common.ApiError(c, err)
				return
			}
			for _, r := range rows {
				dateMap[r.StatDate] = r.RechargeAmount
			}
		} else {
			// 有 user 过滤：实时聚合 logs
			rows, err := aggregateRechargeByDateFromLogs(f, histEnd)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			for k, v := range rows {
				dateMap[k] = v
			}
		}
	}

	// 今天段：从 logs 实时聚合
	if f.startDate <= todayStr && f.endDate >= todayStr {
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
		_, todayRecharge, err := sumTodayConsumeRecharge(todayStart, now.Unix(), f.userIds)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		dateMap[todayStr] = todayRecharge
	}

	out := make([]rechargeTrendRow, 0, len(dates))
	for _, d := range dates {
		out = append(out, rechargeTrendRow{Date: d, RechargeCny: dateMap[d]})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

func emptyDateSeries(f *chartFilter) []rechargeTrendRow {
	loc := time.Now().Location()
	start, _ := time.ParseInLocation("2006-01-02", f.startDate, loc)
	end, _ := time.ParseInLocation("2006-01-02", f.endDate, loc)
	rows := make([]rechargeTrendRow, 0)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		rows = append(rows, rechargeTrendRow{Date: d.Format("2006-01-02"), RechargeCny: 0})
	}
	return rows
}

// aggregateRechargeByDateFromLogs 从 logs 表按天聚合充值金额（有 user 过滤时用）。
// 历史范围 [startDate, histEnd]，按 created_at 的本地日期 GROUP BY。
//
// 注：跨 DB 的日期函数差异较大（MySQL FROM_UNIXTIME、PostgreSQL to_timestamp、SQLite date(...)）。
// 这里直接在内存里按时间戳分桶，避免 SQL 方言问题。
func aggregateRechargeByDateFromLogs(f *chartFilter, histEnd string) (map[string]float64, error) {
	result := make(map[string]float64)
	loc := time.Now().Location()
	end, _ := time.ParseInLocation("2006-01-02", histEnd, loc)
	histEndTs := end.Add(24 * time.Hour).Add(-time.Nanosecond).Unix()

	tx := model.LOG_DB.Model(&model.Log{}).
		Where("type = ?", model.LogTypeManage).
		Where("operation_type = ?", model.OperationTypeQuota).
		Where("quota_type = ?", model.QuotaTypeRecharge).
		Where("user_id > 0").
		Where("created_at >= ? AND created_at <= ?", f.startTs, histEndTs).
		Where("recharge_input_amount IS NOT NULL")
	if f.userIds != nil {
		tx = tx.Where("user_id IN ?", f.userIds)
	}
	type row struct {
		CreatedAt int64
		Amount    float64
	}
	var rows []row
	if err := tx.Select("created_at, COALESCE(recharge_input_amount, 0) AS amount").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		date := time.Unix(r.CreatedAt, 0).In(loc).Format("2006-01-02")
		result[date] += r.Amount
	}
	return result, nil
}

// =========================================================
// 渠道客户消耗占比 top10
// =========================================================

type channelPieRow struct {
	Channel     string  `json:"channel"`
	ConsumedUsd float64 `json:"consumed_usd"`
	Percent     float64 `json:"percent"` // 占筛选范围总消耗的百分比
}

// GetUserStatsChannelPie 按 users.business_channel 分组的消耗占比 top10。
//
// 数据流：
//  1. aggregateUserConsumption(f) 得到 map[user_id]quota（含今天）
//  2. 拉 users 的 id → business_channel 映射
//  3. 内存里按 channel 聚合，空 channel 归到「未分类」
//  4. 排序、取 top 10、算占比
func GetUserStatsChannelPie(c *gin.Context) {
	// channel 自身不作为本接口的过滤参数（输出就是按 channel 分组）
	c.Request.URL.RawQuery = stripQueryParam(c.Request.URL.RawQuery, "channel")
	f, err := parseChartFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if f.userIds != nil && len(f.userIds) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []channelPieRow{}})
		return
	}
	perUser, err := aggregateUserConsumption(f)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(perUser) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []channelPieRow{}})
		return
	}

	// 拉 user_id → business_channel
	ids := make([]int, 0, len(perUser))
	for id := range perUser {
		ids = append(ids, id)
	}
	type row struct {
		Id              int
		BusinessChannel string
	}
	var rows []row
	if err := model.DB.Model(&model.User{}).
		Select("id, business_channel").
		Where("id IN ?", ids).
		Scan(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	channelOf := make(map[int]string, len(rows))
	for _, r := range rows {
		ch := strings.TrimSpace(r.BusinessChannel)
		if ch == "" {
			ch = "未分类"
		}
		channelOf[r.Id] = ch
	}

	// 按 channel 聚合
	channelQuota := make(map[string]int64)
	for uid, q := range perUser {
		ch := channelOf[uid]
		if ch == "" {
			ch = "未分类"
		}
		channelQuota[ch] += q
	}

	// 排序 + top10
	type kv struct {
		Channel string
		Quota   int64
	}
	list := make([]kv, 0, len(channelQuota))
	var totalQuota int64
	for ch, q := range channelQuota {
		list = append(list, kv{Channel: ch, Quota: q})
		totalQuota += q
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Quota > list[j].Quota })
	if len(list) > 10 {
		list = list[:10]
	}

	out := make([]channelPieRow, 0, len(list))
	for _, x := range list {
		usd := quotaToUSD(x.Quota)
		var pct float64
		if totalQuota > 0 {
			pct = float64(x.Quota) / float64(totalQuota) * 100
		}
		out = append(out, channelPieRow{
			Channel:     x.Channel,
			ConsumedUsd: usd,
			Percent:     pct,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// stripQueryParam 从 raw query 中移除指定 key（用于 channel_pie 强行剔除 channel 入参）。
func stripQueryParam(raw, key string) string {
	if raw == "" {
		return raw
	}
	parts := strings.Split(raw, "&")
	kept := parts[:0]
	prefix := key + "="
	for _, p := range parts {
		if p == key || strings.HasPrefix(p, prefix) {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, "&")
}

// =========================================================
// 用户消耗趋势（按天 / 按小时切换 + 可选对比周期）
// =========================================================

type consumptionTrendResp struct {
	Granularity    string    `json:"granularity"`
	Buckets        []string  `json:"buckets"` // X 轴标签：day → "YYYY-MM-DD"；hour → "YYYY-MM-DD#HH"
	Values         []float64 `json:"values"`  // 与 buckets 一一对应的 USD 消耗
	HasCompare     bool      `json:"has_compare"`
	CompareBuckets []string  `json:"compare_buckets,omitempty"`
	CompareValues  []float64 `json:"compare_values,omitempty"`
	CurrentTotal   float64   `json:"current_total,omitempty"`
	CompareTotal   float64   `json:"compare_total,omitempty"`
	Diff           float64   `json:"diff,omitempty"`
	ChangeRate     float64   `json:"change_rate,omitempty"` // %，compare_total=0 时为 0
}

// GetUserStatsConsumptionTrend 用户消耗趋势折线图数据。
//
//	granularity=day  → 历史读 vip_daily_consumptions  + 今天实时聚合 logs
//	granularity=hour → 历史读 vip_hourly_consumptions + 当前小时实时聚合 logs
//
// 可选对比周期（compare_start_date / compare_end_date）：传入则同时返回对比期数据 + KPI。
// 可选小时窗口（start_hour / end_hour，0..23）：仅 granularity=hour 生效，同时限定当前 + 对比周期。
//
// 受顶部全局筛选（用户名 / 渠道 / 销售 / 是否重点客户）控制（user_ids 同时作用于两期）。
func GetUserStatsConsumptionTrend(c *gin.Context) {
	granularity := strings.ToLower(strings.TrimSpace(c.Query("granularity")))
	if granularity != "hour" {
		granularity = "day"
	}
	f, err := parseChartFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	startHour, _ := strconv.Atoi(c.DefaultQuery("start_hour", "0"))
	endHour, _ := strconv.Atoi(c.DefaultQuery("end_hour", "23"))
	if startHour < 0 || startHour > 23 {
		startHour = 0
	}
	if endHour < 0 || endHour > 23 {
		endHour = 23
	}
	if endHour < startHour {
		endHour = startHour
	}

	compareStart := strings.TrimSpace(c.Query("compare_start_date"))
	compareEnd := strings.TrimSpace(c.Query("compare_end_date"))

	resp := consumptionTrendResp{Granularity: granularity}

	// 当前周期
	buckets, values, err := computeTrendSeries(f.startDate, f.endDate, f.userIds, granularity, startHour, endHour)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	resp.Buckets = buckets
	resp.Values = values
	for _, v := range values {
		resp.CurrentTotal += v
	}

	// 对比周期（可选）
	if compareStart != "" && compareEnd != "" {
		cBuckets, cValues, err := computeTrendSeries(compareStart, compareEnd, f.userIds, granularity, startHour, endHour)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		resp.HasCompare = true
		resp.CompareBuckets = cBuckets
		resp.CompareValues = cValues
		for _, v := range cValues {
			resp.CompareTotal += v
		}
		resp.Diff = resp.CurrentTotal - resp.CompareTotal
		if resp.CompareTotal != 0 {
			resp.ChangeRate = (resp.CurrentTotal - resp.CompareTotal) / resp.CompareTotal * 100
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}

// computeTrendSeries 计算给定起止日期、用户筛选、粒度、小时窗口的趋势序列。
// userIds == nil  → 不做 user 过滤（全部用户）
// userIds 长度 0 → 没有匹配用户，返回 buckets 序列 + 全 0 values
// 小时窗口仅 granularity=hour 生效。
func computeTrendSeries(startDate, endDate string, userIds []int, granularity string, startHour, endHour int) ([]string, []float64, error) {
	loc := time.Now().Location()
	startT, err := time.ParseInLocation("2006-01-02", startDate, loc)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid start date %q: %w", startDate, err)
	}
	endT, err := time.ParseInLocation("2006-01-02", endDate, loc)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid end date %q: %w", endDate, err)
	}
	noUsers := userIds != nil && len(userIds) == 0

	if granularity == "day" {
		bucketMap := make(map[string]int64)
		buckets := make([]string, 0)
		for d := startT; !d.After(endT); d = d.AddDate(0, 0, 1) {
			s := d.Format("2006-01-02")
			bucketMap[s] = 0
			buckets = append(buckets, s)
		}
		if !noUsers {
			now := time.Now()
			todayStr := now.Format("2006-01-02")
			histEnd := endDate
			if histEnd >= todayStr {
				histEnd = now.AddDate(0, 0, -1).Format("2006-01-02")
			}
			// 历史段
			if startDate <= histEnd {
				tx := model.DB.Model(&model.VipDailyConsumption{}).
					Where("stat_date >= ? AND stat_date <= ?", startDate, histEnd)
				if userIds != nil {
					tx = tx.Where("user_id IN ?", userIds)
				}
				type row struct {
					StatDate string
					Total    int64
				}
				var rows []row
				if err := tx.
					Select("stat_date, COALESCE(SUM(quota), 0) AS total").
					Group("stat_date").
					Scan(&rows).Error; err != nil {
					return nil, nil, err
				}
				for _, r := range rows {
					bucketMap[r.StatDate] += r.Total
				}
			}
			// 今天段：实时
			if startDate <= todayStr && endDate >= todayStr {
				todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
				todayQuota, _, err := sumTodayConsumeRecharge(todayStart, now.Unix(), userIds)
				if err != nil {
					return nil, nil, err
				}
				bucketMap[todayStr] += todayQuota
			}
		}
		values := make([]float64, 0, len(buckets))
		for _, b := range buckets {
			values = append(values, quotaToUSD(bucketMap[b]))
		}
		return buckets, values, nil
	}

	// hour
	type hourKey struct {
		Date string
		Hour int
	}
	bucketMap := make(map[hourKey]int64)
	buckets := make([]string, 0)
	for d := startT; !d.After(endT); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		for h := startHour; h <= endHour; h++ {
			label := fmt.Sprintf("%s#%02d", date, h)
			buckets = append(buckets, label)
			bucketMap[hourKey{date, h}] = 0
		}
	}
	if !noUsers {
		now := time.Now()
		currentHourStart := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc)
		curDate := currentHourStart.Format("2006-01-02")
		curHour := currentHourStart.Hour()

		// 历史小时段：vip_hourly_consumptions
		tx := model.DB.Model(&model.VipHourlyConsumption{}).
			Where("stat_date >= ? AND stat_date <= ?", startDate, endDate).
			Where("stat_hour >= ? AND stat_hour <= ?", startHour, endHour).
			Where("NOT (stat_date = ? AND stat_hour >= ?)", curDate, curHour)
		if userIds != nil {
			tx = tx.Where("user_id IN ?", userIds)
		}
		type row struct {
			StatDate string
			StatHour int
			Total    int64
		}
		var rows []row
		if err := tx.
			Select("stat_date, stat_hour, COALESCE(SUM(quota), 0) AS total").
			Group("stat_date, stat_hour").
			Scan(&rows).Error; err != nil {
			return nil, nil, err
		}
		for _, r := range rows {
			bucketMap[hourKey{r.StatDate, r.StatHour}] += r.Total
		}

		// 当前小时（如果在范围内）实时聚合 logs
		if curDate >= startDate && curDate <= endDate && curHour >= startHour && curHour <= endHour {
			startTs := currentHourStart.Unix()
			endTs := now.Unix()
			tx2 := model.LOG_DB.Model(&model.Log{}).
				Where("type = ?", model.LogTypeConsume).
				Where("user_id > 0").
				Where("created_at >= ? AND created_at <= ?", startTs, endTs)
			if userIds != nil {
				tx2 = tx2.Where("user_id IN ?", userIds)
			}
			var curQuota int64
			if err := tx2.Select("COALESCE(SUM(quota), 0)").Scan(&curQuota).Error; err != nil {
				return nil, nil, err
			}
			bucketMap[hourKey{curDate, curHour}] += curQuota
		}
	}
	values := make([]float64, 0, len(buckets))
	for _, b := range buckets {
		parts := strings.Split(b, "#")
		if len(parts) != 2 {
			values = append(values, 0)
			continue
		}
		h, _ := strconv.Atoi(parts[1])
		values = append(values, quotaToUSD(bucketMap[hourKey{parts[0], h}]))
	}
	return buckets, values, nil
}
