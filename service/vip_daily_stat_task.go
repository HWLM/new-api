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
	vipStatReportHour   = 2 // 每天本地时间 2 点跑昨天统计
)

var (
	vipStatOnce    sync.Once
	vipStatRunning atomic.Bool
)

// StartVipDailyStatTask 启动 VIP 每日消耗统计任务（仅 master 节点执行）。
// 每天本地时间 02:00 后第一次 tick 时聚合"昨天"的消耗写入 vip_daily_consumption 表。
// 失败时不更新 lastDate，下次 tick 重试；重启时若今天 2 点已过且未统计，立即补跑。
func StartVipDailyStatTask() {
	vipStatOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			common.SysLog(fmt.Sprintf("vip daily stat task started: tick=%s, report_hour=%d", vipStatTickInterval, vipStatReportHour))
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
	if now.Hour() < vipStatReportHour {
		return // 还没到 2 点
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
// 注意：被聚合的"VIP 客户"是当前时刻的 VIP，不是历史 VIP（业务无历史快照）。
func BackfillVipDailyStats(days int) (map[string]int, error) {
	if days <= 0 || days > 90 {
		return nil, fmt.Errorf("days 必须在 1..90 之间，传入：%d", days)
	}
	now := time.Now()
	result := make(map[string]int)
	for i := 1; i <= days; i++ {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		n, err := model.RunVipDailyStat(date)
		if err != nil {
			return result, fmt.Errorf("backfill %s failed: %w", date, err)
		}
		result[date] = n
	}
	return result, nil
}
