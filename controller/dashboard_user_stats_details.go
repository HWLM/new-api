/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

For commercial licensing, please contact support@quantumnous.com
*/
package controller

// 「数据看板 -> 新用户统计」明细数据表 + 单用户趋势对比接口（admin only）。
//
//   GET /api/user_stats/details     —— 明细数据表（带分页、独立筛选）
//   GET /api/user_stats/user_trend  —— 单用户两周期趋势对比（admin 版本，无密码门）
//
// 单位约定：
//   total_consumed_usd / remaining_usd / quota → USD = quota / QuotaPerUnit
//   total_recharge_cny → 人民币 ¥（管理员页面录入即是 ¥）
//
// 字段语义提示：
//   表头「归属渠道」「归属销售」对应 users.inviter 的 business_channel / display_name；
//   子筛选区的「渠道」「销售」也按此语义筛 —— 即筛"邀请人是某销售/属某渠道"，
//   而非筛用户本人的 business_channel。

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

// =========================================================
// 明细数据表
// =========================================================

type detailsFilter struct {
	username            string   // 模糊匹配 username 或 display_name
	channels            []string // 归属渠道（inviter.business_channel）IN
	sales               []string // 归属销售（inviter.display_name）IN
	userGroups          []string // users.group IN
	isVip               *bool
	lastConsumeDateFrom string // YYYY-MM-DD, 用户最后一次消耗日期 >= 这一天
	page                int
	pageSize            int
	sortBy              string // username / display_name / last_consume / consumed / recharge / remaining / created
	sortDir             string // asc / desc
}

type detailsRow struct {
	UserId             int     `json:"user_id"`
	Username           string  `json:"username"`
	DisplayName        string  `json:"display_name"`
	IsVipCustomer      bool    `json:"is_vip_customer"`
	IsOfficial         bool    `json:"is_official"` // group ∈ OfficialUserGroups
	BusinessChannel    string  `json:"business_channel"`
	InviterDisplayName string  `json:"inviter_display_name"`
	UserGroup          string  `json:"user_group"`
	LastConsumeAt      int64   `json:"last_consume_at"` // unix 秒；0 表示无
	LastRechargeAt     int64   `json:"last_recharge_at"`
	TotalRequests      int64   `json:"total_requests"`
	TotalTokens        int64   `json:"total_tokens"`
	TotalRechargeCny   float64 `json:"total_recharge_cny"`
	TotalConsumedUsd   float64 `json:"total_consumed_usd"`
	RemainingUsd       float64 `json:"remaining_usd"`
}

