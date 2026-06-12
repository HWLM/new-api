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
	UserId        int     `json:"user_id"`
	Username      string  `json:"username"`
	Remaining     int64   `json:"remaining"`
	Daily         []int64 `json:"daily"`          // 与 dates 一一对应，最后一个元素是今天的实时消耗 quota
	DailyRequests []int64 `json:"daily_requests"` // 同上，每天请求次数
	DailyTokens   []int64 `json:"daily_tokens"`   // 同上，每天 token 总数
}

// VipDetailResp 明细页接口返回
type VipDetailResp struct {
	Summary       VipDetailSummary `json:"summary"`
	Dates         []string         `json:"dates"`          // YYYY-MM-DD，7 个，最后一个是今天
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
// 列规则：近 7 天的日期数组（[today-6, today]），今天是实时聚合 logs，其余天查统计表
func GetVipStatsDetail() (*VipDetailResp, error) {
	type idQuota struct {
		Id       int
		Username string
		Quota    int64
	}
	var users []idQuota
	if err := DB.Model(&User{}).
		Select("id, username, quota").
		Where("is_vip_customer = ?", commonTrueVal).
		Order("id asc").
		Find(&users).Error; err != nil {
		return nil, err
	}

	// 最近 7 天的日期数组（最后一个是今天）
	now := time.Now()
	loc := now.Location()
	dates := make([]string, 7)
	for i := 0; i < 7; i++ {
		d := now.AddDate(0, 0, -(6 - i))
		dates[i] = d.Format("2006-01-02")
	}

	resp := &VipDetailResp{
		Summary:       VipDetailSummary{UserCount: len(users)},
		Dates:         dates,
		Rows:          []VipDetailRow{},
		Totals:        make([]int64, 7),
		TotalRequests: make([]int64, 7),
		TotalTokens:   make([]int64, 7),
	}

	if len(users) == 0 {
		return resp, nil
	}

	ids := make([]int, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
		resp.Summary.CurrentRemaining += u.Quota
	}

	// 历史 6 天（today-6 ~ today-1）查统计表
	historyMap, err := GetVipDailyConsumptionInRange(ids, dates[0], dates[5])
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
			Remaining:     u.Quota,
			Daily:         make([]int64, 7),
			DailyRequests: make([]int64, 7),
			DailyTokens:   make([]int64, 7),
		}
		for i, date := range dates {
			var quota, requests, tokens int64
			if i == 6 {
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

	// 4 个累计统计：今天来自 totals[6]，7 天累计 = 7 列之和
	resp.Summary.TodayConsumed = resp.Totals[6]
	resp.Summary.TodayRequests = resp.TotalRequests[6]
	resp.Summary.TodayTokens = resp.TotalTokens[6]
	for i := 0; i < 7; i++ {
		resp.Summary.WeeklyConsumed += resp.Totals[i]
		resp.Summary.WeeklyRequests += resp.TotalRequests[i]
		resp.Summary.WeeklyTokens += resp.TotalTokens[i]
	}

	return resp, nil
}

// WeeklyAggregate 7 天累计统计（用于 8 点播报）
type WeeklyAggregate struct {
	Quota    int64
	Requests int64
	Tokens   int64
}

// SumWeeklyRealtimeAggregate 计算"近 7 天累计 quota / requests / tokens"
// 口径：当前实时 VIP 客户；历史 6 天来自统计表，今天来自 logs 实时聚合。
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
	loc := now.Location()

	// 历史 6 天聚合统计表
	startHist := now.AddDate(0, 0, -6).Format("2006-01-02")
	endHist := now.AddDate(0, 0, -1).Format("2006-01-02")
	hist, err := SumVipDailyAggregate(ids, startHist, endHist)
	if err != nil {
		return sum, err
	}
	sum.Quota = hist.Quota
	sum.Requests = hist.RequestCount
	sum.Tokens = hist.Tokens

	// 今天实时聚合 logs
	todayStartTs := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
	todayMap, err := sumLogsTodayPerUser(ids, todayStartTs, now.Unix())
	if err != nil {
		return sum, err
	}
	for _, v := range todayMap {
		sum.Quota += v.Quota
		sum.Requests += v.Requests
		sum.Tokens += v.Tokens
	}
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
