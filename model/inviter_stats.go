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
	"fmt"
	"sort"
	"time"
)

// ----------------------------------------------------------------------------
// 顶部 4 张卡片
// ----------------------------------------------------------------------------

type InviterStatCards struct {
	InvitedCount     int   `json:"invited_count"`      // 已邀请人数（不受时间影响）
	TotalConsumed    int64 `json:"total_consumed"`     // 累计消耗金额（SUM(users.used_quota)，不查 logs）
	TodayActiveUsers int   `json:"today_active_users"` // 今日使用用户数（今天 logs 出现的去重 user 数）
	TodayConsumed    int64 `json:"today_consumed"`     // 今日消耗金额 (SUM(quota) from logs today)
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

	// 今日活跃用户：以「今日出现 type=LogTypeConsume」判断，退款不算活跃
	var activeUserIds []int
	if err := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", ids).
		Where("created_at >= ? AND created_at <= ?", todayStart, now.Unix()).
		Distinct("user_id").
		Pluck("user_id", &activeUserIds).Error; err != nil {
		return nil, err
	}
	stat.TodayActiveUsers = len(activeUserIds)

	// 今日消耗：净口径，扣掉视频等异步任务差额结算的退款
	var todayNetQuota int64
	if err := LOG_DB.Model(&Log{}).
		Where("type IN ?", NetQuotaSumTypes()).
		Where("user_id IN ?", ids).
		Where("created_at >= ? AND created_at <= ?", todayStart, now.Unix()).
		Select(NetQuotaSumExpr()).
		Scan(&todayNetQuota).Error; err != nil {
		return nil, err
	}
	stat.TodayConsumed = todayNetQuota
	return stat, nil
}

// ----------------------------------------------------------------------------
// 3 个图表（时间窗口内）
// ----------------------------------------------------------------------------

// InviterChartUserSpend Top10 用户消耗排行的单行
type InviterChartUserSpend struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Quota       int64  `json:"quota"`
}

// InviterChartDayPoint 时间趋势单点（按天）
type InviterChartDayPoint struct {
	Date     string `json:"date"`     // YYYY-MM-DD
	Quota    int64  `json:"quota"`    // 消耗趋势
	Requests int64  `json:"requests"` // 请求次数
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
		Id          int
		Username    string
		DisplayName string
	}
	var users []idName
	if err := DB.Model(&User{}).
		Select("id, username, display_name").
		Where("inviter_id = ?", myUserId).
		Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return resp, nil
	}
	type userBrief struct {
		Username    string
		DisplayName string
	}
	briefById := make(map[int]userBrief, len(users))
	ids := make([]int, 0, len(users))
	for _, u := range users {
		briefById[u.Id] = userBrief{Username: u.Username, DisplayName: u.DisplayName}
		ids = append(ids, u.Id)
	}

	// TopUsers：单查 logs 按 user_id 聚合，倒序取前 10（净口径，扣掉退款）
	type userAgg struct {
		UserId int
		Total  int64
	}
	var perUser []userAgg
	tx := LOG_DB.Model(&Log{}).
		Where("type IN ?", NetQuotaSumTypes()).
		Where("user_id IN ?", ids)
	if startTs > 0 {
		tx = tx.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		tx = tx.Where("created_at <= ?", endTs)
	}
	if err := tx.
		Select("user_id, " + NetQuotaSumExpr() + " AS total").
		Group("user_id").
		Order("total DESC").
		Limit(10).
		Scan(&perUser).Error; err != nil {
		return nil, err
	}
	for _, p := range perUser {
		b := briefById[p.UserId]
		resp.TopUsers = append(resp.TopUsers, InviterChartUserSpend{
			Username:    b.Username,
			DisplayName: b.DisplayName,
			Quota:       p.Total,
		})
	}

	// Daily：按天聚合 quota + requests
	// quota 走净口径（type=LogTypeConsume 计正、type=LogTypeRefund 计负）；
	// requests 只算 type=LogTypeConsume。跨库 date format 不一致，采用前置按 created_at 拉所有 row 再内存聚合
	type dayRow struct {
		Day      string
		Quota    int64
		Requests int64
	}
	var dayRows []dayRow
	type rawRow struct {
		CreatedAt int64
		Quota     int64
		Type      int
	}
	var raw []rawRow
	tx2 := LOG_DB.Model(&Log{}).
		Where("type IN ?", NetQuotaSumTypes()).
		Where("user_id IN ?", ids)
	if startTs > 0 {
		tx2 = tx2.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		tx2 = tx2.Where("created_at <= ?", endTs)
	}
	if err := tx2.Select("created_at, quota, type").Scan(&raw).Error; err != nil {
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
		if r.Type == LogTypeRefund {
			row.Quota -= r.Quota
		} else {
			row.Quota += r.Quota
			row.Requests++
		}
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
	UserId           int     `json:"user_id"`
	Username         string  `json:"username"`
	DisplayName      string  `json:"display_name"`
	CreatedAt        int64   `json:"created_at"`
	LastConsumedAt   int64   `json:"last_consumed_at"` // 0 表示从未消费
	TotalRequests    int64   `json:"total_requests"`   // 累计请求次数
	TotalConsumed    int64   `json:"total_consumed"`   // 累计 quota
	TotalTokens      int64   `json:"total_tokens"`
	TotalRechargeCny float64 `json:"total_recharge_cny"` // 累计充值金额（人民币 ¥），仅 operation_type=额度 + quota_type=充值
	CurrentRemaining int64   `json:"current_remaining"`
}

