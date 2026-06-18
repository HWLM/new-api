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

// VipStat 重点客户聚合统计（用于 8 点 TG 播报）
type VipStat struct {
	UserCount         int   // 重点客户人数
	YesterdayConsumed int64 // 昨天累计消耗 quota
	CurrentRemaining  int64 // 当前累计剩余 quota
}

// CollectVipStat 聚合所有重点客户的人数、昨天消耗、当前剩余余额。
func CollectVipStat() (*VipStat, error) {
	type userIdQuota struct {
		Id    int
		Quota int64
	}
	var users []userIdQuota
	err := DB.Model(&User{}).
		Select("id, quota").
		Where("is_vip_customer = ?", commonTrueVal).
		Find(&users).Error
	if err != nil {
		return nil, err
	}

	stat := &VipStat{UserCount: len(users)}
	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
		stat.CurrentRemaining += u.Quota
	}
	if len(ids) == 0 {
		return stat, nil
	}

	now := time.Now()
	loc := now.Location()
	y, m, d := now.AddDate(0, 0, -1).Date()
	startTs := time.Date(y, m, d, 0, 0, 0, 0, loc).Unix()
	endTs := time.Date(y, m, d, 23, 59, 59, 999999999, loc).Unix()

	var sumQuota int64
	err = LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", ids).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Select("COALESCE(SUM(quota), 0)").
		Scan(&sumQuota).Error
	if err != nil {
		return nil, err
	}
	stat.YesterdayConsumed = sumQuota
	return stat, nil
}

// VipDetailRow 明细页表格一行（一个客户）
type VipDetailRow struct {
	UserId                 int     `json:"user_id"`
	Username               string  `json:"username"`
	DisplayName            string  `json:"display_name"` // 简称（来自 users.display_name）
	Remaining              int64   `json:"remaining"`
	Daily                  []int64 `json:"daily"`                    // 与 dates 一一对应，最后一个元素是今天的实时消耗 quota
	DailyRequests          []int64 `json:"daily_requests"`           // 同上，每天请求次数
	DailyTokens            []int64 `json:"daily_tokens"`             // 同上，每天 token 总数
	InviterUsername        string  `json:"inviter_username"`         // 归属邀请人（邀请人的 username；无邀请人则为空）
	InviterBusinessChannel string  `json:"inviter_business_channel"` // 归属渠道（邀请人的 business_channel；无则为空）
}

// VipDetailResp 明细页接口返回
type VipDetailResp struct {
	Summary       VipDetailSummary `json:"summary"`
	Dates         []string         `json:"dates"`          // YYYY-MM-DD，8 个，最后一个是今天
	Rows          []VipDetailRow   `json:"rows"`           // 按 user_id 升序，行 = 当前实时 VIP 客户
	Totals        []int64          `json:"totals"`         // 每个日期列的 quota 合计
	TotalRequests []int64          `json:"total_requests"` // 每个日期列的请求次数合计
	TotalTokens   []int64          `json:"total_tokens"`   // 每个日期列的 token 数合计
}

// VipDetailSummary 顶部 8 张统计卡片的数据
type VipDetailSummary struct {
	UserCount        int   `json:"user_count"`
	TodayConsumed    int64 `json:"today_consumed"`
	WeeklyConsumed   int64 `json:"weekly_consumed"`
	CurrentRemaining int64 `json:"current_remaining"`
	TodayRequests    int64 `json:"today_requests"`
	TodayTokens      int64 `json:"today_tokens"`
	WeeklyRequests   int64 `json:"weekly_requests"`
	WeeklyTokens     int64 `json:"weekly_tokens"`

	// 较昨日（昨天同一段时间窗口 00:00 ~ now-24h）
	YesterdayConsumed int64 `json:"yesterday_consumed"`
	YesterdayRequests int64 `json:"yesterday_requests"`
	YesterdayTokens   int64 `json:"yesterday_tokens"`
	// 较上周期（[today-14, today-8] 这 7 个整天）
	PrevWeekConsumed int64 `json:"prev_week_consumed"`
	PrevWeekRequests int64 `json:"prev_week_requests"`
	PrevWeekTokens   int64 `json:"prev_week_tokens"`
}

