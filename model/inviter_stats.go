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
package model

import (
	"sort"
	"time"
)

// ----------------------------------------------------------------------------
// 顶部 4 张卡片
// ----------------------------------------------------------------------------

type InviterStatCards struct {
	InvitedCount       int   `json:"invited_count"`        // 已邀请人数（不受时间影响）
	TotalConsumed      int64 `json:"total_consumed"`       // 累计消耗金额（SUM(users.used_quota)，不查 logs）
	TodayActiveUsers   int   `json:"today_active_users"`   // 今日使用用户数（今天 logs 出现的去重 user 数）
	TodayConsumed      int64 `json:"today_consumed"`       // 今日消耗金额 (SUM(quota) from logs today)
}

// GetInviterStatCards 顶部 4 卡片：根据 me 邀请的所有用户，分别计算固定语义指标
func GetInviterStatCards(myUserId int) (*InviterStatCards, error) {
	type uidUsed struct {
		Id        int
		UsedQuota int64
	}
	var users []uidUsed
	if err := DB.Model(&User{}).
		Select("id, used_quota").
		Where("inviter_id = ?", myUserId).
		Find(&users).Error; err != nil {
		return nil, err
	}
	stat := &InviterStatCards{InvitedCount: len(users)}
	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
		stat.TotalConsumed += u.UsedQuota
	}
	if len(ids) == 0 {
		return stat, nil
	}

	// 今日 logs：按 user 去重 + SUM
	now := time.Now()
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()

	type todayRow struct {
		UserId int
		Total  int64
	}
	var rows []todayRow
	if err := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", ids).
		Where("created_at >= ? AND created_at <= ?", todayStart, now.Unix()).
		Select("user_id, COALESCE(SUM(quota), 0) AS total").
		Group("user_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	stat.TodayActiveUsers = len(rows)
	for _, r := range rows {
		stat.TodayConsumed += r.Total
	}
	return stat, nil
}

// ----------------------------------------------------------------------------
// 3 个图表（时间窗口内）
// ----------------------------------------------------------------------------

// InviterChartUserSpend Top10 用户消耗排行的单行
type InviterChartUserSpend struct {
	Username string `json:"username"`
	Quota    int64  `json:"quota"`
}

// InviterChartDayPoint 时间趋势单点（按天）
type InviterChartDayPoint struct {
	Date     string `json:"date"`      // YYYY-MM-DD
	Quota    int64  `json:"quota"`     // 消耗趋势
	Requests int64  `json:"requests"`  // 请求次数
}

type InviterCharts struct {
	TopUsers []InviterChartUserSpend `json:"top_users"` // TOP10 消耗排行
	Daily    []InviterChartDayPoint  `json:"daily"`     // 按天聚合：消耗趋势 + 请求次数
}

