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

// ChannelPromotionRow 渠道推广情况一行
type ChannelPromotionRow struct {
	Channel       string  `json:"channel"`        // 渠道名 (users.business_channel)
	InvitedCount  int     `json:"invited_count"`  // 该渠道下所有销售在时间窗口内邀请到的用户数
	TotalConsumed int64   `json:"total_consumed"` // 这些被邀请用户在时间窗口内的消耗 quota（净口径）
	TotalRecharge float64 `json:"total_recharge"` // 这些被邀请用户在时间窗口内的管理员充值金额（¥）
}

// SalesPromotionRow 销售推广情况一行
type SalesPromotionRow struct {
	Username      string  `json:"username"`       // 销售用户名
	Channel       string  `json:"channel"`        // 归属渠道
	InvitedCount  int     `json:"invited_count"`  // 该销售在时间窗口内邀请到的用户数
	TotalConsumed int64   `json:"total_consumed"` // 这些被邀请用户在时间窗口内的消耗 quota（净口径）
	TotalRecharge float64 `json:"total_recharge"` // 这些被邀请用户在时间窗口内的管理员充值金额（¥）
}

// PromotionStatsResp 推广统计接口返回
type PromotionStatsResp struct {
	Channels []ChannelPromotionRow `json:"channels"` // 已按 TotalConsumed 倒序
	Sales    []SalesPromotionRow   `json:"sales"`    // 已按 TotalConsumed 倒序
}

