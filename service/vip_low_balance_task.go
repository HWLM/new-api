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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// 余额阈值：USD $100，对应 quota = 100 * QuotaPerUnit
	vipLowBalanceThresholdUSD = 100.0

	// options 表 key：记录上一次成功告警的 unix 秒，用于防止小时边界震荡多发
	OptionKeyVipLowBalanceLastHour = "vip_low_balance_last_hour" // 形如 "2026-06-12T15"

	vipLowBalanceTickInterval = 1 * time.Minute
)

var (
	vipLowBalanceOnce    sync.Once
	vipLowBalanceRunning atomic.Bool
)

// StartVipLowBalanceTask 启动 VIP 客户余额告警任务（仅 master 节点执行）。
// 每小时检查一次，所有重点客户中余额 < $100 的，列表非空就发 TG。
func StartVipLowBalanceTask() {
	vipLowBalanceOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			common.SysLog(fmt.Sprintf("vip low balance task started: tick=%s, threshold=$%.2f", vipLowBalanceTickInterval, vipLowBalanceThresholdUSD))
			ticker := time.NewTicker(vipLowBalanceTickInterval)
			defer ticker.Stop()

			runVipLowBalanceIfDue()
			for range ticker.C {
				runVipLowBalanceIfDue()
			}
		})
	})
}

// runVipLowBalanceIfDue 每分钟 tick：当本地小时跟上次记录的小时不同时执行一次。
// 这样每小时只跑一次（凌晨 0、1、2...23 点各一次），避免重启反复发送。
func runVipLowBalanceIfDue() {
	if !vipLowBalanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer vipLowBalanceRunning.Store(false)

	now := time.Now()
	curHour := now.Format("2006-01-02T15")

	if model.GetOptionString(OptionKeyVipLowBalanceLastHour) == curHour {
		return // 当前自然小时已跑过
	}

	if err := executeVipLowBalanceOnce(); err != nil {
		common.SysError(fmt.Sprintf("vip low balance check failed: %s", err.Error()))
		return // 不更新 lastHour，下次 tick 重试
	}
	_ = model.UpdateOption(OptionKeyVipLowBalanceLastHour, curHour)
}

// executeVipLowBalanceOnce 执行一次告警检查。
// 没有 token/chat_id 配置 → 静默跳过（保持跟 8 点播报一致行为）
// 没有客户余额 < 阈值 → 不发任何消息（Q3=A）
func executeVipLowBalanceOnce() error {
	token := model.GetOptionString(OptionKeyTgBotToken)
	chatId := model.GetOptionString(OptionKeyTgChatId)
	if token == "" || chatId == "" {
		return nil
	}

	threshold := int64(vipLowBalanceThresholdUSD * common.QuotaPerUnit)
	users, err := model.GetVipUsersBelowBalance(threshold)
	if err != nil {
		return fmt.Errorf("query vip users below balance: %w", err)
	}
	if len(users) == 0 {
		return nil // 没有告警客户，跳过
	}

	text := BuildVipLowBalanceText(users)
	return SendTelegramMessage(token, chatId, text)
}

// BuildVipLowBalanceText 拼告警消息文本（Q7=A，仅显示 username）
func BuildVipLowBalanceText(users []model.User) string {
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Username)
	}
	return fmt.Sprintf(
		"【余额告警】余额不足%.0f\n  重点客户：%s",
		vipLowBalanceThresholdUSD,
		strings.Join(names, ","),
	)
}

// TriggerVipLowBalanceManually 手动触发余额告警（admin API）。
// 忽略小时去重和阈值检查，只要有客户余额 < 阈值就立即发；列表为空时返回明确错误便于排查。
func TriggerVipLowBalanceManually() error {
	token := model.GetOptionString(OptionKeyTgBotToken)
	chatId := model.GetOptionString(OptionKeyTgChatId)
	if token == "" || chatId == "" {
		return fmt.Errorf("TG 设置未配置（bot token 或 chat id 为空）")
	}

	threshold := int64(vipLowBalanceThresholdUSD * common.QuotaPerUnit)
	users, err := model.GetVipUsersBelowBalance(threshold)
	if err != nil {
		return fmt.Errorf("查询低余额客户失败：%w", err)
	}
	if len(users) == 0 {
		return fmt.Errorf("当前没有余额低于 $%.0f 的重点客户", vipLowBalanceThresholdUSD)
	}

	text := BuildVipLowBalanceText(users)
	return SendTelegramMessage(token, chatId, text)
}
