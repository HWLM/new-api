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
	// OptionKeyVipHourlyStatLastBucket 形如 "2026-06-17#14"，表示已成功统计的最近一个完整小时桶
	OptionKeyVipHourlyStatLastBucket = "vip_hourly_stat_last_bucket"

	vipHourlyStatTickInterval = 1 * time.Minute
	vipHourlyStatTriggerMinute = 5 // 每小时第 5 分钟跑「上一小时」
)

var (
	vipHourlyStatOnce    sync.Once
	vipHourlyStatRunning atomic.Bool
)

// StartVipHourlyStatTask 启动 VIP 每小时消耗统计任务（仅 master 节点执行）。
// 每小时 :05 后第一次 tick 时聚合「刚结束的那一小时」写入 vip_hourly_consumption 表。
// 失败时不更新 lastBucket，下次 tick 重试；重启时若当前小时 :05 已过且未统计，立即补跑。
func StartVipHourlyStatTask() {
	vipHourlyStatOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			common.SysLog(fmt.Sprintf("vip hourly stat task started: tick=%s, trigger_minute=%d", vipHourlyStatTickInterval, vipHourlyStatTriggerMinute))
			ticker := time.NewTicker(vipHourlyStatTickInterval)
			defer ticker.Stop()

			runVipHourlyStatIfDue()
			for range ticker.C {
				runVipHourlyStatIfDue()
			}
		})
	})
}

func runVipHourlyStatIfDue() {
	if !vipHourlyStatRunning.CompareAndSwap(false, true) {
		return
	}
	defer vipHourlyStatRunning.Store(false)

	now := time.Now()
	if now.Minute() < vipHourlyStatTriggerMinute {
		return // 还没到 :05
	}

	// 「上一小时」= 当前小时往前推 1 小时（取整点）
	prev := now.Add(-1 * time.Hour)
	date := prev.Format("2006-01-02")
	hour := prev.Hour()
	bucket := fmt.Sprintf("%s#%02d", date, hour)

	if model.GetOptionString(OptionKeyVipHourlyStatLastBucket) == bucket {
		return // 当前这一小时桶已经跑过
	}

	n, err := model.RunVipHourlyStat(date, hour)
	if err != nil {
		common.SysError(fmt.Sprintf("vip hourly stat failed for %s: %s", bucket, err.Error()))
		return
	}
	common.SysLog(fmt.Sprintf("vip hourly stat done: bucket=%s rows=%d", bucket, n))
	_ = model.UpdateOption(OptionKeyVipHourlyStatLastBucket, bucket)
}

// BackfillVipHourlyStats 手动回填指定起止区间的小时统计。
// 参数：startDate / endDate （YYYY-MM-DD），日期闭区间。每天回填 0~23 共 24 个小时桶。
// 注意：被聚合的"VIP 客户"是当前时刻的 VIP，不是历史 VIP。
func BackfillVipHourlyStats(startDate, endDate string) (map[string]int, error) {
	loc := time.Now().Location()
	start, err := time.ParseInLocation("2006-01-02", startDate, loc)
	if err != nil {
		return nil, fmt.Errorf("startDate 格式错误：%w", err)
	}
	end, err := time.ParseInLocation("2006-01-02", endDate, loc)
	if err != nil {
		return nil, fmt.Errorf("endDate 格式错误：%w", err)
	}
	if end.Before(start) {
		return nil, fmt.Errorf("endDate (%s) 早于 startDate (%s)", endDate, startDate)
	}
	if end.Sub(start) > 90*24*time.Hour {
		return nil, fmt.Errorf("回填区间不能超过 90 天")
	}
	result := make(map[string]int)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		for h := 0; h < 24; h++ {
			n, err := model.RunVipHourlyStat(date, h)
			if err != nil {
				return result, fmt.Errorf("backfill %s#%02d failed: %w", date, h, err)
			}
			result[fmt.Sprintf("%s#%02d", date, h)] = n
		}
	}
	return result, nil
}
