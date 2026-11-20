package middleware

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

const (
	userConcurrencyKeyPrefix = "concurrency:user:"
	// userConcurrencyTTL 对 Redis 计数器加一个保护 TTL，防止进程崩溃后计数永远不归零。
	// 该值应覆盖绝大多数请求的实际处理时长；对极少数超长流式请求，提前过期后下一次
	// INCR 会在空键上重新计数，可能出现瞬间"超发"，属于可接受的权衡。
	userConcurrencyTTL = 60 * time.Second
)

func userConcurrencyKey(userID int) string {
	return fmt.Sprintf("%s%d", userConcurrencyKeyPrefix, userID)
}

// userGroupConcurrencyKey 分组维度的 in-flight 计数键（规格 §21.4）。
func userGroupConcurrencyKey(userID int, group string) string {
	return fmt.Sprintf("%s%d:group:%s", userConcurrencyKeyPrefix, userID, group)
}

// groupRateLimitConfig 是用户某个分组的分组速率配置（规格 §21.3）。
//
//	Limit    -1 不限 / 0（或缺省）该分组不单独限制 / >0 自定义并发上限
//	Fallback 到顶后是否走兜底渠道；默认 true
type groupRateLimitConfig struct {
	Limit    int  `json:"limit"`
	Fallback bool `json:"fallback"`
}

// parseGroupRateLimits 解析 user.group_rate_limits；坏数据按「没配」处理，
// 绝不能因为一段坏 JSON 把整个用户的流量拒掉。
func parseGroupRateLimits(raw string) map[string]groupRateLimitConfig {
	out := make(map[string]groupRateLimitConfig)
	if strings.TrimSpace(raw) == "" {
		return out
	}
	// ⛔ fallback 用 *bool 解析：缺省必须是 true（规格 §21.3「默认走兜底」）。
	// 用普通 bool 的话，库里手写 `{"vip":{"limit":5}}` 会被读成 false，
	// 到顶直接 429 —— 明明是没写，却被当成「关掉了兜底」。
	var parsed map[string]struct {
		Limit    int   `json:"limit"`
		Fallback *bool `json:"fallback"`
	}
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		common.SysLog("UserConcurrencyLimit: 解析 group_rate_limits 失败，按未配置处理: " + err.Error())
		return out
	}
	for group, cfg := range parsed {
		if strings.TrimSpace(group) == "" {
			continue
		}
		normalized := groupRateLimitConfig{Limit: cfg.Limit, Fallback: true}
		if cfg.Fallback != nil {
			normalized.Fallback = *cfg.Fallback
		}
		out[group] = normalized
	}
	return out
}