// GetPromotionStats 计算渠道 / 销售推广情况，返回消耗（净口径）+ 管理员充值金额（¥）。
//
// 数据源采用"历史读汇总表 + 今天补 logs"混合模式，避免直接对 logs 做全时间段扫描：
//   - 历史段（<= 昨天 且 落在 [startTs, endTs] 内）→ vip_daily_consumptions
//     (quota / recharge_amount，按 stat_date 过滤，走索引)
//   - 今天段（[max(todayStart, startTs), min(now, endTs)]）→ logs
//     (net quota / recharge_input_amount，按 created_at 过滤)
//
// startTs / endTs 为 0 表示不限。
func GetPromotionStats(startTs, endTs int64) (*PromotionStatsResp, error) {
	// 1. 拉取所有销售：business_channel != ''
	type salesUser struct {
		Id              int
		Username        string
		BusinessChannel string
	}
	var sales []salesUser
	if err := DB.Model(&User{}).
		Select("id, username, business_channel").
		Where("business_channel <> ''").
		Find(&sales).Error; err != nil {
		return nil, err
	}

	resp := &PromotionStatsResp{
		Channels: []ChannelPromotionRow{},
		Sales:    []SalesPromotionRow{},
	}
	if len(sales) == 0 {
		return resp, nil
	}

	salesIds := make([]int, 0, len(sales))
	salesById := make(map[int]salesUser, len(sales))
	for _, s := range sales {
		salesIds = append(salesIds, s.Id)
		salesById[s.Id] = s
	}

	// 2a. 时间窗口内创建的被邀请用户 → 仅用于 InvitedCount
	type invitee struct {
		Id        int
		InviterId int
	}
	var newInvitees []invitee
	tx := DB.Model(&User{}).
		Select("id, inviter_id").
		Where("inviter_id IN ?", salesIds)
	if startTs > 0 {
		tx = tx.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		tx = tx.Where("created_at <= ?", endTs)
	}
	if err := tx.Find(&newInvitees).Error; err != nil {
		return nil, err
	}

	// 2b. 所有历史被邀请用户 → 用于消耗 / 充值聚合（不限创建时间）
	var allInvitees []invitee
	if err := DB.Model(&User{}).
		Select("id, inviter_id").
		Where("inviter_id IN ?", salesIds).
		Find(&allInvitees).Error; err != nil {
		return nil, err
	}

	// 3. 拆窗口：历史段 [histStartDate, histEndDate] + 今天段 [todaySegStart, todaySegEnd]
	allInviteeIds := make([]int, 0, len(allInvitees))
	inviterByUser := make(map[int]int, len(allInvitees))
	for _, u := range allInvitees {
		allInviteeIds = append(allInviteeIds, u.Id)
		inviterByUser[u.Id] = u.InviterId
	}

	consumedByUser := map[int]int64{}
	rechargeByUser := map[int]float64{}

	if len(allInviteeIds) > 0 {
		now := time.Now()
		loc := now.Location()
		todayStr := now.Format("2006-01-02")
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()

		// 计算历史段日期范围：[startDate, min(endDate, yesterday)]
		// startTs=0 视为 "不设下界"，endTs=0 视为 "不设上界"。
		var histStartDate, histEndDate string
		if startTs > 0 {
			histStartDate = time.Unix(startTs, 0).In(loc).Format("2006-01-02")
		}
		if endTs > 0 {
			endDateStr := time.Unix(endTs, 0).In(loc).Format("2006-01-02")
			// 只取 <= 昨天的部分给历史段
			if endDateStr < todayStr {
				histEndDate = endDateStr
			} else {
				histEndDate = now.AddDate(0, 0, -1).Format("2006-01-02")
			}
		} else {
			histEndDate = now.AddDate(0, 0, -1).Format("2006-01-02")
		}
		hasHist := histEndDate != "" && (histStartDate == "" || histStartDate <= histEndDate)

		// 计算今天段：[max(todayStart, startTs), min(now, endTs)]
		todaySegStart := todayStart
		if startTs > todaySegStart {
			todaySegStart = startTs
		}
		todaySegEnd := now.Unix()
		if endTs > 0 && endTs < todaySegEnd {
			todaySegEnd = endTs
		}
		hasToday := todaySegEnd >= todayStart && todaySegStart <= todaySegEnd

		// 3a. 历史段：vip_daily_consumptions 聚合（quota + recharge_amount 一次拿）
		if hasHist {
			type histRow struct {
				UserId    int
				Quota     int64
				Recharge  float64
				RowCount  int64
			}
			var rows []histRow
			histTx := DB.Model(&VipDailyConsumption{}).
				Where("user_id IN ?", allInviteeIds).
				Where("stat_date <= ?", histEndDate)
			if histStartDate != "" {
				histTx = histTx.Where("stat_date >= ?", histStartDate)
			}
			if err := histTx.
				Select("user_id, " +
					"COALESCE(SUM(quota), 0) AS quota, " +
					"COALESCE(SUM(recharge_amount), 0) AS recharge, " +
					"COUNT(*) AS row_count").
				Group("user_id").
				Scan(&rows).Error; err != nil {
				return nil, err
			}
			for _, r := range rows {
				consumedByUser[r.UserId] += r.Quota
				rechargeByUser[r.UserId] += r.Recharge
			}
		}

		// 3b. 今天段：logs 表实时聚合（消耗净口径 + 充值）
		if hasToday {
			// 消耗（净口径 = LogTypeConsume 计正 + LogTypeRefund 计负）
			type consumeRow struct {
				UserId int
				Total  int64
			}
			var consumeRows []consumeRow
			if err := LOG_DB.Model(&Log{}).
				Where("type IN ?", NetQuotaSumTypes()).
				Where("user_id IN ?", allInviteeIds).
				Where("created_at >= ? AND created_at <= ?", todaySegStart, todaySegEnd).
				Select("user_id, " + NetQuotaSumExpr() + " AS total").
				Group("user_id").
				Scan(&consumeRows).Error; err != nil {
				return nil, err
			}
			for _, r := range consumeRows {
				consumedByUser[r.UserId] += r.Total
			}
			// 充值
			type rechargeRow struct {
				UserId        int
				TotalRecharge float64
			}
			var rechargeRows []rechargeRow
			if err := LOG_DB.Model(&Log{}).
				Where("type = ?", LogTypeManage).
				Where("operation_type = ?", OperationTypeQuota).
				Where("quota_type = ?", QuotaTypeRecharge).
				Where("user_id IN ?", allInviteeIds).
				Where("created_at >= ? AND created_at <= ?", todaySegStart, todaySegEnd).
				Select("user_id, COALESCE(SUM(recharge_input_amount), 0) AS total_recharge").
				Group("user_id").
				Scan(&rechargeRows).Error; err != nil {
				return nil, err
			}
			for _, r := range rechargeRows {
				rechargeByUser[r.UserId] += r.TotalRecharge
			}
		}
	}

	// 4. 内存聚合 → 销售维度
	salesAgg := make(map[int]*SalesPromotionRow, len(sales))
	for _, s := range sales {
		salesAgg[s.Id] = &SalesPromotionRow{
			Username: s.Username,
			Channel:  s.BusinessChannel,
		}
	}
	// InvitedCount：只统计窗口内新增的被邀请用户
	for _, u := range newInvitees {
		if row, ok := salesAgg[u.InviterId]; ok {
			row.InvitedCount++
		}
	}
	// TotalConsumed / TotalRecharge：累加所有历史被邀请用户在窗口内的消耗与充值
	for userId, q := range consumedByUser {
		inviterId := inviterByUser[userId]
		if row, ok := salesAgg[inviterId]; ok {
			row.TotalConsumed += q
		}
	}
	for userId, r := range rechargeByUser {
		inviterId := inviterByUser[userId]
		if row, ok := salesAgg[inviterId]; ok {
			row.TotalRecharge += r
		}
	}

	// 5. 内存聚合 → 渠道维度
	channelAgg := map[string]*ChannelPromotionRow{}
	for _, s := range sales {
		row, ok := channelAgg[s.BusinessChannel]
		if !ok {
			row = &ChannelPromotionRow{Channel: s.BusinessChannel}
			channelAgg[s.BusinessChannel] = row
		}
		// 把销售自己的统计聚合进渠道
		if sRow, ok2 := salesAgg[s.Id]; ok2 {
			row.InvitedCount += sRow.InvitedCount
			row.TotalConsumed += sRow.TotalConsumed
			row.TotalRecharge += sRow.TotalRecharge
		}
	}

	// 6. 输出：按总消耗倒序
	for _, row := range channelAgg {
		resp.Channels = append(resp.Channels, *row)
	}
	sort.SliceStable(resp.Channels, func(i, j int) bool {
		return resp.Channels[i].TotalConsumed > resp.Channels[j].TotalConsumed
	})
	for _, row := range salesAgg {
		resp.Sales = append(resp.Sales, *row)
	}
	sort.SliceStable(resp.Sales, func(i, j int) bool {
		return resp.Sales[i].TotalConsumed > resp.Sales[j].TotalConsumed
	})

	return resp, nil
}