// todayLogAggregate 今天 logs 的 per-user 聚合结果
type todayLogAggregate struct {
	Quota    int64
	Requests int64
	Tokens   int64
}

// sumLogsTodayPerUser 实时聚合 logs 表，返回每个用户今天的 quota / request_count / tokens。
// 仅统计 type=consume 的记录。
func sumLogsTodayPerUser(userIds []int, startTs, endTs int64) (map[int]todayLogAggregate, error) {
	result := make(map[int]todayLogAggregate)
	if len(userIds) == 0 {
		return result, nil
	}
	type row struct {
		UserId       int
		TotalQuota   int64
		RequestCount int64
		TotalTokens  int64
	}
	var rows []row
	err := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", userIds).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Select("user_id, COALESCE(SUM(quota), 0) AS total_quota, COUNT(*) AS request_count, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS total_tokens").
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.UserId] = todayLogAggregate{
			Quota:    r.TotalQuota,
			Requests: r.RequestCount,
			Tokens:   r.TotalTokens,
		}
	}
	return result, nil
}

// GetVipStatsDetail 获取明细页所有数据。
//
// 行规则：以接口调用时刻实时查询的 VIP 客户为行（新标记的立即出现，取消的立即消失）
// 列规则：近 8 天的日期数组（[today-7, today]），今天是实时聚合 logs，其余天查统计表
//
// Summary 中的 Weekly* 字段口径为「不含今天的 7 天」（即 Totals[0..6] 求和，对应 [today-7, today-1]）
func GetVipStatsDetail() (*VipDetailResp, error) {
	type idQuota struct {
		Id          int
		Username    string
		DisplayName string
		Quota       int64
		InviterId   int
	}
	var users []idQuota
	if err := DB.Model(&User{}).
		Select("id, username, display_name, quota, inviter_id").
		Where("is_vip_customer = ?", commonTrueVal).
		Order("id asc").
		Find(&users).Error; err != nil {
		return nil, err
	}

	// 批量查所有邀请人的 username + business_channel（避免 N+1）
	inviterMap := map[int]struct {
		Username        string
		BusinessChannel string
	}{}
	inviterIdSet := map[int]struct{}{}
	for _, u := range users {
		if u.InviterId > 0 {
			inviterIdSet[u.InviterId] = struct{}{}
		}
	}
	if len(inviterIdSet) > 0 {
		inviterIds := make([]int, 0, len(inviterIdSet))
		for id := range inviterIdSet {
			inviterIds = append(inviterIds, id)
		}
		type inviterRow struct {
			Id              int
			Username        string
			BusinessChannel string
		}
		var inviters []inviterRow
		if err := DB.Model(&User{}).
			Select("id, username, business_channel").
			Where("id IN ?", inviterIds).
			Find(&inviters).Error; err == nil {
			for _, r := range inviters {
				inviterMap[r.Id] = struct {
					Username        string
					BusinessChannel string
				}{r.Username, r.BusinessChannel}
			}
		}
	}

	// 最近 8 天的日期数组（最后一个是今天）
	now := time.Now()
	loc := now.Location()
	dates := make([]string, 8)
	for i := 0; i < 8; i++ {
		d := now.AddDate(0, 0, -(7 - i))
		dates[i] = d.Format("2006-01-02")
	}

	resp := &VipDetailResp{
		Summary:       VipDetailSummary{UserCount: len(users)},
		Dates:         dates,
		Rows:          []VipDetailRow{},
		Totals:        make([]int64, 8),
		TotalRequests: make([]int64, 8),
		TotalTokens:   make([]int64, 8),
	}

	if len(users) == 0 {
		return resp, nil
	}

	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
		resp.Summary.CurrentRemaining += u.Quota
	}

	// 历史 7 天（today-7 ~ today-1）查统计表
	historyMap, err := GetVipDailyConsumptionInRange(ids, dates[0], dates[6])
	if err != nil {
		return nil, err
	}

	// 今天实时聚合 logs
	todayStartTs := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
	todayMap, err := sumLogsTodayPerUser(ids, todayStartTs, now.Unix())
	if err != nil {
		return nil, err
	}

	for _, u := range users {
		row := VipDetailRow{
			UserId:        u.Id,
			Username:      u.Username,
			DisplayName:   u.DisplayName,
			Remaining:     u.Quota,
			Daily:         make([]int64, 8),
			DailyRequests: make([]int64, 8),
			DailyTokens:   make([]int64, 8),
		}
		if u.InviterId > 0 {
			if info, ok := inviterMap[u.InviterId]; ok {
				row.InviterUsername = info.Username
				row.InviterBusinessChannel = info.BusinessChannel
			}
		}
		for i, date := range dates {
			var quota, requests, tokens int64
			if i == 7 {
				agg := todayMap[u.Id]
				quota = agg.Quota
				requests = agg.Requests
				tokens = agg.Tokens
			} else {
				if perDate, ok := historyMap[u.Id]; ok {
					if entry, ok2 := perDate[date]; ok2 {
						quota = entry.Quota
						requests = entry.RequestCount
						tokens = entry.Tokens
					}
				}
			}
			row.Daily[i] = quota
			row.DailyRequests[i] = requests
			row.DailyTokens[i] = tokens
			resp.Totals[i] += quota
			resp.TotalRequests[i] += requests
			resp.TotalTokens[i] += tokens
		}
		resp.Rows = append(resp.Rows, row)
	}

	// user_id 升序（防御性，DB 已 Order）
	sort.SliceStable(resp.Rows, func(i, j int) bool {
		return resp.Rows[i].UserId < resp.Rows[j].UserId
	})

	// 累计统计：今天来自 totals[7]；7 天累计 = 前 7 列之和（不含今天）
	resp.Summary.TodayConsumed = resp.Totals[7]
	resp.Summary.TodayRequests = resp.TotalRequests[7]
	resp.Summary.TodayTokens = resp.TotalTokens[7]
	for i := 0; i < 7; i++ {
		resp.Summary.WeeklyConsumed += resp.Totals[i]
		resp.Summary.WeeklyRequests += resp.TotalRequests[i]
		resp.Summary.WeeklyTokens += resp.TotalTokens[i]
	}

	// 环比：较昨日（昨天 00:00 ~ now-24h 的实时 logs 聚合，时间窗口与今天等长）
	yStartTs := todayStartTs - 86400
	yEndTs := now.Unix() - 86400
	yMap, err := sumLogsTodayPerUser(ids, yStartTs, yEndTs)
	if err != nil {
		return nil, err
	}
	for _, v := range yMap {
		resp.Summary.YesterdayConsumed += v.Quota
		resp.Summary.YesterdayRequests += v.Requests
		resp.Summary.YesterdayTokens += v.Tokens
	}

	// 环比：较上周期（[today-14, today-8] 7 个整天，来自统计表）
	prevStart := now.AddDate(0, 0, -14).Format("2006-01-02")
	prevEnd := now.AddDate(0, 0, -8).Format("2006-01-02")
	prevAgg, err := SumVipDailyAggregate(ids, prevStart, prevEnd)
	if err != nil {
		return nil, err
	}
	resp.Summary.PrevWeekConsumed = prevAgg.Quota
	resp.Summary.PrevWeekRequests = prevAgg.RequestCount
	resp.Summary.PrevWeekTokens = prevAgg.Tokens

	return resp, nil
}

