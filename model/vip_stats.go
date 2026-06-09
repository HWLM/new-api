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

import "time"

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
