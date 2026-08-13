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
// 内部工具
// ----------------------------------------------------------------------------

// inviterUserIdBatchSize 大商务客户数很多时对 user_id IN 做分批，避免 IN 列表过长。
const inviterUserIdBatchSize = 500

// chunkUserIds 把 user id 列表切成不超过 inviterUserIdBatchSize 的分片。
func chunkUserIds(ids []int) [][]int {
	if len(ids) <= inviterUserIdBatchSize {
		return [][]int{ids}
	}
	out := make([][]int, 0, (len(ids)+inviterUserIdBatchSize-1)/inviterUserIdBatchSize)
	for i := 0; i < len(ids); i += inviterUserIdBatchSize {
		end := i + inviterUserIdBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		out = append(out, ids[i:end])
	}
	return out
}

// splitInviterTimeWindow 把 [startTs, endTs] 切成"历史段（<= 昨天）+ 今天段"两部分。
// 历史段返回 stat_date 字符串上下界（YYYY-MM-DD），今天段返回 unix 秒上下界。
// 任一段为空时其 hasXxx=false。
func splitInviterTimeWindow(startTs, endTs int64, loc *time.Location) (
	histStart, histEnd string, hasHist bool,
	todayStart, todayEnd int64, hasToday bool,
) {
	now := time.Now().In(loc)
	todayZero := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
	todayStr := now.Format("2006-01-02")

	if startTs > 0 {
		histStart = time.Unix(startTs, 0).In(loc).Format("2006-01-02")
	}
	if endTs > 0 {
		endDateStr := time.Unix(endTs, 0).In(loc).Format("2006-01-02")
		if endDateStr < todayStr {
			histEnd = endDateStr
		} else {
			histEnd = now.AddDate(0, 0, -1).Format("2006-01-02")
		}
	} else {
		histEnd = now.AddDate(0, 0, -1).Format("2006-01-02")
	}
	hasHist = histEnd != "" && (histStart == "" || histStart <= histEnd)

	todayStart = todayZero
	if startTs > todayStart {
		todayStart = startTs
	}
	todayEnd = now.Unix()
	if endTs > 0 && endTs < todayEnd {
		todayEnd = endTs
	}
	hasToday = todayEnd >= todayZero && todayStart <= todayEnd
	return
}

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
// startTs / endTs 必须为正（controller 层保证：调用方未传时注入默认最近 30 天）。
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

	loc := time.Now().Location()
	histStart, histEnd, hasHist, todayStart, todayEnd, hasToday := splitInviterTimeWindow(startTs, endTs, loc)

	// TopUsers：历史段读 vip_daily_consumptions SUM(quota) GROUP BY user_id + 今天段读 logs，
	// 内存合并后 ORDER BY total DESC LIMIT 10。
	totalByUser := map[int]int64{}
	if hasHist {
		for _, chunk := range chunkUserIds(ids) {
			type histRow struct {
				UserId int
				Total  int64
			}
			var rows []histRow
			histTx := DB.Model(&VipDailyConsumption{}).
				Where("user_id IN ?", chunk).
				Where("stat_date <= ?", histEnd)
			if histStart != "" {
				histTx = histTx.Where("stat_date >= ?", histStart)
			}
			if err := histTx.
				Select("user_id, COALESCE(SUM(quota), 0) AS total").
				Group("user_id").
				Scan(&rows).Error; err != nil {
				return nil, err
			}
			for _, r := range rows {
				totalByUser[r.UserId] += r.Total
			}
		}
	}
	if hasToday {
		for _, chunk := range chunkUserIds(ids) {
			type todayRow struct {
				UserId int
				Total  int64
			}
			var rows []todayRow
			if err := LOG_DB.Model(&Log{}).
				Where("type IN ?", NetQuotaSumTypes()).
				Where("user_id IN ?", chunk).
				Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
				Select("user_id, " + NetQuotaSumExpr() + " AS total").
				Group("user_id").
				Scan(&rows).Error; err != nil {
				return nil, err
			}
			for _, r := range rows {
				totalByUser[r.UserId] += r.Total
			}
		}
	}
	type userTotal struct {
		UserId int
		Total  int64
	}
	sorted := make([]userTotal, 0, len(totalByUser))
	for uid, t := range totalByUser {
		sorted = append(sorted, userTotal{UserId: uid, Total: t})
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Total > sorted[j].Total })
	if len(sorted) > 10 {
		sorted = sorted[:10]
	}
	for _, s := range sorted {
		b := briefById[s.UserId]
		resp.TopUsers = append(resp.TopUsers, InviterChartUserSpend{
			Username:    b.Username,
			DisplayName: b.DisplayName,
			Quota:       s.Total,
		})
	}

	// Daily：按天聚合 quota + requests
	// 历史段直接从 vip_daily_consumptions 按 stat_date 分组，避免拉 raw log；
	// 今天段读 logs today window（数据量小），按用户 GROUP BY 后归到当天。
	type dayAgg struct {
		Quota    int64
		Requests int64
	}
	daily := map[string]*dayAgg{}
	if hasHist {
		for _, chunk := range chunkUserIds(ids) {
			type histRow struct {
				StatDate     string
				Quota        int64
				RequestCount int64
			}
			var rows []histRow
			histTx := DB.Model(&VipDailyConsumption{}).
				Where("user_id IN ?", chunk).
				Where("stat_date <= ?", histEnd)
			if histStart != "" {
				histTx = histTx.Where("stat_date >= ?", histStart)
			}
			if err := histTx.
				Select("stat_date, " +
					"COALESCE(SUM(quota), 0) AS quota, " +
					"COALESCE(SUM(request_count), 0) AS request_count").
				Group("stat_date").
				Scan(&rows).Error; err != nil {
				return nil, err
			}
			for _, r := range rows {
				d, ok := daily[r.StatDate]
				if !ok {
					d = &dayAgg{}
					daily[r.StatDate] = d
				}
				d.Quota += r.Quota
				d.Requests += r.RequestCount
			}
		}
	}
	if hasToday {
		todayStr := time.Now().In(loc).Format("2006-01-02")
		for _, chunk := range chunkUserIds(ids) {
			type todayRow struct {
				Quota    int64
				Requests int64
			}
			var row todayRow
			if err := LOG_DB.Model(&Log{}).
				Where("type IN ?", NetQuotaSumTypes()).
				Where("user_id IN ?", chunk).
				Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
				Select(fmt.Sprintf(
					"%s AS quota, "+
						"COUNT(CASE WHEN type = %d THEN 1 END) AS requests",
					NetQuotaSumExpr(), LogTypeConsume)).
				Scan(&row).Error; err != nil {
				return nil, err
			}
			d, ok := daily[todayStr]
			if !ok {
				d = &dayAgg{}
				daily[todayStr] = d
			}
			d.Quota += row.Quota
			d.Requests += row.Requests
		}
	}
	type dayRow struct {
		Day      string
		Quota    int64
		Requests int64
	}
	dayRows := make([]dayRow, 0, len(daily))
	for date, d := range daily {
		dayRows = append(dayRows, dayRow{Day: date, Quota: d.Quota, Requests: d.Requests})
	}
	sort.SliceStable(dayRows, func(i, j int) bool { return dayRows[i].Day < dayRows[j].Day })
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
		like := "%" + f.UsernameKeyword + "%"
		tx = tx.Where("username LIKE ? OR display_name LIKE ?", like, like)
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

	// 消耗聚合：历史段读 vip_daily_consumptions（一次 GROUP BY user_id 拿到 quota/tokens/requests/last_consumed_at），
	// 今天段读 logs（today window + user_id IN）。避免对 logs 做全时段 GROUP BY。
	type logAgg struct {
		UserId         int
		TotalQuota     int64
		TotalTokens    int64
		RequestCount   int64
		LastConsumedAt int64
	}
	aggBy := make(map[int]logAgg, len(users))

	now := time.Now()
	loc := now.Location()
	yesterdayStr := now.AddDate(0, 0, -1).Format("2006-01-02")
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
	todayEnd := now.Unix()

	// 历史段：vip_daily_consumptions（含 Claude 缓存补齐后的 tokens）
	for _, chunk := range chunkUserIds(ids) {
		type histRow struct {
			UserId         int
			TotalQuota     int64
			TotalTokens    int64
			RequestCount   int64
			LastConsumedAt int64
		}
		var rows []histRow
		if err := DB.Model(&VipDailyConsumption{}).
			Where("user_id IN ?", chunk).
			Where("stat_date <= ?", yesterdayStr).
			Select("user_id, " +
				"COALESCE(SUM(quota), 0) AS total_quota, " +
				"COALESCE(SUM(tokens), 0) AS total_tokens, " +
				"COALESCE(SUM(request_count), 0) AS request_count, " +
				"COALESCE(MAX(last_consumed_at), 0) AS last_consumed_at").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			aggBy[r.UserId] = logAgg{
				UserId:         r.UserId,
				TotalQuota:     r.TotalQuota,
				TotalTokens:    r.TotalTokens,
				RequestCount:   r.RequestCount,
				LastConsumedAt: r.LastConsumedAt,
			}
		}
	}

	// 今天段：logs today window（净口径），并补加 Claude 缓存 token
	for _, chunk := range chunkUserIds(ids) {
		type todayRow struct {
			UserId         int
			TotalQuota     int64
			TotalTokens    int64
			RequestCount   int64
			LastConsumedAt int64
		}
		var rows []todayRow
		if err := LOG_DB.Model(&Log{}).
			Where("type IN ?", NetQuotaSumTypes()).
			Where("user_id IN ?", chunk).
			Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
			Select(fmt.Sprintf(
				"user_id, %s AS total_quota, "+
					"COALESCE(SUM(CASE WHEN type = %d THEN prompt_tokens + completion_tokens ELSE 0 END), 0) AS total_tokens, "+
					"COUNT(CASE WHEN type = %d THEN 1 END) AS request_count, "+
					"COALESCE(MAX(CASE WHEN type = %d THEN created_at END), 0) AS last_consumed_at",
				NetQuotaSumExpr(), LogTypeConsume, LogTypeConsume, LogTypeConsume)).
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			agg := aggBy[r.UserId]
			agg.UserId = r.UserId
			agg.TotalQuota += r.TotalQuota
			agg.TotalTokens += r.TotalTokens
			agg.RequestCount += r.RequestCount
			if r.LastConsumedAt > agg.LastConsumedAt {
				agg.LastConsumedAt = r.LastConsumedAt
			}
			aggBy[r.UserId] = agg
		}
		claudeExtra, err := SumClaudeCacheTokensByUsers(todayStart, todayEnd, chunk)
		if err != nil {
			return nil, err
		}
		for uid, extra := range claudeExtra {
			agg := aggBy[uid]
			agg.UserId = uid
			agg.TotalTokens += extra
			aggBy[uid] = agg
		}
	}

	// 充值聚合：历史段读 vip_daily_consumptions.recharge_amount + last_recharge_at，
	// 今天段读 logs today window。同样按 500 分批。
	rechargeBy := make(map[int]float64, len(users))
	for _, chunk := range chunkUserIds(ids) {
		type histRow struct {
			UserId        int
			TotalRecharge float64
		}
		var rows []histRow
		if err := DB.Model(&VipDailyConsumption{}).
			Where("user_id IN ?", chunk).
			Where("stat_date <= ?", yesterdayStr).
			Select("user_id, COALESCE(SUM(recharge_amount), 0) AS total_recharge").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			rechargeBy[r.UserId] += r.TotalRecharge
		}
	}
	for _, chunk := range chunkUserIds(ids) {
		type todayRow struct {
			UserId        int
			TotalRecharge float64
		}
		var rows []todayRow
		if err := LOG_DB.Model(&Log{}).
			Where("type = ?", LogTypeManage).
			Where("operation_type = ?", OperationTypeQuota).
			Where("quota_type = ?", QuotaTypeRecharge).
			Where("user_id IN ?", chunk).
			Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
			Select("user_id, COALESCE(SUM(recharge_input_amount), 0) AS total_recharge").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			rechargeBy[r.UserId] += r.TotalRecharge
		}
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
//   - 只显示有消费记录或有充值记录的 (天, 用户) 组合
//   - 排序：日期倒序，同日 total_consumed 倒序
//   - 返回所有行，前端分页
//
// StartTs / EndTs 必须为正（controller 层保证：调用方未传时注入默认最近 30 天）。
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
		like := "%" + f.UsernameKeyword + "%"
		tx = tx.Where("username LIKE ? OR display_name LIKE ?", like, like)
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

	loc := time.Now().Location()
	histStart, histEnd, hasHist, todayStart, todayEnd, hasToday := splitInviterTimeWindow(f.StartTs, f.EndTs, loc)

	type bucketKey struct {
		Date   string
		UserId int
	}
	bucket := map[bucketKey]*InviterDailyRow{}
	ensureRow := func(k bucketKey) *InviterDailyRow {
		if row, ok := bucket[k]; ok {
			return row
		}
		b := briefById[k.UserId]
		row := &InviterDailyRow{
			Date:        k.Date,
			Username:    b.Username,
			DisplayName: b.DisplayName,
		}
		bucket[k] = row
		return row
	}

	// 历史段：vip_daily_consumptions 天然按 (user_id, stat_date) 分组
	if hasHist {
		for _, chunk := range chunkUserIds(ids) {
			type histRow struct {
				UserId         int
				StatDate       string
				Quota          int64
				RequestCount   int64
				Tokens         int64
				RechargeAmount float64
			}
			var rows []histRow
			histTx := DB.Model(&VipDailyConsumption{}).
				Where("user_id IN ?", chunk).
				Where("stat_date <= ?", histEnd)
			if histStart != "" {
				histTx = histTx.Where("stat_date >= ?", histStart)
			}
			if err := histTx.
				Select("user_id, stat_date, quota, request_count, tokens, recharge_amount").
				Scan(&rows).Error; err != nil {
				return nil, err
			}
			for _, r := range rows {
				k := bucketKey{Date: r.StatDate, UserId: r.UserId}
				row := ensureRow(k)
				row.TotalConsumed += r.Quota
				row.TotalRequests += r.RequestCount
				row.TotalTokens += r.Tokens
				row.TotalRechargeCny += r.RechargeAmount
			}
		}
	}

	// 今天段：消耗聚合（今天窗内 logs，按 user_id GROUP BY 后归到今天）
	if hasToday {
		todayStr := time.Now().In(loc).Format("2006-01-02")
		for _, chunk := range chunkUserIds(ids) {
			type todayRow struct {
				UserId       int
				TotalQuota   int64
				RequestCount int64
				TotalTokens  int64
			}
			var rows []todayRow
			if err := LOG_DB.Model(&Log{}).
				Where("type IN ?", NetQuotaSumTypes()).
				Where("user_id IN ?", chunk).
				Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
				Select(fmt.Sprintf(
					"user_id, %s AS total_quota, "+
						"COUNT(CASE WHEN type = %d THEN 1 END) AS request_count, "+
						"COALESCE(SUM(CASE WHEN type = %d THEN prompt_tokens + completion_tokens ELSE 0 END), 0) AS total_tokens",
					NetQuotaSumExpr(), LogTypeConsume, LogTypeConsume)).
				Group("user_id").
				Scan(&rows).Error; err != nil {
				return nil, err
			}
			for _, r := range rows {
				if r.TotalQuota == 0 && r.RequestCount == 0 && r.TotalTokens == 0 {
					continue
				}
				k := bucketKey{Date: todayStr, UserId: r.UserId}
				row := ensureRow(k)
				row.TotalConsumed += r.TotalQuota
				row.TotalRequests += r.RequestCount
				row.TotalTokens += r.TotalTokens
			}
			claudeExtra, err := SumClaudeCacheTokensByUsers(todayStart, todayEnd, chunk)
			if err != nil {
				return nil, err
			}
			for uid, extra := range claudeExtra {
				k := bucketKey{Date: todayStr, UserId: uid}
				row := ensureRow(k)
				row.TotalTokens += extra
			}
		}

		// 今天段：充值聚合
		for _, chunk := range chunkUserIds(ids) {
			type todayRech struct {
				UserId        int
				TotalRecharge float64
			}
			var rows []todayRech
			if err := LOG_DB.Model(&Log{}).
				Where("type = ?", LogTypeManage).
				Where("operation_type = ?", OperationTypeQuota).
				Where("quota_type = ?", QuotaTypeRecharge).
				Where("user_id IN ?", chunk).
				Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
				Select("user_id, COALESCE(SUM(recharge_input_amount), 0) AS total_recharge").
				Group("user_id").
				Scan(&rows).Error; err != nil {
				return nil, err
			}
			for _, r := range rows {
				if r.TotalRecharge == 0 {
					continue
				}
				k := bucketKey{Date: todayStr, UserId: r.UserId}
				row := ensureRow(k)
				row.TotalRechargeCny += r.TotalRecharge
			}
		}
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