// WeeklyAggregate 7 天累计统计（用于 8 点播报）
type WeeklyAggregate struct {
	Quota    int64
	Requests int64
	Tokens   int64
}

// SumWeeklyRealtimeAggregate 计算"近 7 天累计 quota / requests / tokens"
// 口径：当前实时 VIP 客户；7 天范围为 [today-7, today-1]（不含今天），全部来自统计表。
func SumWeeklyRealtimeAggregate() (WeeklyAggregate, error) {
	var sum WeeklyAggregate
	var ids []int
	if err := DB.Model(&User{}).
		Select("id").
		Where("is_vip_customer = ?", commonTrueVal).
		Pluck("id", &ids).Error; err != nil {
		return sum, err
	}
	if len(ids) == 0 {
		return sum, nil
	}

	now := time.Now()

	// 近 7 天（today-7 ~ today-1）聚合统计表，不含今天
	startHist := now.AddDate(0, 0, -7).Format("2006-01-02")
	endHist := now.AddDate(0, 0, -1).Format("2006-01-02")
	hist, err := SumVipDailyAggregate(ids, startHist, endHist)
	if err != nil {
		return sum, err
	}
	sum.Quota = hist.Quota
	sum.Requests = hist.RequestCount
	sum.Tokens = hist.Tokens
	return sum, nil
}