type InviterSummaryFilter struct {
	LastConsumedStart int64
	LastConsumedEnd   int64
	RemainingOp       string // ">=" / "<=" / "=" / ""（不筛）
	RemainingValue    int64
	UsernameKeyword   string
	// 排序字段，为空则按 total_consumed 倒序（默认行为）。
	// 允许值：username / created_at / last_consumed_at / total_requests /
	//   total_consumed / total_tokens / total_recharge_cny / current_remaining
	SortBy string
	// "asc" / "desc"，默认 "desc"
	SortOrder string
}

// GetInviterSummary 返回汇总表格的所有行（不分页 — 一个登录用户通常邀请数有限）
// 汇总数据按 TotalConsumed 倒序
func GetInviterSummary(myUserId int, f InviterSummaryFilter) ([]InviterSummaryRow, error) {
	type userRow struct {
		Id          int
		Username    string
		DisplayName string
		CreatedAt   int64
		Quota       int64
		UsedQuota   int64
	}
	var users []userRow
	tx := DB.Model(&User{}).
		Select("id, username, display_name, created_at, quota, used_quota").
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
	// quota 走净口径（type=LogTypeConsume 计正、type=LogTypeRefund 计负）；
	// tokens / request_count / last_consumed_at 仅算 type=LogTypeConsume（退款不是新请求，也不代表"最后消费日期"）
	type logAgg struct {
		UserId         int
		TotalQuota     int64
		TotalTokens    int64
		RequestCount   int64
		LastConsumedAt int64
	}
	var aggs []logAgg
	if err := LOG_DB.Model(&Log{}).
		Where("type IN ?", NetQuotaSumTypes()).
		Where("user_id IN ?", ids).
		Select(fmt.Sprintf(
			"user_id, %s AS total_quota, "+
				"COALESCE(SUM(CASE WHEN type = %d THEN prompt_tokens + completion_tokens ELSE 0 END), 0) AS total_tokens, "+
				"COUNT(CASE WHEN type = %d THEN 1 END) AS request_count, "+
				"COALESCE(MAX(CASE WHEN type = %d THEN created_at END), 0) AS last_consumed_at",
			NetQuotaSumExpr(), LogTypeConsume, LogTypeConsume, LogTypeConsume)).
		Group("user_id").
		Scan(&aggs).Error; err != nil {
		return nil, err
	}
	aggBy := make(map[int]logAgg, len(aggs))
	for _, a := range aggs {
		aggBy[a.UserId] = a
	}

	// 充值聚合：与消耗对称的一次查询；口径 = 管理员"调整额度-充值"录入金额。
	type rechargeAgg struct {
		UserId        int
		TotalRecharge float64
	}
	var rechargeAggs []rechargeAgg
	if err := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeManage).
		Where("operation_type = ?", OperationTypeQuota).
		Where("quota_type = ?", QuotaTypeRecharge).
		Where("user_id IN ?", ids).
		Select("user_id, COALESCE(SUM(recharge_input_amount), 0) AS total_recharge").
		Group("user_id").
		Scan(&rechargeAggs).Error; err != nil {
		return nil, err
	}
	rechargeBy := make(map[int]float64, len(rechargeAggs))
	for _, r := range rechargeAggs {
		rechargeBy[r.UserId] = r.TotalRecharge
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
			DisplayName:      u.DisplayName,
			CreatedAt:        u.CreatedAt,
			LastConsumedAt:   a.LastConsumedAt,
			TotalRequests:    a.RequestCount,
			TotalConsumed:    a.TotalQuota,
			TotalTokens:      a.TotalTokens,
			TotalRechargeCny: rechargeBy[u.Id],
			CurrentRemaining: u.Quota,
		})
	}
	sortInviterSummaryRows(rows, f.SortBy, f.SortOrder)
	return rows, nil
}

