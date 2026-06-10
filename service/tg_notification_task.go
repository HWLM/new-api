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
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// 用 options 表存储的 key
const (
	OptionKeyTgBotToken         = "tg_notify_bot_token"
	OptionKeyTgChatId           = "tg_notify_chat_id"
	OptionKeyTgLastReportDate   = "tg_notify_last_report_date" // 形如 "2026-06-09"
	OptionKeyTgRetryCountDate   = "tg_notify_retry_date"       // 形如 "2026-06-09|2"，记录当天重试次数
	OptionKeyTgFrontendBaseUrl  = "tg_notify_frontend_base_url" // 形如 "https://aiyunrouter.com/"，用于消耗明细链接
	VipStatsDetailPathFromBase  = "vip-stats"                   // 与前端公开路由 /vip-stats 对应
)

const (
	tgNotifyTickInterval = 1 * time.Minute
	tgNotifyReportHour   = 8 // 每天本地时间 8 点播报
	tgNotifyMaxRetryDay  = 3 // 单日最多重试次数（含首次）
)

var (
	tgNotifyOnce    sync.Once
	tgNotifyRunning atomic.Bool
)

// StartTgNotificationTask 启动 TG 重点客户每日播报任务（仅 master 节点执行）。
func StartTgNotificationTask() {
	tgNotifyOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			common.SysLog(fmt.Sprintf("tg notification task started: tick=%s, report_hour=%d", tgNotifyTickInterval, tgNotifyReportHour))
			ticker := time.NewTicker(tgNotifyTickInterval)
			defer ticker.Stop()

			// 启动时立即尝试一次：覆盖"今天 8 点已过、但服务此前未运行"场景（Q6 重启补跑）
			runTgNotifyIfDue()
			for range ticker.C {
				runTgNotifyIfDue()
			}
		})
	})
}

// runTgNotifyIfDue tick 回调。多重保护：
//   - 同一进程不重入（atomic flag）
//   - 同一天不重发（lastDate）
//   - 失败有限重试（单日 ≤ tgNotifyMaxRetryDay 次）
func runTgNotifyIfDue() {
	if !tgNotifyRunning.CompareAndSwap(false, true) {
		return
	}
	defer tgNotifyRunning.Store(false)

	now := time.Now()
	today := now.Format("2006-01-02")

	if model.GetOptionString(OptionKeyTgLastReportDate) == today {
		return // 今天已经成功播报过
	}
	if now.Hour() < tgNotifyReportHour {
		return // 还没到 8 点
	}

	// 检查当天重试次数（Q5: 单日最多 3 次）
	retried := readTodayRetryCount(today)
	if retried >= tgNotifyMaxRetryDay {
		// 当天已重试到上限：标记 lastDate 跳过，避免每分钟都做无效计算
		_ = model.UpdateOption(OptionKeyTgLastReportDate, today)
		common.SysError(fmt.Sprintf("tg notify reached daily retry limit (%d), skip %s", tgNotifyMaxRetryDay, today))
		return
	}

	if err := executeTgNotifyOnce(); err != nil {
		// 失败：递增重试计数，不更新 lastDate，下次 tick 还会重试
		writeTodayRetryCount(today, retried+1)
		common.SysError(fmt.Sprintf("tg notify failed (attempt %d/%d): %s", retried+1, tgNotifyMaxRetryDay, err.Error()))
		return
	}
	_ = model.UpdateOption(OptionKeyTgLastReportDate, today)
}

// executeTgNotifyOnce 真正执行一次播报。返回 nil 表示成功（或语义性"无需发送"）。
//
// Q3: token/chatId 任一为空 → 跳过整个播报
// Q4: 重点客户人数为 0 → 跳过不发，但语义上算作"已完成"
func executeTgNotifyOnce() error {
	token := model.GetOptionString(OptionKeyTgBotToken)
	chatId := model.GetOptionString(OptionKeyTgChatId)
	if token == "" || chatId == "" {
		return nil // 视为成功，避免无意义重试
	}

	stat, err := model.CollectVipStat()
	if err != nil {
		return fmt.Errorf("collect vip stat: %w", err)
	}
	if stat.UserCount == 0 {
		return nil // 没有重点客户，跳过不发（Q4 选项 B）
	}

	weekly, err := model.SumWeeklyConsumedRealtime()
	if err != nil {
		return fmt.Errorf("collect weekly consumed: %w", err)
	}

	text := BuildVipReportText(stat, weekly)
	return SendTelegramMessage(token, chatId, text)
}

// BuildVipReportText 把统计结果拼成 TG 消息文本（Q1: USD $X.XXXX 格式）。
// 如果 options 表里配置了 frontend base url，会在末尾附消耗明细页 URL。
func BuildVipReportText(stat *model.VipStat, weeklyConsumed int64) string {
	body := fmt.Sprintf(
		"统计用户数：%d\n近7天累计消耗金额：$%.4f\n前一日累计消耗金额：$%.4f\n当前累计剩余余额：$%.4f",
		stat.UserCount,
		float64(weeklyConsumed)/common.QuotaPerUnit,
		float64(stat.YesterdayConsumed)/common.QuotaPerUnit,
		float64(stat.CurrentRemaining)/common.QuotaPerUnit,
	)
	if url := buildVipStatsDetailUrl(); url != "" {
		body += "\n消耗明细：" + url
	}
	return body
}

// buildVipStatsDetailUrl 拼明细页 URL。base 未配置时返回空串（不放进消息）
func buildVipStatsDetailUrl() string {
	base := model.GetOptionString(OptionKeyTgFrontendBaseUrl)
	if base == "" {
		return ""
	}
	// 兼容用户配置时是否带尾 "/"
	if base[len(base)-1] != '/' {
		base += "/"
	}
	return base + VipStatsDetailPathFromBase
}

// TriggerVipReportManually 由 admin API 调用：忽略时间窗口和 lastDate，立即发一次。
// 配置缺失或没有重点客户时返回明确错误，便于排查。
func TriggerVipReportManually() error {
	token := model.GetOptionString(OptionKeyTgBotToken)
	chatId := model.GetOptionString(OptionKeyTgChatId)
	if token == "" || chatId == "" {
		return errors.New("TG 设置未配置（bot token 或 chat id 为空）")
	}
	stat, err := model.CollectVipStat()
	if err != nil {
		return fmt.Errorf("收集统计失败：%w", err)
	}
	weekly, err := model.SumWeeklyConsumedRealtime()
	if err != nil {
		return fmt.Errorf("收集 7 天累计失败：%w", err)
	}
	text := BuildVipReportText(stat, weekly)
	return SendTelegramMessage(token, chatId, text)
}

// 重试计数以 "date|count" 格式存到 options，避免新建表
func readTodayRetryCount(today string) int {
	raw := model.GetOptionString(OptionKeyTgRetryCountDate)
	if raw == "" {
		return 0
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] == '|' {
			date := raw[:i]
			if date != today {
				return 0 // 不是今天，归零
			}
			n, _ := strconv.Atoi(raw[i+1:])
			return n
		}
	}
	return 0
}

func writeTodayRetryCount(today string, n int) {
	_ = model.UpdateOption(OptionKeyTgRetryCountDate, today+"|"+strconv.Itoa(n))
}