// UserConcurrencyLimit 实现每用户 in-flight 并发限制 + 拒答比超限降级 + 分组速率（§21）。
//
// 三条独立的信号，出口各不相同（⛔ 不许合并成一段通用逻辑）：
//   - 拒答降级：置 UserOverloaded + RefusalDemoted → Distribute 走**渠道自配**的
//     FallbackChannelId（行为与 §21 之前逐字节一致）。
//   - 分组速率超限且该分组 fallback=true：置 RateLimitExceeded → Distribute 走
//     **全局** RateLimitFallbackChannelId。
//   - 分组速率超限且该分组 fallback=false：置 RateLimitRejected → 直接 429，
//     连跳转都不做。
//   - 没配分组速率、只超总并发：旧行为（UserOverloaded → 渠道自配兜底）。
func UserConcurrencyLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		if _, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
			// 管理员指定渠道，绕过并发计数和降级重导
			common.SysLog("[concurrency] specific channel pinned, skip")
			c.Next()
			return
		}
		userID := c.GetInt("id")
		if userID <= 0 {
			common.SysLog("[concurrency] no user id in context, skip")
			c.Next()
			return
		}

		// 拒答降级判定：独立于并发开关。只要用户当前处于降级态，就置 Overloaded
		// 标记，让 Distribute 中间件把请求重导到渠道的 FallbackChannelId。
		if demoted, ratio, windowCount, refusedCount := service.IsUserDemoted(c, userID); demoted {
			common.SetContextKey(c, constant.ContextKeyUserOverloaded, true)
			common.SetContextKey(c, constant.ContextKeyRefusalDemoted, true)
			common.SetContextKey(c, constant.ContextKeyRefusalDemoteRatio, ratio)
			common.SetContextKey(c, constant.ContextKeyRefusalDemoteWindow, windowCount)
			common.SetContextKey(c, constant.ContextKeyRefusalRefusedCount, refusedCount)
			common.SysLog(fmt.Sprintf("[concurrency] user=%d refusal demoted ratio=%.2f window=%d, routing to fallback",
				userID, ratio, windowCount))
			// 不 return：继续走并发计数，若并发开关也开启就正常累加槽位
		}

		cache, cacheErr := model.GetUserCache(userID)
		if cacheErr != nil {
			common.SysLog(fmt.Sprintf("[concurrency] user=%d GetUserCache err=%v", userID, cacheErr))
		}

		// ── 总并发上限（沿用原三态语义，行为不变）──────────────────────
		userMax := 0
		groupRateLimits := map[string]groupRateLimitConfig{}
		if cache != nil {
			userMax = cache.MaxConcurrency
			groupRateLimits = parseGroupRateLimits(cache.GroupRateLimits)
		}
		globalMax := setting.MaxConcurrentRequestsPerUser

		totalLimit := 0
		if setting.MaxConcurrentRequestsPerUserEnabled {
			switch {
			case userMax < 0: // 用户显式不限
			case userMax == 0 && globalMax < 0: // 用户走回落且全局不限
			default:
				limit := globalMax
				if userMax > 0 {
					limit = userMax
				}
				if limit > 0 {
					totalLimit = limit
				}
			}
		}

		// ── 分组速率（规格 §21.3）────────────────────────────────────
		// 分组取「本次请求所在分组」：TokenAuth 已经写入 UsingGroup；auto 分组的
		// 真实分组要到 Distribute 才定，这里按 auto 字面值处理（不单独限）。
		group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		groupCfg, hasGroupCfg := groupRateLimits[group]
		groupLimited := hasGroupCfg && groupCfg.Limit > 0

		if totalLimit <= 0 && !groupLimited {
			c.Next()
			return
		}

		count := 0
		if totalLimit > 0 {
			var release func()
			count, release = acquireConcurrencySlot(c, userID)
			defer release()
		}
		groupCount := 0
		if groupLimited {
			var release func()
			groupCount, release = acquireGroupConcurrencySlot(c, userID, group)
			defer release()
		}

		decision := decideConcurrencyAction(totalLimit, count, hasGroupCfg, groupCfg, groupLimited, groupCount)
		overTotal, overGroup := decision.OverTotal, decision.OverGroup
		common.SysLog(fmt.Sprintf("[concurrency] user=%d path=%s group=%q count=%d limit=%d groupCount=%d groupLimit=%d overTotal=%v overGroup=%v",
			userID, c.Request.URL.Path, group, count, totalLimit, groupCount, groupCfg.Limit, overTotal, overGroup))
		if !decision.Over {
			c.Next()
			return
		}

		// 超限：先把现场落到 context，供 Distribute 与告警拼消息。
		common.SetContextKey(c, constant.ContextKeyConcurrencyOverloaded, true)
		shownCount, shownLimit := count, totalLimit
		if overGroup && !overTotal {
			shownCount, shownLimit = groupCount, groupCfg.Limit
		}
		common.SetContextKey(c, constant.ContextKeyConcurrencyCount, shownCount)
		common.SetContextKey(c, constant.ContextKeyConcurrencyLimit, shownLimit)

		switch {
		case decision.Reject:
			// 分组策略是不走兜底：直接 429，连跳转都不做。
			common.SetContextKey(c, constant.ContextKeyRateLimitRejected, true)
			common.SetContextKey(c, constant.ContextKeyRateLimitGroup, group)
		case decision.RateLimitFallback:
			// 走全局速率兜底渠道（与渠道自配的兜底不是一条链路）。
			common.SetContextKey(c, constant.ContextKeyRateLimitExceeded, true)
			common.SetContextKey(c, constant.ContextKeyRateLimitGroup, group)
		default:
			// 没配分组速率、只超总并发：旧行为，走渠道自配的 FallbackChannelId。
			common.SetContextKey(c, constant.ContextKeyUserOverloaded, true)
		}
		c.Next()
	}
}