// SumWeeklyConsumedRealtime 旧 API 兼容包装：只返回 quota，TG 8 点播报继续用
func SumWeeklyConsumedRealtime() (int64, error) {
	w, err := SumWeeklyRealtimeAggregate()
	return w.Quota, err
}

// RunVipDailyStat 聚合给定日期（YYYY-MM-DD）的"当前 VIP 客户"消耗、请求次数、token 数
// 写入 vip_daily_consumption 表。凌晨 2 点定时任务和手动 backfill 都复用这个函数。
func RunVipDailyStat(statDate string) (int, error) {
	type idUsername struct {
		Id       int
		Username string
	}
	var users []idUsername
	if err := DB.Model(&User{}).
		Select("id, username").
		Where("is_vip_customer = ?", commonTrueVal).
		Find(&users).Error; err != nil {
		return 0, err
	}
	if len(users) == 0 {
		return 0, nil
	}

	t, err := time.ParseInLocation("2006-01-02", statDate, time.Now().Location())
	if err != nil {
		return 0, err
	}
	startTs := t.Unix()
	endTs := t.Add(24 * time.Hour).Add(-time.Nanosecond).Unix()

	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
	}
	perUser, err := sumLogsTodayPerUser(ids, startTs, endTs)
	if err != nil {
		return 0, err
	}

	records := make([]VipDailyConsumption, 0, len(users))
	for _, u := range users {
		agg := perUser[u.Id] // 当天没消耗的客户拿到零值
		// 三个指标全为 0 = 该客户当天完全没动静，不入库（查询端 SUM/COALESCE 天然按 0 处理）
		if agg.Quota == 0 && agg.Requests == 0 && agg.Tokens == 0 {
			continue
		}
		records = append(records, VipDailyConsumption{
			UserId:       u.Id,
			Username:     u.Username,
			StatDate:     statDate,
			Quota:        agg.Quota,
			RequestCount: agg.Requests,
			Tokens:       agg.Tokens,
		})
	}
	if err := UpsertVipDailyConsumption(records); err != nil {
		return 0, err
	}
	return len(records), nil
}

// TrendQuery 趋势对比查询参数
type TrendQuery struct {
	UserId      int    // 单用户 ID（0 = 全体 VIP 聚合）
	Granularity string // "day" / "hour"
	StartDate   string // YYYY-MM-DD，闭区间
	EndDate     string // YYYY-MM-DD，闭区间
	StartHour   int    // 0..23，仅 granularity=hour 时使用（day 时建议传 0）
	EndHour     int    // 0..23，仅 granularity=hour 时使用（day 时建议传 23）
}