type detailsResp struct {
	Rows     []detailsRow `json:"rows"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

func parseDetailsFilter(c *gin.Context) *detailsFilter {
	f := &detailsFilter{
		username:            strings.TrimSpace(c.Query("username")),
		channels:            splitCSV(c.Query("channel")),
		sales:               splitCSV(c.Query("sales")),
		userGroups:          splitCSV(c.Query("user_group")),
		lastConsumeDateFrom: strings.TrimSpace(c.Query("last_consume_date_from")),
		sortBy:              strings.TrimSpace(c.Query("sort_by")),
		sortDir:             strings.ToLower(strings.TrimSpace(c.Query("sort_dir"))),
	}
	if v := c.Query("is_vip"); v != "" {
		b, e := strconv.ParseBool(v)
		if e == nil {
			f.isVip = &b
		}
	}
	if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 0 {
		f.page = p
	} else {
		f.page = 1
	}
	if ps, err := strconv.Atoi(c.Query("page_size")); err == nil && ps > 0 && ps <= 200 {
		f.pageSize = ps
	} else {
		f.pageSize = 20
	}
	if f.sortDir != "asc" && f.sortDir != "desc" {
		f.sortDir = "desc"
	}
	return f
}

// detailsSortColumn 把 sortBy 映射到 users 表 / 聚合后字段。
// 对于 logs 聚合字段（last_consume / consumed / recharge / requests / tokens），无法在 users 表层 ORDER BY，
// 需要先聚合再排序 —— 这里仅返回 users 表能直接 ORDER BY 的列；其余在内存中排序。
func detailsSortColumnUserTable(sortBy string) string {
	switch sortBy {
	case "username":
		return "username"
	case "display_name":
		return "display_name"
	case "remaining":
		return "quota"
	case "created":
		return "created_at"
	case "":
		return "id"
	}
	return "" // 聚合字段，需要内存排序
}

// GetUserStatsDetails 明细数据列表。
//
// 数据流：
//  1. 用 users 表先过滤（username/inviter/user_group/is_vip）→ 候选 user_id
//  2. 如果有 last_consume_date_from → logs 子查询过滤
//  3. count + 分页（users 表层能 ORDER BY 的字段直接走 SQL；聚合字段走内存排序）
//  4. 当前页 user_ids 反查 logs 聚合拿展示字段
func GetUserStatsDetails(c *gin.Context) {
	f := parseDetailsFilter(c)

	// 1. 「归属」过滤需要先查出 inviter 候选 ids
	var inviterIds []int
	needInviterFilter := len(f.channels) > 0 || len(f.sales) > 0
	if needInviterFilter {
		tx := model.DB.Model(&model.User{}).Where("business_channel <> ''")
		if len(f.channels) > 0 {
			tx = tx.Where("business_channel IN ?", f.channels)
		}
		if len(f.sales) > 0 {
			tx = tx.Where("display_name IN ?", f.sales)
		}
		if err := tx.Pluck("id", &inviterIds).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		if len(inviterIds) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data":    detailsResp{Rows: []detailsRow{}, Total: 0, Page: f.page, PageSize: f.pageSize},
			})
			return
		}
	}

	// 2. 拼 users WHERE
	userTx := model.DB.Model(&model.User{})
	if f.username != "" {
		userTx = userTx.Where("username LIKE ? OR display_name LIKE ?",
			"%"+f.username+"%", "%"+f.username+"%")
	}
	if needInviterFilter {
		userTx = userTx.Where("inviter_id IN ?", inviterIds)
	}
	if len(f.userGroups) > 0 {
		userTx = userTx.Where(commonUserGroupCol()+" IN ?", f.userGroups)
	}
	if f.isVip != nil {
		userTx = userTx.Where("is_vip_customer = ?", *f.isVip)
	}

	// 3. last_consume_date_from：先查 logs 拿到 last_consume >= X 的 user_ids
	if f.lastConsumeDateFrom != "" {
		loc := time.Now().Location()
		t, err := time.ParseInLocation("2006-01-02", f.lastConsumeDateFrom, loc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid last_consume_date_from"})
			return
		}
		startTs := t.Unix()
		var matchingIds []int
		if err := model.LOG_DB.Model(&model.Log{}).
			Where("type = ?", model.LogTypeConsume).
			Where("user_id > 0").
			Group("user_id").
			Having("MAX(created_at) >= ?", startTs).
			Pluck("user_id", &matchingIds).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		if len(matchingIds) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data":    detailsResp{Rows: []detailsRow{}, Total: 0, Page: f.page, PageSize: f.pageSize},
			})
			return
		}
		userTx = userTx.Where("id IN ?", matchingIds)
	}

	// 4. count total
	var total int64
	if err := userTx.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	// 5. 用户表层能 ORDER BY 的字段直接 SQL 分页；聚合字段走"先全量取候选 ids 再内存排序"
	sortCol := detailsSortColumnUserTable(f.sortBy)
	type userBrief struct {
		Id              int
		Username        string
		DisplayName     string
		IsVipCustomer   bool
		BusinessChannel string
		InviterId       int
		Quota           int64
		UserGroup       string
	}
	var users []userBrief
	userSelect := "id, username, display_name, is_vip_customer, business_channel, inviter_id, quota, " +
		commonUserGroupCol() + " AS user_group"
	if sortCol != "" {
		// 直接 SQL 分页
		offset := (f.page - 1) * f.pageSize
		if err := userTx.
			Select(userSelect).
			Order(sortCol + " " + f.sortDir).
			Limit(f.pageSize).
			Offset(offset).
			Scan(&users).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		// 聚合字段排序：先取出所有候选用户的 id（仅基本字段），再聚合排序，最后分页
		if err := userTx.
			Select(userSelect).
			Scan(&users).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if len(users) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    detailsResp{Rows: []detailsRow{}, Total: total, Page: f.page, PageSize: f.pageSize},
		})
		return
	}

	// 6. logs 聚合：consume 部分（COUNT/SUM quota/SUM tokens/MAX created_at）
	userIds := make([]int, 0, len(users))
	for _, u := range users {
		userIds = append(userIds, u.Id)
	}
	type consumeAgg struct {
		UserId         int
		TotalQuota     int64
		RequestCount   int64
		TotalTokens    int64
		LastConsumedAt int64
	}
	// quota 走净口径（type=LogTypeConsume 计正、type=LogTypeRefund 计负）；
	// request_count / tokens / last_consumed_at 仅算 type=LogTypeConsume
	consumeMap := map[int]consumeAgg{}
	{
		var rows []consumeAgg
		if err := model.LOG_DB.Model(&model.Log{}).
			Where("type IN ?", model.NetQuotaSumTypes()).
			Where("user_id IN ?", userIds).
			Select(fmt.Sprintf(
				"user_id, %s AS total_quota, "+
					"COUNT(CASE WHEN type = %d THEN 1 END) AS request_count, "+
					"COALESCE(SUM(CASE WHEN type = %d THEN prompt_tokens + completion_tokens ELSE 0 END), 0) AS total_tokens, "+
					"COALESCE(MAX(CASE WHEN type = %d THEN created_at END), 0) AS last_consumed_at",
				model.NetQuotaSumExpr(), model.LogTypeConsume, model.LogTypeConsume, model.LogTypeConsume)).
			Group("user_id").
			Scan(&rows).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		for _, r := range rows {
			consumeMap[r.UserId] = r
		}
	}

	// 7. logs 聚合：recharge 部分（SUM recharge_input_amount / MAX created_at）
	type rechargeAgg struct {
		UserId         int
		TotalRecharge  float64
		LastRechargeAt int64
	}
	rechargeMap := map[int]rechargeAgg{}
	{
		var rows []rechargeAgg
		if err := model.LOG_DB.Model(&model.Log{}).
			Where("type = ?", model.LogTypeManage).
			Where("operation_type = ?", model.OperationTypeQuota).
			Where("quota_type = ?", model.QuotaTypeRecharge).
			Where("user_id IN ?", userIds).
			Select("user_id, COALESCE(SUM(recharge_input_amount), 0) AS total_recharge, " +
				"COALESCE(MAX(created_at), 0) AS last_recharge_at").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		for _, r := range rows {
			rechargeMap[r.UserId] = r
		}
	}

	// 8. inviter display_name / business_channel 反查
	inviterDisplayMap := map[int]string{}
	inviterChannelMap := map[int]string{}
	{
		inviterIdSet := map[int]struct{}{}
		for _, u := range users {
			if u.InviterId > 0 {
				inviterIdSet[u.InviterId] = struct{}{}
			}
		}
		if len(inviterIdSet) > 0 {
			ids := make([]int, 0, len(inviterIdSet))
			for id := range inviterIdSet {
				ids = append(ids, id)
			}
			type row struct {
				Id              int
				DisplayName     string
				BusinessChannel string
			}
			var inv []row
			if err := model.DB.Model(&model.User{}).
				Select("id, display_name, business_channel").
				Where("id IN ?", ids).
				Scan(&inv).Error; err != nil {
				common.ApiError(c, err)
				return
			}
			for _, r := range inv {
				// 「归属销售」语义：邀请人 business_channel 非空才算销售；
				// 否则可能是普通客户邀请普通客户，display_name 不参与展示。
				inviterChannelMap[r.Id] = r.BusinessChannel
				if r.BusinessChannel != "" {
					inviterDisplayMap[r.Id] = r.DisplayName
				}
			}
		}
	}

	// 9. 正式用户判定：使用现有 GetOfficialUserIds，返回所有正式用户 id 集合
	officialSet := map[int]struct{}{}
	{
		ids, err := model.GetOfficialUserIds()
		if err == nil {
			for _, id := range ids {
				officialSet[id] = struct{}{}
			}
		}
	}

	// 10. 拼装 detailsRow
	rows := make([]detailsRow, 0, len(users))
	for _, u := range users {
		ca := consumeMap[u.Id]
		ra := rechargeMap[u.Id]
		_, isOfficial := officialSet[u.Id]
		rows = append(rows, detailsRow{
			UserId:             u.Id,
			Username:           u.Username,
			DisplayName:        u.DisplayName,
			IsVipCustomer:      u.IsVipCustomer,
			IsOfficial:         isOfficial,
			BusinessChannel:    inviterChannelMap[u.InviterId],
			InviterDisplayName: inviterDisplayMap[u.InviterId],
			UserGroup:          u.UserGroup,
			LastConsumeAt:      ca.LastConsumedAt,
			LastRechargeAt:     ra.LastRechargeAt,
			TotalRequests:      ca.RequestCount,
			TotalTokens:        ca.TotalTokens,
			TotalRechargeCny:   ra.TotalRecharge,
			TotalConsumedUsd:   quotaToUSD(ca.TotalQuota),
			RemainingUsd:       quotaToUSD(u.Quota),
		})
	}

	// 11. 聚合字段排序 + 分页（仅当 sortCol == "" 时走这里；SQL 分页那条已经返回）
	if sortCol == "" {
		sortRowsByAggregate(rows, f.sortBy, f.sortDir)
		offset := (f.page - 1) * f.pageSize
		end := offset + f.pageSize
		if offset > len(rows) {
			rows = []detailsRow{}
		} else {
			if end > len(rows) {
				end = len(rows)
			}
			rows = rows[offset:end]
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    detailsResp{Rows: rows, Total: total, Page: f.page, PageSize: f.pageSize},
	})
}

func sortRowsByAggregate(rows []detailsRow, sortBy, sortDir string) {
	less := func(i, j int) bool { return rows[i].UserId < rows[j].UserId }
	switch sortBy {
	case "last_consume":
		less = func(i, j int) bool { return rows[i].LastConsumeAt < rows[j].LastConsumeAt }
	case "last_recharge":
		less = func(i, j int) bool { return rows[i].LastRechargeAt < rows[j].LastRechargeAt }
	case "consumed":
		less = func(i, j int) bool { return rows[i].TotalConsumedUsd < rows[j].TotalConsumedUsd }
	case "recharge":
		less = func(i, j int) bool { return rows[i].TotalRechargeCny < rows[j].TotalRechargeCny }
	case "requests":
		less = func(i, j int) bool { return rows[i].TotalRequests < rows[j].TotalRequests }
	case "tokens":
		less = func(i, j int) bool { return rows[i].TotalTokens < rows[j].TotalTokens }
	}
	if sortDir == "desc" {
		orig := less
		less = func(i, j int) bool { return orig(j, i) }
	}
	sort.SliceStable(rows, less)
}

// commonUserGroupCol 返回 users 表 "group" 列的安全引用（与 model/main.go 的命名一致）。
// group 是 SQL 保留字，PG 用 "group"，MySQL/SQLite 用 `group`。
func commonUserGroupCol() string {
	// 复用 model 层暴露的列引用变量（commonGroupCol 是 logs/users 共用的 group 列引用）
	return model.UsersGroupCol()
}

// =========================================================
// 单用户趋势对比（admin 版，复用 model.GetVipStatsTrend）
// =========================================================

// GetUserStatsUserTrend GET /api/user_stats/user_trend
// 与 /api/vip_stats/trend 同样的 model 逻辑，但用 admin auth 替代 password gate。
func GetUserStatsUserTrend(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	if userId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "user_id required"})
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
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "current_start/current_end/compare_start/compare_end required",
		})
		return
	}
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
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": resp})
}

// =========================================================
// 明细数据「按天统计」tab
// =========================================================

type detailsDailyFilter struct {
	startDate  string // YYYY-MM-DD（必填）
	endDate    string // YYYY-MM-DD（必填）
	username   string
	channels   []string
	sales      []string
	userGroups []string
	isVip      *bool
	page       int
	pageSize   int
	sortBy     string // date / username / display_name / requests / consumed / tokens
	sortDir    string
}

type detailsDailyRow struct {
	Date               string  `json:"date"` // YYYY-MM-DD
	UserId             int     `json:"user_id"`
	Username           string  `json:"username"`
	DisplayName        string  `json:"display_name"`
	IsVipCustomer      bool    `json:"is_vip_customer"`
	IsOfficial         bool    `json:"is_official"`
	BusinessChannel    string  `json:"business_channel"`
	InviterDisplayName string  `json:"inviter_display_name"`
	UserGroup          string  `json:"user_group"`
	DailyRequests      int64   `json:"daily_requests"`
	DailyConsumedUsd   float64 `json:"daily_consumed_usd"`
	DailyTokens        int64   `json:"daily_tokens"`
	// 当日统计专用：用户实时余额（与查询日期无关）；daily 接口不填该字段，omitempty 不输出
	RemainingUsd *float64 `json:"remaining_usd,omitempty"`
	// 当日统计专用：当天管理员"调整额度-充值"录入的金额（人民币 ¥）；daily 接口不填，omitempty 不输出
	DailyRechargeCny *float64 `json:"daily_recharge_cny,omitempty"`
}

type detailsDailyResp struct {
	Rows     []detailsDailyRow `json:"rows"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

// parseDetailsDailyFilter 与 parseDetailsFilter 共享了大部分字段，但 date 维度强制传入。
func parseDetailsDailyFilter(c *gin.Context) (*detailsDailyFilter, error) {
	f := &detailsDailyFilter{
		startDate:  strings.TrimSpace(c.Query("start_date")),
		endDate:    strings.TrimSpace(c.Query("end_date")),
		username:   strings.TrimSpace(c.Query("username")),
		channels:   splitCSV(c.Query("channel")),
		sales:      splitCSV(c.Query("sales")),
		userGroups: splitCSV(c.Query("user_group")),
		sortBy:     strings.TrimSpace(c.Query("sort_by")),
		sortDir:    strings.ToLower(strings.TrimSpace(c.Query("sort_dir"))),
	}
	if v := c.Query("is_vip"); v != "" {
		b, e := strconv.ParseBool(v)
		if e == nil {
			f.isVip = &b
		}
	}
	if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 0 {
		f.page = p
	} else {
		f.page = 1
	}
	if ps, err := strconv.Atoi(c.Query("page_size")); err == nil && ps > 0 && ps <= 200 {
		f.pageSize = ps
	} else {
		f.pageSize = 20
	}
	if f.sortDir != "asc" && f.sortDir != "desc" {
		f.sortDir = "desc"
	}
	// 默认排序：按日期倒序，同日期内 user_id asc 稳定
	if f.sortBy == "" {
		f.sortBy = "date"
		f.sortDir = "desc"
	}
	if f.startDate == "" || f.endDate == "" {
		return nil, fmt.Errorf("start_date / end_date required")
	}
	return f, nil
}

// GetUserStatsDetailsDaily 「按天统计」明细：每用户每天一行（仅当天有数据才出现）。
//
// 数据流：
//  1. 按 filter 拿候选 user_ids（与 details 接口同样的过滤逻辑）
//  2. 历史段 [start, min(end, yesterday)] 从 vip_daily_consumptions 读
//  3. 含今天 → 今天那天实时聚合 logs
//  4. 用候选 user_ids IN 限定；某用户某天三项全 0 不入库 → 也不出现在结果
//  5. 内存合并、排序、分页
func GetUserStatsDetailsDaily(c *gin.Context) {
	f, err := parseDetailsDailyFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 1. 候选 user_ids（与 details 接口对齐：username / channel / sales / user_group / is_vip）
	candidateIds, ok, err := resolveDailyCandidateUserIds(f)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !ok {
		// 过滤命中 0 用户，直接空返回
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    detailsDailyResp{Rows: []detailsDailyRow{}, Total: 0, Page: f.page, PageSize: f.pageSize},
		})
		return
	}

	// 2. 历史段：vip_daily_consumptions（[start, min(end, yesterday)]）
	now := time.Now()
	loc := now.Location()
	todayStr := now.Format("2006-01-02")
	histEnd := f.endDate
	if histEnd >= todayStr {
		histEnd = now.AddDate(0, 0, -1).Format("2006-01-02")
	}

	type aggRow struct {
		UserId       int
		Date         string
		Quota        int64
		RequestCount int64
		Tokens       int64
	}
	aggMap := make(map[string]aggRow) // key = userId#date

	if f.startDate <= histEnd {
		tx := model.DB.Model(&model.VipDailyConsumption{}).
			Where("stat_date >= ? AND stat_date <= ?", f.startDate, histEnd)
		if candidateIds != nil {
			tx = tx.Where("user_id IN ?", candidateIds)
		}
		type row struct {
			UserId       int
			StatDate     string
			Quota        int64
			RequestCount int64
			Tokens       int64
		}
		var rows []row
		if err := tx.
			Select("user_id, stat_date, quota, request_count, tokens").
			Scan(&rows).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		for _, r := range rows {
			k := fmt.Sprintf("%d#%s", r.UserId, r.StatDate)
			aggMap[k] = aggRow{
				UserId:       r.UserId,
				Date:         r.StatDate,
				Quota:        r.Quota,
				RequestCount: r.RequestCount,
				Tokens:       r.Tokens,
			}
		}
	}

	// 3. 含今天 → 实时聚合 logs（净口径：与 vip_daily_consumption.quota 保持一致）
	if f.startDate <= todayStr && f.endDate >= todayStr {
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
		todayEnd := now.Unix()
		tx := model.LOG_DB.Model(&model.Log{}).
			Where("type IN ?", model.NetQuotaSumTypes()).
			Where("user_id > 0").
			Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd)
		if candidateIds != nil {
			tx = tx.Where("user_id IN ?", candidateIds)
		}
		type row struct {
			UserId       int
			TotalQuota   int64
			RequestCount int64
			TotalTokens  int64
		}
		var rows []row
		if err := tx.
			Select(fmt.Sprintf(
				"user_id, %s AS total_quota, "+
					"COUNT(CASE WHEN type = %d THEN 1 END) AS request_count, "+
					"COALESCE(SUM(CASE WHEN type = %d THEN prompt_tokens + completion_tokens ELSE 0 END), 0) AS total_tokens",
				model.NetQuotaSumExpr(), model.LogTypeConsume, model.LogTypeConsume)).
			Group("user_id").
			Scan(&rows).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		for _, r := range rows {
			// 跳过三项全 0 的（极少出现，但保险一下）
			if r.TotalQuota == 0 && r.RequestCount == 0 && r.TotalTokens == 0 {
				continue
			}
			k := fmt.Sprintf("%d#%s", r.UserId, todayStr)
			aggMap[k] = aggRow{
				UserId:       r.UserId,
				Date:         todayStr,
				Quota:        r.TotalQuota,
				RequestCount: r.RequestCount,
				Tokens:       r.TotalTokens,
			}
		}
	}

	if len(aggMap) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    detailsDailyResp{Rows: []detailsDailyRow{}, Total: 0, Page: f.page, PageSize: f.pageSize},
		})
		return
	}

	// 4. 收集出现的 user_id，反查 users 拿展示字段
	idSet := map[int]struct{}{}
	for _, a := range aggMap {
		idSet[a.UserId] = struct{}{}
	}
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	type userBrief struct {
		Id              int
		Username        string
		DisplayName     string
		IsVipCustomer   bool
		BusinessChannel string
		InviterId       int
		UserGroup       string
	}
	var users []userBrief
	if err := model.DB.Model(&model.User{}).
		Select("id, username, display_name, is_vip_customer, business_channel, inviter_id, "+
			commonUserGroupCol()+" AS user_group").
		Where("id IN ?", ids).
		Scan(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	userMap := make(map[int]userBrief, len(users))
	for _, u := range users {
		userMap[u.Id] = u
	}

	// 5. inviter display_name / business_channel 反查
	inviterDisplayMap := map[int]string{}
	inviterChannelMap := map[int]string{}
	{
		inviterIdSet := map[int]struct{}{}
		for _, u := range users {
			if u.InviterId > 0 {
				inviterIdSet[u.InviterId] = struct{}{}
			}
		}
		if len(inviterIdSet) > 0 {
			invIds := make([]int, 0, len(inviterIdSet))
			for id := range inviterIdSet {
				invIds = append(invIds, id)
			}
			type row struct {
				Id              int
				DisplayName     string
				BusinessChannel string
			}
			var inv []row
			if err := model.DB.Model(&model.User{}).
				Select("id, display_name, business_channel").
				Where("id IN ?", invIds).
				Scan(&inv).Error; err == nil {
				for _, r := range inv {
					// 「归属销售」语义：邀请人 business_channel 非空才算销售。
					inviterChannelMap[r.Id] = r.BusinessChannel
					if r.BusinessChannel != "" {
						inviterDisplayMap[r.Id] = r.DisplayName
					}
				}
			}
		}
	}

	// 6. 正式用户判定
	officialSet := map[int]struct{}{}
	if oids, err := model.GetOfficialUserIds(); err == nil {
		for _, id := range oids {
			officialSet[id] = struct{}{}
		}
	}

	// 7. 拼装所有 detailsDailyRow（一行 = 一用户一天）
	all := make([]detailsDailyRow, 0, len(aggMap))
	for _, a := range aggMap {
		u, ok := userMap[a.UserId]
		if !ok {
			continue // 用户已删除等异常
		}
		_, isOfficial := officialSet[a.UserId]
		all = append(all, detailsDailyRow{
			Date:               a.Date,
			UserId:             a.UserId,
			Username:           u.Username,
			DisplayName:        u.DisplayName,
			IsVipCustomer:      u.IsVipCustomer,
			IsOfficial:         isOfficial,
			BusinessChannel:    inviterChannelMap[u.InviterId],
			InviterDisplayName: inviterDisplayMap[u.InviterId],
			UserGroup:          u.UserGroup,
			DailyRequests:      a.RequestCount,
			DailyConsumedUsd:   quotaToUSD(a.Quota),
			DailyTokens:        a.Tokens,
		})
	}

	// 8. 排序
	sortDailyRows(all, f.sortBy, f.sortDir)

	total := int64(len(all))
	// 9. 分页
	offset := (f.page - 1) * f.pageSize
	end := offset + f.pageSize
	var pageRows []detailsDailyRow
	if offset >= len(all) {
		pageRows = []detailsDailyRow{}
	} else {
		if end > len(all) {
			end = len(all)
		}
		pageRows = all[offset:end]
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    detailsDailyResp{Rows: pageRows, Total: total, Page: f.page, PageSize: f.pageSize},
	})
}