// acquireConcurrencySlot 返回自增后的当前计数以及用于释放槽位的闭包。
// 当 Redis 不可用或自增失败时，自动退回进程内计数器，并保证总是返回可调用
// 的 release，上层无需额外判空。
func acquireConcurrencySlot(c *gin.Context, userID int) (int, func()) {
	if current, release, ok := acquireRedisConcurrencySlot(c, userConcurrencyKey(userID)); ok {
		return current, release
	}
	counter := common.UserConcurrencyCounter()
	current := counter.Incr(userID)
	return current, func() {
		counter.Decr(userID)
	}
}

// 进程内分组计数器：Redis 不可用时兜底。按 (userID, group) 组合键计数，
// 与总并发用的 common.UserConcurrencyCounter 分开，避免两套口径互相污染。
var (
	groupConcurrencyMu    sync.Mutex
	groupConcurrencyCount = map[string]int{}
)

func acquireGroupConcurrencySlot(c *gin.Context, userID int, group string) (int, func()) {
	if current, release, ok := acquireRedisConcurrencySlot(c, userGroupConcurrencyKey(userID, group)); ok {
		return current, release
	}
	key := fmt.Sprintf("%d|%s", userID, group)
	groupConcurrencyMu.Lock()
	groupConcurrencyCount[key]++
	current := groupConcurrencyCount[key]
	groupConcurrencyMu.Unlock()
	return current, func() {
		groupConcurrencyMu.Lock()
		defer groupConcurrencyMu.Unlock()
		if v := groupConcurrencyCount[key]; v <= 1 {
			delete(groupConcurrencyCount, key)
		} else {
			groupConcurrencyCount[key] = v - 1
		}
	}
}

// acquireRedisConcurrencySlot 是两种计数器共用的 Redis 实现。
// 返回 ok=false 表示 Redis 不可用，调用方退回进程内计数。
func acquireRedisConcurrencySlot(c *gin.Context, key string) (int, func(), bool) {
	if !common.RedisEnabled || common.RDB == nil {
		return 0, nil, false
	}
	ctx := context.Background()
	pipe := common.RDB.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, userConcurrencyTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		common.SysLog(fmt.Sprintf("UserConcurrencyLimit: Redis incr failed key=%s err=%v, falling back to in-memory counter", key, err))
		return 0, nil, false
	}
	current := int(incr.Val())
	return current, func() {
		// 释放时直接 DECR；若因 TTL 自愈先于释放把 key 清掉，
		// DECR 会把它变成 -1，这里顺手归零避免下次 INCR 从负数起算。
		dctx := context.Background()
		if v, dErr := common.RDB.Decr(dctx, key).Result(); dErr == nil && v < 0 {
			common.RDB.Set(dctx, key, 0, userConcurrencyTTL)
		}
	}, true
}

// concurrencyDecision 是一次超限判定的结论（规格 §21.4）。
//
// 抽成纯函数是为了能直接断言三条出口互不串味：
// 分组策略拒、走全局速率兜底、旧的总并发兜底。middleware 里只负责把结论
// 翻成 context 标记。
type concurrencyDecision struct {
	Over      bool
	OverTotal bool
	OverGroup bool
	// Reject 分组策略 fallback=false：直接 429。
	Reject bool
	// RateLimitFallback 分组策略 fallback=true：走全局速率兜底渠道。
	RateLimitFallback bool
	// LegacyOverload 没配分组速率、只超总并发：旧行为，走渠道自配兜底。
	LegacyOverload bool
}

func decideConcurrencyAction(totalLimit, count int, hasGroupCfg bool, cfg groupRateLimitConfig, groupLimited bool, groupCount int) concurrencyDecision {
	d := concurrencyDecision{
		OverTotal: totalLimit > 0 && count > totalLimit,
		OverGroup: groupLimited && groupCount > cfg.Limit,
	}
	d.Over = d.OverTotal || d.OverGroup
	if !d.Over {
		return d
	}
	switch {
	case hasGroupCfg && !cfg.Fallback:
		d.Reject = true
	case hasGroupCfg:
		d.RateLimitFallback = true
	default:
		d.LegacyOverload = true
	}
	return d
}