// TrendSeries 单条趋势线数据
type TrendSeries struct {
	Buckets []string `json:"buckets"` // x 轴 label：day → "MM-DD"；hour → "MM-DD HH:00"
	Values  []int64  `json:"values"`  // 对应 quota 值（单位 quota，前端转 $）
	Total   int64    `json:"total"`   // 全部 buckets 求和
}

// queryTrendSeries 单边查询：按 granularity 聚合给定 user + 时间窗口的 quota 数据。
func queryTrendSeries(q TrendQuery) (TrendSeries, error) {
	var series TrendSeries
	if q.UserId <= 0 {
		return series, fmt.Errorf("user_id required")
	}
	if q.StartDate == "" || q.EndDate == "" {
		return series, fmt.Errorf("date range required")
	}
	if q.Granularity != "day" && q.Granularity != "hour" {
		return series, fmt.Errorf("granularity must be day or hour")
	}
	if q.StartHour < 0 {
		q.StartHour = 0
	}
	if q.EndHour < 0 || q.EndHour > 23 {
		q.EndHour = 23
	}

	loc := time.Now().Location()
	start, err := time.ParseInLocation("2006-01-02", q.StartDate, loc)
	if err != nil {
		return series, fmt.Errorf("invalid start_date: %w", err)
	}
	end, err := time.ParseInLocation("2006-01-02", q.EndDate, loc)
	if err != nil {
		return series, fmt.Errorf("invalid end_date: %w", err)
	}
	if end.Before(start) {
		return series, fmt.Errorf("end_date earlier than start_date")
	}

	userIds := []int{q.UserId}

	if q.Granularity == "day" {
		// 按天：先生成 [start, end] 完整 date 序列，再用 SumVipHourlyByDay 填充
		valuesMap, err := SumVipHourlyByDay(userIds, q.StartDate, q.EndDate, q.StartHour, q.EndHour)
		if err != nil {
			return series, err
		}
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			date := d.Format("2006-01-02")
			v := valuesMap[date]
			series.Buckets = append(series.Buckets, d.Format("01-02"))
			series.Values = append(series.Values, v)
			series.Total += v
		}
		return series, nil
	}

	// 按小时：生成 [start#StartHour, end#EndHour] 完整 (date, hour) 序列
	valuesMap, err := SumVipHourlyByHour(userIds, q.StartDate, q.EndDate, q.StartHour, q.EndHour)
	if err != nil {
		return series, err
	}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		for h := q.StartHour; h <= q.EndHour; h++ {
			key := hourBucketKey(date, h)
			v := valuesMap[key]
			series.Buckets = append(series.Buckets, fmt.Sprintf("%s %02d:00", d.Format("01-02"), h))
			series.Values = append(series.Values, v)
			series.Total += v
		}
	}
	return series, nil
}

// TrendCompareResp 趋势对比完整响应
type TrendCompareResp struct {
	Granularity string      `json:"granularity"`
	Current     TrendSeries `json:"current"`
	Compare     TrendSeries `json:"compare"`
	// Bottom 4 cards
	CurrentTotal int64   `json:"current_total"`
	CompareTotal int64   `json:"compare_total"`
	Diff         int64   `json:"diff"`        // current - compare
	ChangeRate   float64 `json:"change_rate"` // (diff / compare) * 100，compare=0 时返回 0
}

