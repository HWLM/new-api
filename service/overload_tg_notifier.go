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
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// 过载类告警（并发超限 / 拒答降级）发送到 TG 群的服务。
//
// 两个告警独立开关：
//   - OptionKeyTgNotifyConcurrencyEnabled 控制并发超限告警
//   - OptionKeyTgNotifyRefusalDemoteEnabled 控制拒答降级告警
//
// 接收群优先取 OptionKeyTgOverloadChatId，为空则回落到 OptionKeyTgChatId（VIP 日报群）。
//
// 节流策略：
//   - 并发超限：同一用户 5 分钟内只发一次（防止异常用户刷屏）
//   - 拒答降级：窗口 = DemoteSeconds，一次降级生命周期内只报一条
//
// 调用时机：均由 Distribute 中间件在决定「重导兜底 or 429」后触发，
// 消息里据此明确提示「已重导到渠道 X」或「无可用兜底渠道」。
const (
	// overloadConcurrencyThrottleWindow 并发超限告警的单用户节流窗口。
	// 这个窗口内同一个 user 只会生一条 TG。
	overloadConcurrencyThrottleWindow = 5 * time.Minute

	overloadConcurrencyThrottleKeyPrefix = "overload:tg:concurrency:"
	overloadRefusalThrottleKeyPrefix     = "overload:tg:refusal:"
	overloadTgRedisOpTimeoutMs           = 100
)

// Pretty 名字从 UserBase.Username 读；取不到就显式写 id。
type overloadNotifyUser struct {
	ID       int
	Username string
}

// OverloadFallbackOutcome 描述 Distribute 中间件对本次「超限/降级请求」给出的最终路由决策。
// 一次只会命中其中一个取值：
//   - OverloadFallbackRedirected  已经重导到兜底渠道（FallbackChannelID 填已切换到的渠道 ID）
//   - OverloadFallbackUnavailable 原渠道未配置兜底 / 自环 / 禁用 / 不支持模型 → 直接 429
type OverloadFallbackOutcome int

const (
	OverloadFallbackRedirected OverloadFallbackOutcome = iota
	OverloadFallbackUnavailable
)

// NotifyConcurrencyOverload 异步发送「并发超限」告警。
// 由 Distribute 中间件在确定「重导/拒绝」结果后调用，消息里据此拼「已重导渠道 ID」
// 或「无可用兜底渠道」。开关关闭、配置缺失、节流命中 → 静默跳过。
func NotifyConcurrencyOverload(userID, currentCount, limit, originalChannelID, fallbackChannelID int, outcome OverloadFallbackOutcome) {
	if userID <= 0 {
		return
	}
	gopool.Go(func() {
		if !isTgOverloadEnabled(OptionKeyTgNotifyConcurrencyEnabled) {
			return
		}
		if !tryAcquireConcurrencyThrottle(userID) {
			return
		}
		token, chatId := resolveOverloadTgTarget()
		if token == "" || chatId == "" {
			return
		}
		user := resolveOverloadUser(userID)
		text := buildConcurrencyOverloadText(user, currentCount, limit, originalChannelID, fallbackChannelID, outcome)
		if err := SendTelegramMessageNoPreview(token, chatId, text); err != nil {
			common.SysError(fmt.Sprintf("overload tg notify (concurrency) failed user=%d: %v", userID, err))
		}
	})
}

// NotifyRefusalDemoteTriggered 异步发送「拒答降级」告警。
// 由 Distribute 中间件在确定「重导/拒绝」结果后调用。SETNX 节流窗口与 DemoteSeconds
// 对齐，同一个用户的一次降级生命周期内只会生成一条，TTL 过期后再次降级才会再发。
func NotifyRefusalDemoteTriggered(userID, refusedCount, totalWindow, thresholdPct, demoteSeconds, originalChannelID, fallbackChannelID int, outcome OverloadFallbackOutcome) {
	if userID <= 0 {
		return
	}
	gopool.Go(func() {
		if !isTgOverloadEnabled(OptionKeyTgNotifyRefusalDemoteEnabled) {
			return
		}
		if !tryAcquireRefusalThrottle(userID, demoteSeconds) {
			return
		}
		token, chatId := resolveOverloadTgTarget()
		if token == "" || chatId == "" {
			return
		}
		user := resolveOverloadUser(userID)
		text := buildRefusalDemoteText(user, refusedCount, totalWindow, thresholdPct, demoteSeconds, originalChannelID, fallbackChannelID, outcome)
		if err := SendTelegramMessageNoPreview(token, chatId, text); err != nil {
			common.SysError(fmt.Sprintf("overload tg notify (refusal_demote) failed user=%d: %v", userID, err))
		}
	})
}

