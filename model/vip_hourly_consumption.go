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
	"time"

	"gorm.io/gorm/clause"
)

// VipHourlyConsumption 用户「按小时」消耗+充值统计（历史名 vip_*，现已扩展为全量用户）。
// 由每小时 :05 cron 写入；每条记录 = 某用户某小时(0~23) 的总消耗 quota / 请求次数 / token 数 / 管理员充值金额。
// 小时维度专门为「趋势变化对比图」按小时切换准备，避免每次实时 GROUP BY logs 表。
// 表名沿用历史命名，但底层数据已包含所有用户；重点客户统计页通过 user_id IN (VIP ids) 过滤后再查询。
type VipHourlyConsumption struct {
	Id             int     `json:"id" gorm:"primaryKey"`
	UserId         int     `json:"user_id" gorm:"index;uniqueIndex:uk_vip_hourly_user_date_hour,priority:1"`
	Username       string  `json:"username" gorm:"type:varchar(64);default:''"`
	StatDate       string  `json:"stat_date" gorm:"type:varchar(10);index;uniqueIndex:uk_vip_hourly_user_date_hour,priority:2"` // YYYY-MM-DD
	StatHour       int     `json:"stat_hour" gorm:"uniqueIndex:uk_vip_hourly_user_date_hour,priority:3"`                        // 0..23
	Quota          int64   `json:"quota" gorm:"default:0"`
	RequestCount   int64   `json:"request_count" gorm:"default:0;column:request_count"`
	Tokens         int64   `json:"tokens" gorm:"default:0;column:tokens"`
	RechargeAmount float64 `json:"recharge_amount" gorm:"default:0;column:recharge_amount"` // 当小时管理员「调整额度-充值」录入的总金额（人民币 ¥）
	CreatedAt      int64   `json:"created_at" gorm:"autoCreateTime"`
}

// UpsertVipHourlyConsumption 批量插入/更新某些小时的统计数据。
// 同一 (user_id, stat_date, stat_hour) 已存在时覆盖（重复跑支持幂等）。
func UpsertVipHourlyConsumption(records []VipHourlyConsumption) error {
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
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "stat_date"}, {Name: "stat_hour"}},
		DoUpdates: clause.AssignmentColumns([]string{"quota", "request_count", "tokens", "username", "recharge_amount"}),
	}).Create(&records).Error
}

// HourlyConsumptionEntry 一个客户某小时的指标
type HourlyConsumptionEntry struct {
	Quota        int64
	RequestCount int64
	Tokens       int64
}

// SumVipHourlyByDay 给定 userIds + 日期区间，按天汇总 quota（消耗指标，单位仍是 quota）。
// 返回 map[date]quota。专供趋势图「按天」x 轴使用。
//
// 注：这里复用 vip_hourly_consumption 表按 stat_date 汇总，结果等同于 vip_daily_consumption；
// 之所以走小时表是为了支持「时段截取」—— 比如自定义某天只取 09:00~18:00 这一段。
// 如果起止时间正好覆盖整天 [00:00, 23:00]，等价于直接读 daily 表。
func SumVipHourlyByDay(userIds []int, startDate, endDate string, startHour, endHour int) (map[string]int64, error) {
	result := make(map[string]int64)
	if len(userIds) == 0 {
		return result, nil
	}
	type row struct {
		StatDate string
		Total    int64
	}
	var rows []row
	tx := DB.Model(&VipHourlyConsumption{}).
		Where("user_id IN ?", userIds).
		Where("stat_date >= ? AND stat_date <= ?", startDate, endDate)
	if startHour > 0 || endHour < 23 {
		tx = tx.Where("stat_hour >= ? AND stat_hour <= ?", startHour, endHour)
	}
	err := tx.
		Select("stat_date, COALESCE(SUM(quota), 0) AS total").
		Group("stat_date").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.StatDate] = r.Total
	}
	return result, nil
}

// SumVipHourlyByHour 给定 userIds + 时间起止（精确到小时），按 (date, hour) 汇总 quota。
// 返回 map[YYYY-MM-DD#hh]quota。专供趋势图「按小时」x 轴使用。
func SumVipHourlyByHour(userIds []int, startDate, endDate string, startHour, endHour int) (map[string]int64, error) {
	result := make(map[string]int64)
	if len(userIds) == 0 {
		return result, nil
	}
	type row struct {
		StatDate string
		StatHour int
		Total    int64
	}
	var rows []row
	tx := DB.Model(&VipHourlyConsumption{}).
		Where("user_id IN ?", userIds).
		Where("stat_date >= ? AND stat_date <= ?", startDate, endDate)
	if startHour > 0 || endHour < 23 {
		tx = tx.Where("stat_hour >= ? AND stat_hour <= ?", startHour, endHour)
	}
	err := tx.
		Select("stat_date, stat_hour, COALESCE(SUM(quota), 0) AS total").
		Group("stat_date, stat_hour").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		key := hourBucketKey(r.StatDate, r.StatHour)
		result[key] = r.Total
	}
	return result, nil
}

// hourBucketKey 把 (date, hour) 拼成 trend 图 x 轴的 bucket key（"YYYY-MM-DD#HH"）
func hourBucketKey(date string, hour int) string {
	return fmt.Sprintf("%s#%02d", date, hour)
}