// GetInviterCharts 时间窗口内的图表数据
func GetInviterCharts(myUserId int, startTs, endTs int64) (*InviterCharts, error) {
	resp := &InviterCharts{
		TopUsers: []InviterChartUserSpend{},
		Daily:    []InviterChartDayPoint{},
	}

	type idName struct {
		Id       int
		Username string
	}
	var users []idName
	if err := DB.Model(&User{}).
		Select("id, username").
		Where("inviter_id = ?", myUserId).
		Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return resp, nil
	}
	usernameById := make(map[int]string, len(users))
	ids := make([]int, 0, len(users))
	for _, u := range users {
		usernameById[u.Id] = u.Username
		ids = append(ids, u.Id)
	}

	// TopUsers：单查 logs 按 user_id 聚合，倒序取前 10
	type userAgg struct {
		UserId int
		Total  int64
	}
	var perUser []userAgg
	tx := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", ids)
	if startTs > 0 {
		tx = tx.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		tx = tx.Where("created_at <= ?", endTs)
	}
	if err := tx.
		Select("user_id, COALESCE(SUM(quota), 0) AS total").
		Group("user_id").
		Order("total DESC").
		Limit(10).
		Scan(&perUser).Error; err != nil {
		return nil, err
	}
	for _, p := range perUser {
		resp.TopUsers = append(resp.TopUsers, InviterChartUserSpend{
			Username: usernameById[p.UserId],
			Quota:    p.Total,
		})
	}

	// Daily：按天聚合 quota + requests
	type dayRow struct {
		Day      string
		Quota    int64
		Requests int64
	}
	var dayRows []dayRow
	// 三库通用：用 unix 秒除以 86400 取整得到天序号；为了能按 date 字符串排序，传回 YYYY-MM-DD
	// 但跨库 date format 函数不一致，采用前置按 created_at 拉所有 row 再内存聚合
	type rawRow struct {
		CreatedAt int64
		Quota     int64
	}
	var raw []rawRow
	tx2 := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", ids)
	if startTs > 0 {
		tx2 = tx2.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		tx2 = tx2.Where("created_at <= ?", endTs)
	}
	if err := tx2.Select("created_at, quota").Scan(&raw).Error; err != nil {
		return nil, err
	}
	loc := time.Now().Location()
	daily := map[string]*dayRow{}
	for _, r := range raw {
		date := time.Unix(r.CreatedAt, 0).In(loc).Format("2006-01-02")
		row, ok := daily[date]
		if !ok {
			row = &dayRow{Day: date}
			daily[date] = row
		}
		row.Quota += r.Quota
		row.Requests++
	}
	for _, r := range daily {
		dayRows = append(dayRows, *r)
	}
	sort.SliceStable(dayRows, func(i, j int) bool {
		return dayRows[i].Day < dayRows[j].Day
	})
	for _, r := range dayRows {
		resp.Daily = append(resp.Daily, InviterChartDayPoint{
			Date:     r.Day,
			Quota:    r.Quota,
			Requests: r.Requests,
		})
	}
	return resp, nil
}

// ----------------------------------------------------------------------------
// 汇总表格
// ----------------------------------------------------------------------------

type InviterSummaryRow struct {
	UserId           int    `json:"user_id"`
	Username         string `json:"username"`
	CreatedAt        int64  `json:"created_at"`
	LastConsumedAt   int64  `json:"last_consumed_at"`   // 0 表示从未消费
	TotalRequests    int64  `json:"total_requests"`     // 累计请求次数
	TotalConsumed    int64  `json:"total_consumed"`     // 累计 quota
	TotalTokens      int64  `json:"total_tokens"`
	CurrentRemaining int64  `json:"current_remaining"`
}

type InviterSummaryFilter struct {
	LastConsumedStart int64
	LastConsumedEnd   int64
	RemainingOp       string // ">=" / "<=" / "=" / ""（不筛）
	RemainingValue    int64
	UsernameKeyword   string
}

// GetInviterSummary 返回汇总表格的所有行（不分页 — 一个登录用户通常邀请数有限）
// 汇总数据按 TotalConsumed 倒序
func GetInviterSummary(myUserId int, f InviterSummaryFilter) ([]InviterSummaryRow, error) {
	type userRow struct {
		Id        int
		Username  string
		CreatedAt int64
		Quota     int64
		UsedQuota int64
	}
	var users []userRow
	tx := DB.Model(&User{}).
		Select("id, username, created_at, quota, used_quota").
		Where("inviter_id = ?", myUserId)
	if f.UsernameKeyword != "" {
		tx = tx.Where("username LIKE ?", "%"+f.UsernameKeyword+"%")
	}
	switch f.RemainingOp {
	case ">=":
		tx = tx.Where("quota >= ?", f.RemainingValue)
	case "<=":
		tx = tx.Where("quota <= ?", f.RemainingValue)
	case "=":
		tx = tx.Where("quota = ?", f.RemainingValue)
	}
	if err := tx.Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return []InviterSummaryRow{}, nil
	}

	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
	}

	// 一次 logs 聚合：每个用户的 SUM(quota)、SUM(tokens)、COUNT(*)、MAX(created_at)
	type logAgg struct {
		UserId         int
		TotalQuota     int64
		TotalTokens    int64
		RequestCount   int64
		LastConsumedAt int64
	}
	var aggs []logAgg
	if err := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", ids).
		Select("user_id, COALESCE(SUM(quota), 0) AS total_quota, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS total_tokens, COUNT(*) AS request_count, COALESCE(MAX(created_at), 0) AS last_consumed_at").
		Group("user_id").
		Scan(&aggs).Error; err != nil {
		return nil, err
	}
	aggBy := make(map[int]logAgg, len(aggs))
	for _, a := range aggs {
		aggBy[a.UserId] = a
	}

	rows := make([]InviterSummaryRow, 0, len(users))
	for _, u := range users {
		a := aggBy[u.Id]
		// 按"最后一次消耗日期"窗口过滤（仅当 LastConsumedAt 在窗口内）
		if f.LastConsumedStart > 0 || f.LastConsumedEnd > 0 {
			if a.LastConsumedAt == 0 {
				continue
			}
			if f.LastConsumedStart > 0 && a.LastConsumedAt < f.LastConsumedStart {
				continue
			}
			if f.LastConsumedEnd > 0 && a.LastConsumedAt > f.LastConsumedEnd {
				continue
			}
		}
		rows = append(rows, InviterSummaryRow{
			UserId:           u.Id,
			Username:         u.Username,
			CreatedAt:        u.CreatedAt,
			LastConsumedAt:   a.LastConsumedAt,
			TotalRequests:    a.RequestCount,
			TotalConsumed:    a.TotalQuota,
			TotalTokens:      a.TotalTokens,
			CurrentRemaining: u.Quota,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].TotalConsumed > rows[j].TotalConsumed
	})
	return rows, nil
}