// isTgOverloadEnabled 读独立开关 option。两个 key 都没显式写入 "true" 时视为关闭。
func isTgOverloadEnabled(key string) bool {
	switch strings.ToLower(strings.TrimSpace(model.GetOptionString(key))) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// resolveOverloadTgTarget 决定发到哪个群：
// 必须显式配置 OptionKeyTgOverloadChatId；为空表示"管理员未开启过载告警"，
// 直接返回空串让调用方跳过发送（不会回落到 VIP 日报群，避免噪声串群）。
// bot token 永远从 OptionKeyTgBotToken 读，与 VIP 日报共用。
func resolveOverloadTgTarget() (token, chatId string) {
	token = model.GetOptionString(OptionKeyTgBotToken)
	if token == "" {
		return "", ""
	}
	cid := strings.TrimSpace(model.GetOptionString(OptionKeyTgOverloadChatId))
	if cid == "" {
		return "", ""
	}
	return token, cid
}

func resolveOverloadUser(userID int) overloadNotifyUser {
	u := overloadNotifyUser{ID: userID}
	if cache, err := model.GetUserCache(userID); err == nil && cache != nil {
		u.Username = cache.Username
	}
	return u
}

func buildConcurrencyOverloadText(u overloadNotifyUser, currentCount, limit, originalChannelID, fallbackChannelID int, outcome OverloadFallbackOutcome) string {
	var b strings.Builder
	b.WriteString("【并发超限告警】\n")
	fmt.Fprintf(&b, "用户：%s\n", formatUserDisplay(u))
	fmt.Fprintf(&b, "当前 in-flight：%d / 上限 %d\n", currentCount, limit)
	if originalChannelID > 0 {
		fmt.Fprintf(&b, "原渠道：%d\n", originalChannelID)
	}
	writeFallbackOutcome(&b, fallbackChannelID, outcome)
	if link := buildOverloadLogsLink(u.ID); link != "" {
		b.WriteString("\n查看请求日志：")
		b.WriteString(link)
	}
	return b.String()
}

func buildRefusalDemoteText(u overloadNotifyUser, refusedCount, totalWindow, thresholdPct, demoteSeconds, originalChannelID, fallbackChannelID int, outcome OverloadFallbackOutcome) string {
	ratio := 0.0
	if totalWindow > 0 {
		ratio = float64(refusedCount) / float64(totalWindow) * 100
	}
	var b strings.Builder
	b.WriteString("【拒答比超限告警】\n")
	fmt.Fprintf(&b, "用户：%s\n", formatUserDisplay(u))
	fmt.Fprintf(&b, "滑窗拒答比：%.1f%% (%d/%d) 阈值 %d%%\n", ratio, refusedCount, totalWindow, thresholdPct)
	fmt.Fprintf(&b, "降级持续：%d 秒\n", demoteSeconds)
	if originalChannelID > 0 {
		fmt.Fprintf(&b, "原渠道：%d\n", originalChannelID)
	}
	writeFallbackOutcome(&b, fallbackChannelID, outcome)
	if link := buildOverloadLogsLink(u.ID); link != "" {
		b.WriteString("\n查看请求日志：")
		b.WriteString(link)
	}
	return b.String()
}

// writeFallbackOutcome 把 Distribute 中间件的路由决策渲染成一行文案。
// 已重导 → 明确给出兜底渠道 ID；兜底不可用 → 提示 429 并说明原因口径。
func writeFallbackOutcome(b *strings.Builder, fallbackChannelID int, outcome OverloadFallbackOutcome) {
	switch outcome {
	case OverloadFallbackRedirected:
		if fallbackChannelID > 0 {
			fmt.Fprintf(b, "已重导到兜底渠道：%d", fallbackChannelID)
		} else {
			b.WriteString("已重导到兜底渠道")
		}
	case OverloadFallbackUnavailable:
		b.WriteString("无可用兜底渠道（未配置 / 自环 / 已禁用 / 不支持当前模型），已返回 429")
	}
}

func formatUserDisplay(u overloadNotifyUser) string {
	if u.Username == "" {
		return fmt.Sprintf("id=%d", u.ID)
	}
	return fmt.Sprintf("%s (id=%d)", u.Username, u.ID)
}

// buildOverloadLogsLink 复用 VIP 日报的 base url 配置，拼管理员日志页。
// 没配 base url 就返回空串，不写进消息。
func buildOverloadLogsLink(userID int) string {
	base := strings.TrimSpace(model.GetOptionString(OptionKeyTgFrontendBaseUrl))
	if base == "" {
		return ""
	}
	if base[len(base)-1] != '/' {
		base += "/"
	}
	return fmt.Sprintf("%sconsole/log?user_id=%d", base, userID)
}

// 并发超限告警的节流：优先走 Redis，失败回落到进程内 map。
// 返回 true 表示本次抢到了「发送权」，调用方可以发；false 表示在节流窗口内被压制。
func tryAcquireConcurrencyThrottle(userID int) bool {
	return tryAcquireOverloadThrottle(
		fmt.Sprintf("%s%d", overloadConcurrencyThrottleKeyPrefix, userID),
		userID,
		"concurrency",
		overloadConcurrencyThrottleWindow,
	)
}

// 拒答降级告警的节流：窗口与 DemoteSeconds TTL 对齐，一次降级生命周期只报一次。
// 兜底窗口 60s，避免 demoteSeconds 异常为 0 时节流失效导致刷屏。
func tryAcquireRefusalThrottle(userID, demoteSeconds int) bool {
	window := time.Duration(demoteSeconds) * time.Second
	if window < 60*time.Second {
		window = 60 * time.Second
	}
	return tryAcquireOverloadThrottle(
		fmt.Sprintf("%s%d", overloadRefusalThrottleKeyPrefix, userID),
		userID,
		"refusal_demote",
		window,
	)
}

func tryAcquireOverloadThrottle(key string, userID int, kind string, window time.Duration) bool {
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(overloadTgRedisOpTimeoutMs)*time.Millisecond)
		defer cancel()
		ok, err := common.RDB.SetNX(ctx, key, "1", window).Result()
		if err == nil {
			return ok
		}
		logger.LogWarn(ctx, fmt.Sprintf("overload tg throttle redis setnx failed kind=%s user=%d: %v, falling back", kind, userID, err))
	}
	return overloadThrottleMem().tryAcquire(key, window)
}

// --- 进程内 fallback 节流 -------------------------------------------------

type overloadThrottleMemStore struct {
	mu   sync.Mutex
	last map[string]time.Time
}

var (
	overloadThrottleOnce sync.Once
	overloadThrottleInst *overloadThrottleMemStore
)

func overloadThrottleMem() *overloadThrottleMemStore {
	overloadThrottleOnce.Do(func() {
		overloadThrottleInst = &overloadThrottleMemStore{last: make(map[string]time.Time)}
	})
	return overloadThrottleInst
}

func (s *overloadThrottleMemStore) tryAcquire(key string, window time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if last, ok := s.last[key]; ok && now.Sub(last) < window {
		return false
	}
	s.last[key] = now
	// 简单的被动清理：顺手清掉已过窗口的条目，避免长期活跃进程堆积。
	// 严格限制一次扫描量，防止 O(N)。
	cleaned := 0
	for k, t := range s.last {
		if now.Sub(t) >= window {
			delete(s.last, k)
			cleaned++
			if cleaned >= 32 {
				break
			}
		}
	}
	return true
}
