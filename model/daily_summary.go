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
	"strings"

	"gorm.io/gorm/clause"
)

// OptionKeyOfficialUserGroups 正式用户分组配置 key。
// 值为逗号分隔的 group 名称列表（如 "vip,svip"）；为空时正式用户列表为空。
const OptionKeyOfficialUserGroups = "OfficialUserGroups"

// DailySummary 全局每日汇总（一天一条记录）。
// 由 daily 定时任务在 vip_daily_consumption 写入之后顺带写入。
// 6 个当天列 + 4 个累计列：累计列 = 上一条记录的累计列 + 当天对应值。
// 即使当天所有指标为 0 也写入一条，保证累计列连续。
type DailySummary struct {
	Id                     int     `json:"id" gorm:"primaryKey"`
	StatDate               string  `json:"stat_date" gorm:"type:varchar(10);uniqueIndex:uk_daily_summary_stat_date"`  // YYYY-MM-DD
	Quota                  int64   `json:"quota" gorm:"default:0"`                                                    // 当天全量用户总消耗 quota
	RequestCount           int64   `json:"request_count" gorm:"default:0;column:request_count"`                       // 当天全量请求次数
	Tokens                 int64   `json:"tokens" gorm:"default:0"`                                                   // 当天全量 prompt_tokens + completion_tokens
	RechargeAmount         float64 `json:"recharge_amount" gorm:"default:0;column:recharge_amount"`                   // 当天全量管理员充值金额（人民币 ¥）
	OfficialQuota          int64   `json:"official_quota" gorm:"default:0;column:official_quota"`                     // 当天正式用户消耗 quota
	OfficialRechargeAmount float64 `json:"official_recharge_amount" gorm:"default:0;column:official_recharge_amount"` // 当天正式用户充值金额（人民币 ¥）
	// 累计列：截至 stat_date 当日（含当日）的累加值
	CumQuota                  int64   `json:"cum_quota" gorm:"default:0;column:cum_quota"`
	CumRechargeAmount         float64 `json:"cum_recharge_amount" gorm:"default:0;column:cum_recharge_amount"`
	CumOfficialQuota          int64   `json:"cum_official_quota" gorm:"default:0;column:cum_official_quota"`
	CumOfficialRechargeAmount float64 `json:"cum_official_recharge_amount" gorm:"default:0;column:cum_official_recharge_amount"`
	CreatedAt                 int64   `json:"created_at" gorm:"autoCreateTime"`
}

// UpsertDailySummary 按 stat_date 唯一索引插入/更新当日汇总。重复跑同一天会覆盖所有可变列。
func UpsertDailySummary(s *DailySummary) error {
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "stat_date"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"quota", "request_count", "tokens", "recharge_amount",
			"official_quota", "official_recharge_amount",
			"cum_quota", "cum_recharge_amount",
			"cum_official_quota", "cum_official_recharge_amount",
		}),
	}).Create(s).Error
}

// GetPrevDailySummary 取 stat_date < 给定日期的最近一条记录，用于累计列起算基线。
// 不存在更早记录时返回零值结构体（不报错）—— 首次运行或最早一天用 0 起算。
func GetPrevDailySummary(statDate string) (DailySummary, error) {
	var rows []DailySummary
	err := DB.Model(&DailySummary{}).
		Where("stat_date < ?", statDate).
		Order("stat_date desc").
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return DailySummary{}, err
	}
	if len(rows) == 0 {
		return DailySummary{}, nil
	}
	return rows[0], nil
}

// GetOfficialUserIds 解析 options 中 OfficialUserGroups（逗号分隔），返回所有命中 group 的 user_id。
// key 不存在或值为空 / 全空白时返回 nil（视为没有正式用户，正式用户当天指标即为 0）。
func GetOfficialUserIds() ([]int, error) {
	raw := GetOptionString(OptionKeyOfficialUserGroups)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	groups := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			groups = append(groups, p)
		}
	}
	if len(groups) == 0 {
		return nil, nil
	}
	var ids []int
	if err := DB.Model(&User{}).
		Where(commonGroupCol+" IN ?", groups).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}