// ----------------------------------------------------------------------------
// 按天表格
// ----------------------------------------------------------------------------

type InviterDailyRow struct {
	Date          string `json:"date"`           // YYYY-MM-DD
	Username      string `json:"username"`
	TotalRequests int64  `json:"total_requests"` // 当天该用户的请求次数
	TotalConsumed int64  `json:"total_consumed"` // 当天 quota
	TotalTokens   int64  `json:"total_tokens"`
}

type InviterDailyFilter struct {
	StartTs         int64
	EndTs           int64
	UsernameKeyword string
}

// GetInviterDaily 按天展开：每个 (天, 用户) 一行。
//   - 只显示有消费记录的 (天, 用户) 组合（Q4=A）
//   - 排序：日期倒序，同日 total_consumed 倒序
//   - 返回所有行，前端分页
func GetInviterDaily(myUserId int, f InviterDailyFilter) ([]InviterDailyRow, error) {
	type idName struct {
		Id       int
		Username string
	}
	var users []idName
	tx := DB.Model(&User{}).
		Select("id, username").
		Where("inviter_id = ?", myUserId)
	if f.UsernameKeyword != "" {
		tx = tx.Where("username LIKE ?", "%"+f.UsernameKeyword+"%")
	}
	if err := tx.Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return []InviterDailyRow{}, nil
	}
	usernameById := map[int]string{}
	ids := make([]int, 0, len(users))
	for _, u := range users {
		usernameById[u.Id] = u.Username
		ids = append(ids, u.Id)
	}

	type rawRow struct {
		UserId    int
		CreatedAt int64
		Quota     int64
		Tokens    int64
	}
	var raw []rawRow
	tx2 := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", ids)
	if f.StartTs > 0 {
		tx2 = tx2.Where("created_at >= ?", f.StartTs)
	}
	if f.EndTs > 0 {
		tx2 = tx2.Where("created_at <= ?", f.EndTs)
	}
	if err := tx2.
		Select("user_id, created_at, quota, (prompt_tokens + completion_tokens) AS tokens").
		Scan(&raw).Error; err != nil {
		return nil, err
	}
	loc := time.Now().Location()
	type bucketKey struct {
		Date   string
		UserId int
	}
	bucket := map[bucketKey]*InviterDailyRow{}
	for _, r := range raw {
		date := time.Unix(r.CreatedAt, 0).In(loc).Format("2006-01-02")
		key := bucketKey{Date: date, UserId: r.UserId}
		row, ok := bucket[key]
		if !ok {
			row = &InviterDailyRow{
				Date:     date,
				Username: usernameById[r.UserId],
			}
			bucket[key] = row
		}
		row.TotalRequests++
		row.TotalConsumed += r.Quota
		row.TotalTokens += r.Tokens
	}
	rows := make([]InviterDailyRow, 0, len(bucket))
	for _, r := range bucket {
		rows = append(rows, *r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date > rows[j].Date // 日期倒序
		}
		return rows[i].TotalConsumed > rows[j].TotalConsumed
	})
	return rows, nil
}