// sortInviterSummaryRows 根据 sortBy/sortOrder 对汇总行原地排序；未指定或 key 未识别时，
// 保持默认口径（total_consumed DESC）。使用稳定排序，避免同值行在多次点击间乱跳。
func sortInviterSummaryRows(rows []InviterSummaryRow, sortBy, sortOrder string) {
	asc := sortOrder == "asc"
	less := func(i, j int) bool { return rows[i].TotalConsumed > rows[j].TotalConsumed }
	switch sortBy {
	case "username":
		less = func(i, j int) bool {
			if asc {
				return rows[i].Username < rows[j].Username
			}
			return rows[i].Username > rows[j].Username
		}
	case "created_at":
		less = func(i, j int) bool {
			if asc {
				return rows[i].CreatedAt < rows[j].CreatedAt
			}
			return rows[i].CreatedAt > rows[j].CreatedAt
		}
	case "last_consumed_at":
		less = func(i, j int) bool {
			if asc {
				return rows[i].LastConsumedAt < rows[j].LastConsumedAt
			}
			return rows[i].LastConsumedAt > rows[j].LastConsumedAt
		}
	case "total_requests":
		less = func(i, j int) bool {
			if asc {
				return rows[i].TotalRequests < rows[j].TotalRequests
			}
			return rows[i].TotalRequests > rows[j].TotalRequests
		}
	case "total_consumed":
		less = func(i, j int) bool {
			if asc {
				return rows[i].TotalConsumed < rows[j].TotalConsumed
			}
			return rows[i].TotalConsumed > rows[j].TotalConsumed
		}
	case "total_tokens":
		less = func(i, j int) bool {
			if asc {
				return rows[i].TotalTokens < rows[j].TotalTokens
			}
			return rows[i].TotalTokens > rows[j].TotalTokens
		}
	case "total_recharge_cny":
		less = func(i, j int) bool {
			if asc {
				return rows[i].TotalRechargeCny < rows[j].TotalRechargeCny
			}
			return rows[i].TotalRechargeCny > rows[j].TotalRechargeCny
		}
	case "current_remaining":
		less = func(i, j int) bool {
			if asc {
				return rows[i].CurrentRemaining < rows[j].CurrentRemaining
			}
			return rows[i].CurrentRemaining > rows[j].CurrentRemaining
		}
	}
	sort.SliceStable(rows, less)
}

// ----------------------------------------------------------------------------
// 按天表格
// ----------------------------------------------------------------------------

type InviterDailyRow struct {
	Date             string  `json:"date"` // YYYY-MM-DD
	Username         string  `json:"username"`
	DisplayName      string  `json:"display_name"`
	TotalRequests    int64   `json:"total_requests"` // 当天该用户的请求次数
	TotalConsumed    int64   `json:"total_consumed"` // 当天 quota
	TotalTokens      int64   `json:"total_tokens"`
	TotalRechargeCny float64 `json:"total_recharge_cny"` // 当天充值金额（人民币 ¥），仅 operation_type=额度 + quota_type=充值
}

type InviterDailyFilter struct {
	StartTs         int64
	EndTs           int64
	UsernameKeyword string
	// 排序字段，为空则按 (date DESC, total_consumed DESC)（默认行为）。
	// 允许值：date / username / total_requests / total_consumed /
	//   total_tokens / total_recharge_cny
	SortBy string
	// "asc" / "desc"，默认 "desc"
	SortOrder string
}

