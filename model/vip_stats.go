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

// VipStat 重点客户聚合统计
type VipStat struct {
	UserCount         int   // 重点客户人数
	YesterdayConsumed int64 // 昨天累计消耗 quota（按服务器本地时区计算）
	CurrentRemaining  int64 // 当前累计剩余 quota
}

// CollectVipStat 聚合所有重点客户的人数、昨天消耗、当前剩余余额。
// 跨主 DB 和 LOG_DB 分两步查询，保证 LOG_SQL_DSN 单独配置时仍能工作。
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

	// 昨天 00:00:00 - 23:59:59（服务器本地时区）
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
	UserId    int     `json:"user_id"`
	Username  string  `json:"username"`
	Remaining int64   `json:"remaining"`
	Daily     []int64 `json:"daily"` // 与 dates 一一对应，最后一个元素是今天的实时消耗
}

// VipDetailResp 明细页接口返回
type VipDetailResp struct {
	Summary VipDetailSummary `json:"summary"`
	Dates   []string         `json:"dates"`  // YYYY-MM-DD，7 个，最后一个是今天
	Rows    []VipDetailRow   `json:"rows"`   // 按 user_id 升序，行 = 当前实时 VIP 客户
	Totals  []int64          `json:"totals"` // 每个日期列的合计
}

type VipDetailSummary struct {
	UserCount        int   `json:"user_count"`
	TodayConsumed    int64 `json:"today_consumed"`
	WeeklyConsumed   int64 `json:"weekly_consumed"`
	CurrentRemaining int64 `json:"current_remaining"`
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

	// 生成最近 7 天的日期数组（最后一个是今天）
	now := time.Now()
	loc := now.Location()
	dates := make([]string, 7)
	for i := 0; i < 7; i++ {
		d := now.AddDate(0, 0, -(6 - i))
		dates[i] = d.Format("2006-01-02")
	}

	resp := &VipDetailResp{
		Summary: VipDetailSummary{UserCount: len(users)},
		Dates:   dates,
		Rows:    []VipDetailRow{},
		Totals:  make([]int64, 7),
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

	// 今天实时聚合 logs（每个 vip 客户的 sum）
	todayStartTs := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
	todayMap, err := sumLogsTodayPerUser(ids, todayStartTs, now.Unix())
	if err != nil {
		return nil, err
	}

	for _, u := range users {
		row := VipDetailRow{
			UserId:    u.Id,
			Username:  u.Username,
			Remaining: u.Quota,
			Daily:     make([]int64, 7),
		}
		var todaySum int64
		for i, date := range dates {
			var v int64
			if i == 6 {
				v = todayMap[u.Id]
				todaySum = v
			} else {
				if perDate, ok := historyMap[u.Id]; ok {
					v = perDate[date]
				}
			}
			row.Daily[i] = v
			resp.Totals[i] += v
		}
		resp.Summary.TodayConsumed += todaySum
		resp.Rows = append(resp.Rows, row)
	}

	// 保证 rows 按 user_id 升序（防御性，Find 加了 Order 已经是升序）
	sort.SliceStable(resp.Rows, func(i, j int) bool {
		return resp.Rows[i].UserId < resp.Rows[j].UserId
	})

	for _, v := range resp.Totals {
		resp.Summary.WeeklyConsumed += v
	}

	return resp, nil
}

// sumLogsTodayPerUser 实时聚合 logs 表，返回 map[userId]quota。
// 仅统计 type=consume 的记录。
func sumLogsTodayPerUser(userIds []int, startTs, endTs int64) (map[int]int64, error) {
	result := make(map[int]int64)
	if len(userIds) == 0 {
		return result, nil
	}
	type row struct {
		UserId int
		Total  int64
	}
	var rows []row
	err := LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeConsume).
		Where("user_id IN ?", userIds).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Select("user_id, COALESCE(SUM(quota), 0) AS total").
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.UserId] = r.Total
	}
	return result, nil
}

// SumWeeklyConsumedRealtime 计算"近 7 天累计消耗"（含今天，按调用时刻的实时 VIP 客户口径）
// 用于 8 点 TG 播报。
func SumWeeklyConsumedRealtime() (int64, error) {
	var ids []int
	if err := DB.Model(&User{}).
		Select("id").
		Where("is_vip_customer = ?", commonTrueVal).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	now := time.Now()
	loc := now.Location()

	// 历史 6 天（today-6 ~ yesterday）查统计表
	startHist := now.AddDate(0, 0, -6).Format("2006-01-02")
	endHist := now.AddDate(0, 0, -1).Format("2006-01-02")
	histSum, err := SumVipDailyConsumptionInRange(ids, startHist, endHist)
	if err != nil {
		return 0, err
	}

	// 今天实时聚合 logs
	todayStartTs := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
	todayMap, err := sumLogsTodayPerUser(ids, todayStartTs, now.Unix())
	if err != nil {
		return 0, err
	}
	var todaySum int64
	for _, v := range todayMap {
		todaySum += v
	}
	return histSum + todaySum, nil
}

// RunVipDailyStat 聚合给定日期（YYYY-MM-DD）的"当前 VIP 客户"消耗写入统计表。
// 凌晨 2 点的定时任务以及手动 backfill 都复用这个函数。
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

	// 解析日期得到 startTs / endTs
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
		records = append(records, VipDailyConsumption{
			UserId:   u.Id,
			Username: u.Username,
			StatDate: statDate,
			Quota:    perUser[u.Id], // 当天没消耗的客户写 0，便于明细页区分"没数据"和"0 消耗"
		})
	}
	if err := UpsertVipDailyConsumption(records); err != nil {
		return 0, err
	}
	return len(records), nil
}