// GetVipStatsTrend 查询单用户的两周期对比。
// 规则：
//  1. 按 granularity 分别查 current / compare 两段 buckets
//  2. 长度不等时按较短的截断（业务规则 Q4=B）
//  3. 用截断后的总和算 diff / change_rate
func GetVipStatsTrend(currQ, compQ TrendQuery) (*TrendCompareResp, error) {
	curr, err := queryTrendSeries(currQ)
	if err != nil {
		return nil, err
	}
	comp, err := queryTrendSeries(compQ)
	if err != nil {
		return nil, err
	}

	// 长度不等 → 按较短截断（保持 x 轴对齐）
	minLen := len(curr.Buckets)
	if len(comp.Buckets) < minLen {
		minLen = len(comp.Buckets)
	}
	if len(curr.Buckets) > minLen {
		dropped := sumInt64(curr.Values[minLen:])
		curr.Buckets = curr.Buckets[:minLen]
		curr.Values = curr.Values[:minLen]
		curr.Total -= dropped
	}
	if len(comp.Buckets) > minLen {
		dropped := sumInt64(comp.Values[minLen:])
		comp.Buckets = comp.Buckets[:minLen]
		comp.Values = comp.Values[:minLen]
		comp.Total -= dropped
	}

	resp := &TrendCompareResp{
		Granularity:  currQ.Granularity,
		Current:      curr,
		Compare:      comp,
		CurrentTotal: curr.Total,
		CompareTotal: comp.Total,
		Diff:         curr.Total - comp.Total,
	}
	if comp.Total != 0 {
		resp.ChangeRate = float64(curr.Total-comp.Total) / float64(comp.Total) * 100
	}
	return resp, nil
}

func sumInt64(xs []int64) int64 {
	var s int64
	for _, x := range xs {
		s += x
	}
	return s
}

// RunVipHourlyStat 聚合给定日期 + 小时（YYYY-MM-DD, 0..23）的"当前 VIP 客户"消耗、请求次数、token 数
// 写入 vip_hourly_consumption 表。每小时 :05 cron 跑「上一小时」+ 手动 backfill 都复用这个函数。
func RunVipHourlyStat(statDate string, statHour int) (int, error) {
	if statHour < 0 || statHour > 23 {
		return 0, fmt.Errorf("statHour 必须在 0..23 之间，传入：%d", statHour)
	}
	type idUsername struct {
		Id       int
		Username string
	}
	var users []idUsername
	if err := DB.Model(&User{}).
		Select("id, username").
		Where("is_vip_customer = ?", commonTrueVal).
		Find(&users).Error; err != nil {
		return 0, err
	}
	if len(users) == 0 {
		return 0, nil
	}

	t, err := time.ParseInLocation("2006-01-02", statDate, time.Now().Location())
	if err != nil {
		return 0, err
	}
	startTs := t.Add(time.Duration(statHour) * time.Hour).Unix()
	endTs := t.Add(time.Duration(statHour+1)*time.Hour).Unix() - 1

	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
	}
	perUser, err := sumLogsTodayPerUser(ids, startTs, endTs)
	if err != nil {
		return 0, err
	}

	records := make([]VipHourlyConsumption, 0, len(users))
	for _, u := range users {
		agg := perUser[u.Id]
		// 三个指标全为 0 = 该客户那一小时完全没动静，不入库（查询端 SUM/COALESCE 天然按 0 处理）
		if agg.Quota == 0 && agg.Requests == 0 && agg.Tokens == 0 {
			continue
		}
		records = append(records, VipHourlyConsumption{
			UserId:       u.Id,
			Username:     u.Username,
			StatDate:     statDate,
			StatHour:     statHour,
			Quota:        agg.Quota,
			RequestCount: agg.Requests,
			Tokens:       agg.Tokens,
		})
	}
	if err := UpsertVipHourlyConsumption(records); err != nil {
		return 0, err
	}
	return len(records), nil
}

// GetVipUsersBelowBalance 返回所有重点客户里余额 < threshold（quota 单位）的客户。
// 用于每小时余额告警任务。
func GetVipUsersBelowBalance(threshold int64) ([]User, error) {
	var users []User
	err := DB.Model(&User{}).
		Select("id, username, quota").
		Where("is_vip_customer = ?", commonTrueVal).
		Where("quota < ?", threshold).
		Order("id asc").
		Find(&users).Error
	return users, err
}