// resolveDailyCandidateUserIds 解析 daily 接口的用户过滤参数 → 候选 user_ids。
//
// 返回 (ids, ok, err)：
//
//	ok == false: 过滤明确命中 0 用户（如 channel/sales 匹配空、或 user 表过滤空）
//	ids == nil:  没有任何用户维度过滤（不需要 user_id IN）
//	ids 非空:    有匹配的 user_id 子集
func resolveDailyCandidateUserIds(f *detailsDailyFilter) ([]int, bool, error) {
	// inviter 过滤
	var inviterIds []int
	needInviterFilter := len(f.channels) > 0 || len(f.sales) > 0
	if needInviterFilter {
		tx := model.DB.Model(&model.User{}).Where("business_channel <> ''")
		if len(f.channels) > 0 {
			tx = tx.Where("business_channel IN ?", f.channels)
		}
		if len(f.sales) > 0 {
			tx = tx.Where("display_name IN ?", f.sales)
		}
		if err := tx.Pluck("id", &inviterIds).Error; err != nil {
			return nil, false, err
		}
		if len(inviterIds) == 0 {
			return nil, false, nil
		}
	}

	hasUserLevelFilter := f.username != "" || needInviterFilter || len(f.userGroups) > 0 || f.isVip != nil
	if !hasUserLevelFilter {
		return nil, true, nil
	}

	tx := model.DB.Model(&model.User{})
	if f.username != "" {
		tx = tx.Where("username LIKE ? OR display_name LIKE ?",
			"%"+f.username+"%", "%"+f.username+"%")
	}
	if needInviterFilter {
		tx = tx.Where("inviter_id IN ?", inviterIds)
	}
	if len(f.userGroups) > 0 {
		tx = tx.Where(commonUserGroupCol()+" IN ?", f.userGroups)
	}
	if f.isVip != nil {
		tx = tx.Where("is_vip_customer = ?", *f.isVip)
	}
	var ids []int
	if err := tx.Pluck("id", &ids).Error; err != nil {
		return nil, false, err
	}
	if len(ids) == 0 {
		return nil, false, nil
	}
	return ids, true, nil
}

