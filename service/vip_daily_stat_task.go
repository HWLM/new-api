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
package service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	OptionKeyVipStatLastDate = "vip_stat_last_date" // 形如 "2026-06-10"，表示最近一次成功统计的"统计日"

	vipStatTickInterval = 1 * time.Minute
	// 每天本地时间 00:10 跑「昨天」统计。
	// 选这个点的原因：跨日后尽快完成 daily_summary 写入，
	// 让数据看板「较昨日」的卡片对比在跨日后 10 分钟内即可正常显示，避免空窗。
	vipStatReportHour   = 0
	vipStatReportMinute = 10
)

var (
	vipStatOnce    sync.Once
	vipStatRunning atomic.Bool
)

// StartVipDailyStatTask 启动 VIP 每日消耗统计任务（仅 master 节点执行）。
// 每天本地时间 00:10（vipStatReportHour/Minute）后第一次 tick 时聚合"昨天"的消耗写入 vip_daily_consumption 表。
// 失败时不更新 lastDate，下次 tick 重试；重启时若今天触发点已过且未统计，立即补跑。
func StartVipDailyStatTask() {
	vipStatOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			common.SysLog(fmt.Sprintf("vip daily stat task started: tick=%s, trigger=%02d:%02d", vipStatTickInterval, vipStatReportHour, vipStatReportMinute))
			ticker := time.NewTicker(vipStatTickInterval)
			defer ticker.Stop()

			runVipStatIfDue()
			for range ticker.C {
				runVipStatIfDue()
			}
		})
	})
}

func runVipStatIfDue() {
	if !vipStatRunning.CompareAndSwap(false, true) {
		return
	}
	defer vipStatRunning.Store(false)

	now := time.Now()
	today := now.Format("2006-01-02")

	if model.GetOptionString(OptionKeyVipStatLastDate) == today {
		return
	}
	// 还没到当天的触发点（默认 00:10）
	if now.Hour() < vipStatReportHour ||
		(now.Hour() == vipStatReportHour && now.Minute() < vipStatReportMinute) {
		return
	}

	// 跑昨天
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	n, err := model.RunVipDailyStat(yesterday)
	if err != nil {
		common.SysError(fmt.Sprintf("vip daily stat failed for %s: %s", yesterday, err.Error()))
		return // 不标记 lastDate，下次 tick 重试
	}
	common.SysLog(fmt.Sprintf("vip daily stat done: date=%s rows=%d", yesterday, n))
	_ = model.UpdateOption(OptionKeyVipStatLastDate, today)
}

// BackfillVipDailyStats 手动回填指定天数。days=7 表示回填昨天往前 7 天（不含今天）。
//
// 重要：**正序**回填（从最早一天往最近一天），保证 daily_summary 的累计列
// (cum_quota / cum_recharge_amount / cum_official_*) 能正确衔接 —— 因为 writeDailySummary
// 通过 GetPrevDailySummary 取 stat_date < 当前日期的最近一条作为累计基线。
//
// 注意：被聚合的"VIP 客户"是当前时刻的 VIP，不是历史 VIP（业务无历史快照）。
func BackfillVipDailyStats(days int) (map[string]int, error) {
	if days <= 0 || days > 90 {
		return nil, fmt.Errorf("days 必须在 1..90 之间，传入：%d", days)
	}
	now := time.Now()
	result := make(map[string]int)
	// 从 days 天前开始正序往前推到昨天
	for i := days; i >= 1; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		n, err := model.RunVipDailyStat(date)
		if err != nil {
			return result, fmt.Errorf("backfill %s failed: %w", date, err)
		}
		result[date] = n
	}
	return result, nil
}
