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
)

// ChannelPromotionRow 渠道推广情况一行
type ChannelPromotionRow struct {
	Channel       string `json:"channel"`        // 渠道名 (users.business_channel)
	InvitedCount  int    `json:"invited_count"`  // 该渠道下所有销售在时间窗口内邀请到的用户数
	TotalConsumed int64  `json:"total_consumed"` // 这些被邀请用户在时间窗口内的 logs 消耗 quota
}

// SalesPromotionRow 销售推广情况一行
type SalesPromotionRow struct {
	Username      string `json:"username"`       // 销售用户名
	Channel       string `json:"channel"`        // 归属渠道
	InvitedCount  int    `json:"invited_count"`  // 该销售在时间窗口内邀请到的用户数
	TotalConsumed int64  `json:"total_consumed"` // 这些被邀请用户在时间窗口内的 logs 消耗 quota
}

// PromotionStatsResp 推广统计接口返回
type PromotionStatsResp struct {
	Channels []ChannelPromotionRow `json:"channels"` // 已按 TotalConsumed 倒序
	Sales    []SalesPromotionRow   `json:"sales"`    // 已按 TotalConsumed 倒序
}

// GetPromotionStats 计算渠道 / 销售 推广情况（按 Q3 倒序、Q1=C 时间窗口同时约束邀请人创建时间和消耗 logs）。
//   - startTs / endTs: 时间窗口 (unix 秒)
//   - 调用方按 Q3 截取前 N 行
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

	// 2. 在时间窗口内创建的、且 inviter_id 属于销售群 的用户
	type invitee struct {
		Id        int
		InviterId int
	}
	var invitees []invitee
	tx := DB.Model(&User{}).
		Select("id, inviter_id").
		Where("inviter_id IN ?", salesIds)
	if startTs > 0 {
		tx = tx.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		tx = tx.Where("created_at <= ?", endTs)
	}
	if err := tx.Find(&invitees).Error; err != nil {
		return nil, err
	}

	// 3. 时间窗口内 logs 表按 user_id 聚合消耗（被邀请用户）
	inviteeIds := make([]int, 0, len(invitees))
	for _, u := range invitees {
		inviteeIds = append(inviteeIds, u.Id)
	}
	consumedByUser := map[int]int64{}
	if len(inviteeIds) > 0 {
		type logRow struct {
			UserId int
			Total  int64
		}
		var rows []logRow
		logTx := LOG_DB.Model(&Log{}).
			Where("type = ?", LogTypeConsume).
			Where("user_id IN ?", inviteeIds)
		if startTs > 0 {
			logTx = logTx.Where("created_at >= ?", startTs)
		}
		if endTs > 0 {
			logTx = logTx.Where("created_at <= ?", endTs)
		}
		if err := logTx.
			Select("user_id, COALESCE(SUM(quota), 0) AS total").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			consumedByUser[r.UserId] = r.Total
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
	for _, u := range invitees {
		row, ok := salesAgg[u.InviterId]
		if !ok {
			continue
		}
		row.InvitedCount++
		row.TotalConsumed += consumedByUser[u.Id]
	}

	// 5. 内存聚合 → 渠道维度
	channelAgg := map[string]*ChannelPromotionRow{}
	for _, s := range sales {
		row, ok := channelAgg[s.BusinessChannel]
		if !ok {
			row = &ChannelPromotionRow{Channel: s.BusinessChannel}
			channelAgg[s.BusinessChannel] = row
		}
		// 把销售自己的统计聚合进渠道（销售 X 邀请 5 人消耗 1000 → 渠道 +5/+1000）
		if sRow, ok2 := salesAgg[s.Id]; ok2 {
			row.InvitedCount += sRow.InvitedCount
			row.TotalConsumed += sRow.TotalConsumed
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