func sortDailyRows(rows []detailsDailyRow, sortBy, sortDir string) {
	desc := sortDir == "desc"
	var less func(i, j int) bool
	switch sortBy {
	case "date":
		// 同日期内按 user_id asc 稳定输出（避免每次刷新顺序抖动）
		less = func(i, j int) bool {
			if rows[i].Date != rows[j].Date {
				return rows[i].Date < rows[j].Date
			}
			return rows[i].UserId < rows[j].UserId
		}
	case "username":
		less = func(i, j int) bool { return rows[i].Username < rows[j].Username }
	case "display_name":
		less = func(i, j int) bool { return rows[i].DisplayName < rows[j].DisplayName }
	case "requests":
		less = func(i, j int) bool { return rows[i].DailyRequests < rows[j].DailyRequests }
	case "consumed":
		less = func(i, j int) bool { return rows[i].DailyConsumedUsd < rows[j].DailyConsumedUsd }
	case "tokens":
		less = func(i, j int) bool { return rows[i].DailyTokens < rows[j].DailyTokens }
	default:
		less = func(i, j int) bool { return rows[i].UserId < rows[j].UserId }
	}
	if desc {
		orig := less
		less = func(i, j int) bool { return orig(j, i) }
	}
	sort.SliceStable(rows, less)
}