// GetInviterDaily 按天展开：每个 (天, 用户) 一行。
//   - 只显示有消费记录的 (天, 用户) 组合（Q4=A）
//   - 排序：日期倒序，同日 total_consumed 倒序
//   - 返回所有行，前端分页
func GetInviterDaily(myUserId int, f InviterDailyFilter) ([]InviterDailyRow, error) {
	type idName struct {
		Id          int
		Username    string
		DisplayName string
	}
	var users []idName
	tx := DB.Model(&User{}).
		Select("id, username, display_name").
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
	type userBrief struct {
		Username    string
		DisplayName string
	}
	briefById := map[int]userBrief{}
	ids := make([]int, 0, len(users))
	for _, u := range users {
		briefById[u.Id] = userBrief{Username: u.Username, DisplayName: u.DisplayName}
		ids = append(ids, u.Id)
	}

	// quota 走净口径（type=LogTypeConsume 计正、type=LogTypeRefund 计负）；
	// TotalRequests / TotalTokens 仅算 type=LogTypeConsume
	type rawRow struct {
		UserId    int
		CreatedAt int64
		Quota     int64
		Tokens    int64
		Type      int
	}
	var raw []rawRow
	tx2 := LOG_DB.Model(&Log{}).
		Where("type IN ?", NetQuotaSumTypes()).
		Where("user_id IN ?", ids)
	if f.StartTs > 0 {
		tx2 = tx2.Where("created_at >= ?", f.StartTs)
	}
	if f.EndTs > 0 {
		tx2 = tx2.Where("created_at <= ?", f.EndTs)
	}
	if err := tx2.
		Select("user_id, created_at, quota, (prompt_tokens + completion_tokens) AS tokens, type").
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
			b := briefById[r.UserId]
			row = &InviterDailyRow{
				Date:        date,
				Username:    b.Username,
				DisplayName: b.DisplayName,
			}
			bucket[key] = row
		}
		if r.Type == LogTypeRefund {
			row.TotalConsumed -= r.Quota
		} else {
			row.TotalRequests++
			row.TotalConsumed += r.Quota
			row.TotalTokens += r.Tokens
		}
	}

	// 充值原始行：与消耗对称的一次查询，按 (day, user) 合到同一桶。
	// 仅有充值无消耗的 (day, user) 也会出现一行（消耗列为 0）。
	type rawRecharge struct {
		UserId    int
		CreatedAt int64
		Amount    float64
	}
	var rawRech []rawRecharge
	tx3 := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeManage).
		Where("operation_type = ?", OperationTypeQuota).
		Where("quota_type = ?", QuotaTypeRecharge).
		Where("user_id IN ?", ids)
	if f.StartTs > 0 {
		tx3 = tx3.Where("created_at >= ?", f.StartTs)
	}
	if f.EndTs > 0 {
		tx3 = tx3.Where("created_at <= ?", f.EndTs)
	}
	if err := tx3.
		Select("user_id, created_at, COALESCE(recharge_input_amount, 0) AS amount").
		Scan(&rawRech).Error; err != nil {
		return nil, err
	}
	for _, r := range rawRech {
		date := time.Unix(r.CreatedAt, 0).In(loc).Format("2006-01-02")
		key := bucketKey{Date: date, UserId: r.UserId}
		row, ok := bucket[key]
		if !ok {
			b := briefById[r.UserId]
			row = &InviterDailyRow{
				Date:        date,
				Username:    b.Username,
				DisplayName: b.DisplayName,
			}
			bucket[key] = row
		}
		row.TotalRechargeCny += r.Amount
	}

	rows := make([]InviterDailyRow, 0, len(bucket))
	for _, r := range bucket {
		rows = append(rows, *r)
	}
	sortInviterDailyRows(rows, f.SortBy, f.SortOrder)
	return rows, nil
}

// sortInviterDailyRows 根据 sortBy/sortOrder 对按天行原地排序；未指定或 key 未识别时，
// 保持默认口径（date DESC，同日 total_consumed DESC）。
func sortInviterDailyRows(rows []InviterDailyRow, sortBy, sortOrder string) {
	asc := sortOrder == "asc"
	less := func(i, j int) bool {
		if rows[i].Date != rows[j].Date {
			return rows[i].Date > rows[j].Date
		}
		return rows[i].TotalConsumed > rows[j].TotalConsumed
	}
	switch sortBy {
	case "date":
		less = func(i, j int) bool {
			if asc {
				return rows[i].Date < rows[j].Date
			}
			return rows[i].Date > rows[j].Date
		}
	case "username":
		less = func(i, j int) bool {
			if asc {
				return rows[i].Username < rows[j].Username
			}
			return rows[i].Username > rows[j].Username
		}
	case "total_requests":
		less = func(i, j int) bool {
			if asc {
				return rows[i].TotalRequests < rows[j].TotalRequests
			}
			return rows[i].TotalRequests > rows[j].TotalRequests
		}
	case "total_consumed":
		less = func(i, j int) bool {
			if asc {
				return rows[i].TotalConsumed < rows[j].TotalConsumed
			}
			return rows[i].TotalConsumed > rows[j].TotalConsumed
		}
	case "total_tokens":
		less = func(i, j int) bool {
			if asc {
				return rows[i].TotalTokens < rows[j].TotalTokens
			}
			return rows[i].TotalTokens > rows[j].TotalTokens
		}
	case "total_recharge_cny":
		less = func(i, j int) bool {
			if asc {
				return rows[i].TotalRechargeCny < rows[j].TotalRechargeCny
			}
			return rows[i].TotalRechargeCny > rows[j].TotalRechargeCny
		}
	}
	sort.SliceStable(rows, less)
}
