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
	"time"

	"gorm.io/gorm/clause"
)

// VipDailyConsumption 重点客户每日消耗统计。
// 由凌晨 2 点的定时任务写入，每条记录代表"某个客户某一天"的总消耗 quota / 请求次数 / token 数。
type VipDailyConsumption struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	UserId       int    `json:"user_id" gorm:"index;uniqueIndex:uk_vip_daily_user_date,priority:1"`
	Username     string `json:"username" gorm:"type:varchar(64);default:''"`
	StatDate     string `json:"stat_date" gorm:"type:varchar(10);index;uniqueIndex:uk_vip_daily_user_date,priority:2"` // YYYY-MM-DD
	Quota        int64  `json:"quota" gorm:"default:0"`                                                               // 当天消耗 quota（单位 = QuotaPerUnit / 美元）
	RequestCount int64  `json:"request_count" gorm:"default:0;column:request_count"`                                  // 当天请求次数
	Tokens       int64  `json:"tokens" gorm:"default:0;column:tokens"`                                                // 当天 prompt_tokens + completion_tokens 总和
	CreatedAt    int64  `json:"created_at" gorm:"autoCreateTime"`
}

// UpsertVipDailyConsumption 批量插入/更新某一天的统计数据。
// 同一 (user_id, stat_date) 已存在时覆盖 quota（重复跑时支持幂等）。
func UpsertVipDailyConsumption(records []VipDailyConsumption) error {
	if len(records) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for i := range records {
		if records[i].CreatedAt == 0 {
			records[i].CreatedAt = now
		}
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "stat_date"}},
		DoUpdates: clause.AssignmentColumns([]string{"quota", "request_count", "tokens", "username"}),
	}).Create(&records).Error
}

// DailyConsumptionEntry 一个客户某一天的全部指标
type DailyConsumptionEntry struct {
	Quota        int64
	RequestCount int64
	Tokens       int64
}

// GetVipDailyConsumptionInRange 查询给定用户在 [startDate, endDate] 区间的日消耗记录。
// 返回 map[userId]map[date]DailyConsumptionEntry，方便上层组装表格。
func GetVipDailyConsumptionInRange(userIds []int, startDate, endDate string) (map[int]map[string]DailyConsumptionEntry, error) {
	result := make(map[int]map[string]DailyConsumptionEntry)
	if len(userIds) == 0 {
		return result, nil
	}
	var rows []VipDailyConsumption
	err := DB.Model(&VipDailyConsumption{}).
		Where("user_id IN ?", userIds).
		Where("stat_date >= ? AND stat_date <= ?", startDate, endDate).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if _, ok := result[r.UserId]; !ok {
			result[r.UserId] = make(map[string]DailyConsumptionEntry)
		}
		result[r.UserId][r.StatDate] = DailyConsumptionEntry{
			Quota:        r.Quota,
			RequestCount: r.RequestCount,
			Tokens:       r.Tokens,
		}
	}
	return result, nil
}

// SumVipDailyAggregate 给定 userIds + 日期区间，返回 quota / request_count / tokens 三个总和
func SumVipDailyAggregate(userIds []int, startDate, endDate string) (DailyConsumptionEntry, error) {
	var sum DailyConsumptionEntry
	if len(userIds) == 0 {
		return sum, nil
	}
	type row struct {
		Quota        int64
		RequestCount int64
		Tokens       int64
	}
	var r row
	err := DB.Model(&VipDailyConsumption{}).
		Where("user_id IN ?", userIds).
		Where("stat_date >= ? AND stat_date <= ?", startDate, endDate).
		Select("COALESCE(SUM(quota), 0) AS quota, COALESCE(SUM(request_count), 0) AS request_count, COALESCE(SUM(tokens), 0) AS tokens").
		Scan(&r).Error
	if err != nil {
		return sum, err
	}
	sum.Quota = r.Quota
	sum.RequestCount = r.RequestCount
	sum.Tokens = r.Tokens
	return sum, nil
}