// =========================================================
// 明细数据「当日统计」tab
// =========================================================
//
// 与 daily 的关键差异：
//   - 只查单天，不接受日期范围
//   - 展示所有候选用户（默认排除 deleted/disabled），没消耗的补 0
//   - 排序固定为 quota DESC, id ASC

type detailsSingleDayFilter struct {
	date       string // YYYY-MM-DD（必填）
	username   string
	channels   []string
	sales      []string
	userGroups []string
	isVip      *bool
	page       int
	pageSize   int
}

type detailsSingleDayResp struct {
	Rows     []detailsDailyRow `json:"rows"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

func parseDetailsSingleDayFilter(c *gin.Context) (*detailsSingleDayFilter, error) {
	f := &detailsSingleDayFilter{
		date:       strings.TrimSpace(c.Query("date")),
		username:   strings.TrimSpace(c.Query("username")),
		channels:   splitCSV(c.Query("channel")),
		sales:      splitCSV(c.Query("sales")),
		userGroups: splitCSV(c.Query("user_group")),
	}
	if f.date == "" {
		return nil, fmt.Errorf("date 必填，格式 YYYY-MM-DD")
	}
	if _, err := time.Parse("2006-01-02", f.date); err != nil {
		return nil, fmt.Errorf("date 格式错误，应为 YYYY-MM-DD")
	}
	if v := c.Query("is_vip"); v != "" {
		if b, e := strconv.ParseBool(v); e == nil {
			f.isVip = &b
		}
	}
	if p, e := strconv.Atoi(c.Query("page")); e == nil && p > 0 {
		f.page = p
	} else {
		f.page = 1
	}
	if ps, e := strconv.Atoi(c.Query("page_size")); e == nil && ps > 0 && ps <= 200 {
		f.pageSize = ps
	} else {
		f.pageSize = 20
	}
	return f, nil
}

// GetUserStatsDetailsSingleDay 「当日统计」明细：单天，含所有候选用户（无消耗补 0）。
//
// 数据流：
//  1. 候选用户 = users 表过滤后的全集（默认排除 deleted/disabled）
//  2. 当日聚合：历史日读 vip_daily_consumptions；今天实时聚合 logs
//  3. 内存 LEFT JOIN：用户全集 ⊕ 当日 map（缺失补 0）
//  4. 排序 quota DESC, id ASC；分页
func GetUserStatsDetailsSingleDay(c *gin.Context) {
	f, err := parseDetailsSingleDayFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	all, err := loadSingleDayAllRows(f)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	total := int64(len(all))

	// 分页
	offset := (f.page - 1) * f.pageSize
	var rows []detailsDailyRow
	if offset >= len(all) {
		rows = []detailsDailyRow{}
	} else {
		end := offset + f.pageSize
		if end > len(all) {
			end = len(all)
		}
		rows = all[offset:end]
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": detailsSingleDayResp{
			Rows:     rows,
			Total:    total,
			Page:     f.page,
			PageSize: f.pageSize,
		},
	})
}

// loadSingleDayAllRows 加载「当日统计」全量数据（不分页），按 quota DESC, id ASC 排序。
// 抽出来供导出接口复用。
func loadSingleDayAllRows(f *detailsSingleDayFilter) ([]detailsDailyRow, error) {
	// 1. inviter 过滤（channel / sales 维度按 inviter 语义）
	var inviterIds []int
	needInviterFilter := len(f.channels) > 0 || len(f.sales) > 0
	if needInviterFilter {
		tx := model.DB.Model(&model.User{}).Where("business_channel <> ''")
		if len(f.channels) > 0 {
			tx = tx.Where("business_channel IN ?", f.channels)
		}
		if len(f.sales) > 0 {
			tx = tx.Where("display_name IN ?", f.sales)
		}
		if err := tx.Pluck("id", &inviterIds).Error; err != nil {
			return nil, err
		}
		if len(inviterIds) == 0 {
			return []detailsDailyRow{}, nil
		}
	}

	// 2. users 候选（固定排除 disabled；soft delete 由 gorm.DeletedAt 自动过滤）
	userTx := model.DB.Model(&model.User{}).
		Where("status = ?", common.UserStatusEnabled)
	if f.username != "" {
		userTx = userTx.Where("username LIKE ? OR display_name LIKE ?",
			"%"+f.username+"%", "%"+f.username+"%")
	}
	if needInviterFilter {
		userTx = userTx.Where("inviter_id IN ?", inviterIds)
	}
	if len(f.userGroups) > 0 {
		userTx = userTx.Where(commonUserGroupCol()+" IN ?", f.userGroups)
	}
	if f.isVip != nil {
		userTx = userTx.Where("is_vip_customer = ?", *f.isVip)
	}

	type userBrief struct {
		Id              int
		Username        string
		DisplayName     string
		IsVipCustomer   bool
		BusinessChannel string
		InviterId       int
		UserGroup       string
		Quota           int64
	}
	var users []userBrief
	if err := userTx.
		Select("id, username, display_name, is_vip_customer, business_channel, inviter_id, quota, " +
			commonUserGroupCol() + " AS user_group").
		Scan(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return []detailsDailyRow{}, nil
	}

	// 3. 当日聚合
	type aggRow struct {
		Quota        int64
		RequestCount int64
		Tokens       int64
	}
	aggMap := map[int]aggRow{}
	now := time.Now()
	todayStr := now.Format("2006-01-02")

	candidateIds := make([]int, 0, len(users))
	for _, u := range users {
		candidateIds = append(candidateIds, u.Id)
	}

	if f.date < todayStr {
		// 历史日：vip_daily_consumptions（00:10 定时任务落盘）
		type row struct {
			UserId       int
			Quota        int64
			RequestCount int64
			Tokens       int64
		}
		var rows []row
		if err := model.DB.Model(&model.VipDailyConsumption{}).
			Where("stat_date = ?", f.date).
			Where("user_id IN ?", candidateIds).
			Select("user_id, quota, request_count, tokens").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			aggMap[r.UserId] = aggRow{Quota: r.Quota, RequestCount: r.RequestCount, Tokens: r.Tokens}
		}
	} else if f.date == todayStr {
		// 今天：实时聚合 logs
		loc := now.Location()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
		todayEnd := now.Unix()
		type row struct {
			UserId       int
			TotalQuota   int64
			RequestCount int64
			TotalTokens  int64
		}
		// 净口径：quota 扣掉视频等异步任务的差额退款；request_count/tokens 仅算 type=LogTypeConsume
		var rows []row
		if err := model.LOG_DB.Model(&model.Log{}).
			Where("type IN ?", model.NetQuotaSumTypes()).
			Where("user_id IN ?", candidateIds).
			Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
			Select(fmt.Sprintf(
				"user_id, %s AS total_quota, "+
					"COUNT(CASE WHEN type = %d THEN 1 END) AS request_count, "+
					"COALESCE(SUM(CASE WHEN type = %d THEN prompt_tokens + completion_tokens ELSE 0 END), 0) AS total_tokens",
				model.NetQuotaSumExpr(), model.LogTypeConsume, model.LogTypeConsume)).
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			aggMap[r.UserId] = aggRow{Quota: r.TotalQuota, RequestCount: r.RequestCount, Tokens: r.TotalTokens}
		}
	}
	// f.date > todayStr（未来日）：aggMap 留空，全部展示 0

	// 3b. 当日充值聚合：混合模式 —— 历史日读 vip_daily_consumptions.recharge_amount；
	//     今天实时查 logs（type=3 + operation_type=额度 + quota_type=充值，SUM(recharge_input_amount)）。
	//     单位：人民币 ¥。未来日：留空，全部 0。
	rechargeMap := map[int]float64{}
	if f.date < todayStr {
		type row struct {
			UserId         int
			RechargeAmount float64
		}
		var rows []row
		if err := model.DB.Model(&model.VipDailyConsumption{}).
			Where("stat_date = ?", f.date).
			Where("user_id IN ?", candidateIds).
			Select("user_id, recharge_amount").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			rechargeMap[r.UserId] = r.RechargeAmount
		}
	} else if f.date == todayStr {
		loc := now.Location()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
		todayEnd := now.Unix()
		type row struct {
			UserId        int
			TotalRecharge float64
		}
		var rows []row
		if err := model.LOG_DB.Model(&model.Log{}).
			Where("type = ?", model.LogTypeManage).
			Where("operation_type = ?", model.OperationTypeQuota).
			Where("quota_type = ?", model.QuotaTypeRecharge).
			Where("user_id IN ?", candidateIds).
			Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
			Select("user_id, COALESCE(SUM(recharge_input_amount), 0) AS total_recharge").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			rechargeMap[r.UserId] = r.TotalRecharge
		}
	}

	// 4. 反查邀请人 display_name + business_channel
	inviterDisplayMap := map[int]string{}
	inviterChannelMap := map[int]string{}
	{
		inviterIdSet := map[int]struct{}{}
		for _, u := range users {
			if u.InviterId > 0 {
				inviterIdSet[u.InviterId] = struct{}{}
			}
		}
		if len(inviterIdSet) > 0 {
			invIds := make([]int, 0, len(inviterIdSet))
			for id := range inviterIdSet {
				invIds = append(invIds, id)
			}
			type row struct {
				Id              int
				DisplayName     string
				BusinessChannel string
			}
			var inv []row
			if err := model.DB.Model(&model.User{}).
				Select("id, display_name, business_channel").
				Where("id IN ?", invIds).
				Scan(&inv).Error; err == nil {
				for _, r := range inv {
					// 「归属销售」语义：邀请人 business_channel 非空才算销售。
					inviterChannelMap[r.Id] = r.BusinessChannel
					if r.BusinessChannel != "" {
						inviterDisplayMap[r.Id] = r.DisplayName
					}
				}
			}
		}
	}

	// 5. 正式用户
	officialSet := map[int]struct{}{}
	if oids, err := model.GetOfficialUserIds(); err == nil {
		for _, id := range oids {
			officialSet[id] = struct{}{}
		}
	}

	// 6. 排序：quota DESC, id ASC
	sort.SliceStable(users, func(i, j int) bool {
		qi := aggMap[users[i].Id].Quota
		qj := aggMap[users[j].Id].Quota
		if qi != qj {
			return qi > qj
		}
		return users[i].Id < users[j].Id
	})

	// 7. 拼装
	rows := make([]detailsDailyRow, 0, len(users))
	for _, u := range users {
		a := aggMap[u.Id]
		_, isOfficial := officialSet[u.Id]
		remaining := quotaToUSD(u.Quota)
		recharge := rechargeMap[u.Id]
		rows = append(rows, detailsDailyRow{
			Date:               f.date,
			UserId:             u.Id,
			Username:           u.Username,
			DisplayName:        u.DisplayName,
			IsVipCustomer:      u.IsVipCustomer,
			IsOfficial:         isOfficial,
			BusinessChannel:    inviterChannelMap[u.InviterId],
			InviterDisplayName: inviterDisplayMap[u.InviterId],
			UserGroup:          u.UserGroup,
			DailyRequests:      a.RequestCount,
			DailyConsumedUsd:   quotaToUSD(a.Quota),
			DailyTokens:        a.Tokens,
			RemainingUsd:       &remaining,
			DailyRechargeCny:   &recharge,
		})
	}
	return rows, nil
}
