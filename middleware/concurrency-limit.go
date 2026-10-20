package middleware

import (
	"context"
	"fmt"
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

// UserConcurrencyLimit 实现每用户 in-flight 并发请求数限制 + 拒答比超限降级。
//
// 两个独立开关、同一出口：
//   - setting.MaxConcurrentRequestsPerUserEnabled 控制「并发超限 → 兜底」
//   - operation_setting.RefusalDemoteSetting.Enabled（+ User.RefusalDemoteOverride）
//     控制「拒答比超限 → 兜底」
//
// 两者都通过 ContextKeyUserOverloaded = true 让 Distribute 中间件把请求重导
// 到渠道的 FallbackChannelId。只要其中一个开关开启，中间件就必须跑。
//
// 跳过场景：
//   - 两个开关都关闭
//   - 管理员通过 Token 指定了具体渠道（ContextKeyTokenSpecificChannelId）
//   - 没有有效用户 ID
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
		if demoted, ratio, windowCount := service.IsUserDemoted(c, userID); demoted {
			common.SetContextKey(c, constant.ContextKeyUserOverloaded, true)
			common.SetContextKey(c, constant.ContextKeyRefusalDemoted, true)
			common.SetContextKey(c, constant.ContextKeyRefusalDemoteRatio, ratio)
			common.SetContextKey(c, constant.ContextKeyRefusalDemoteWindow, windowCount)
			common.SysLog(fmt.Sprintf("[concurrency] user=%d refusal demoted ratio=%.2f window=%d, routing to fallback",
				userID, ratio, windowCount))
			// 不 return：继续走并发计数，若并发开关也开启就正常累加槽位
		}

		if !setting.MaxConcurrentRequestsPerUserEnabled {
			common.SysLog("[concurrency] concurrency disabled globally, skip counting (refusal-demote already evaluated)")
			c.Next()
			return
		}

		// 并发上限三态语义（user 和 global 同套）：
		//   -1  = 不限（跳过计数器与兜底重导）
		//    0  = 走回落：user=0 → global；global=0 → 关闭
		//   >0  = 自定义上限
		cache, cacheErr := model.GetUserCache(userID)
		if cacheErr != nil {
			common.SysLog(fmt.Sprintf("[concurrency] user=%d GetUserCache err=%v", userID, cacheErr))
		} else if cache == nil {
			common.SysLog(fmt.Sprintf("[concurrency] user=%d GetUserCache returned nil cache", userID))
		} else {
			common.SysLog(fmt.Sprintf("[concurrency] user=%d cache: id=%d group=%s maxConcurrency=%d cacheSchema=%d",
				userID, cache.Id, cache.Group, cache.MaxConcurrency, cache.CacheSchema))
		}

		userMax := 0
		if cache != nil {
			userMax = cache.MaxConcurrency
		}
		globalMax := setting.MaxConcurrentRequestsPerUser

		// 用户显式不限 → 直接放行
		if userMax < 0 {
			common.SysLog(fmt.Sprintf("[concurrency] user=%d unlimited (user override), skip", userID))
			c.Next()
			return
		}
		// 用户走回落 (0) 且全局不限 → 放行
		if userMax == 0 && globalMax < 0 {
			common.SysLog(fmt.Sprintf("[concurrency] user=%d unlimited (global), skip", userID))
			c.Next()
			return
		}

		limit := globalMax
		userCustom := 0
		if userMax > 0 {
			userCustom = userMax
			limit = userMax
		}
		// 到这里 limit 应 >0；0 表示 global=0 的「关闭」语义，与开关关闭等价
		if limit <= 0 {
			common.SysLog(fmt.Sprintf("[concurrency] user=%d limit=0 (disabled), skip", userID))
			c.Next()
			return
		}

		count, release := acquireConcurrencySlot(c, userID)
		defer release()

		over := count > limit
		common.SysLog(fmt.Sprintf("[concurrency] user=%d path=%s count=%d limit=%d (global=%d userCustom=%d) overloaded=%v",
			userID, c.Request.URL.Path, count, limit,
			globalMax, userCustom, over))
		if over {
			common.SetContextKey(c, constant.ContextKeyUserOverloaded, true)
		}
		c.Next()
	}
}

// acquireConcurrencySlot 返回自增后的当前计数以及用于释放槽位的闭包。
// 当 Redis 不可用或自增失败时，自动退回进程内计数器，并保证总是返回可调用
// 的 release，上层无需额外判空。
func acquireConcurrencySlot(c *gin.Context, userID int) (int, func()) {
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		key := userConcurrencyKey(userID)
		pipe := common.RDB.TxPipeline()
		incr := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, userConcurrencyTTL)
		if _, err := pipe.Exec(ctx); err == nil {
			current := int(incr.Val())
			return current, func() {
				// 释放时直接 DECR；若因 TTL 自愈先于释放把 key 清掉，
				// DECR 会把它变成 -1，这里顺手归零避免下次 INCR 从负数起算。
				dctx := context.Background()
				if v, dErr := common.RDB.Decr(dctx, key).Result(); dErr == nil && v < 0 {
					common.RDB.Set(dctx, key, 0, userConcurrencyTTL)
				}
			}
		}
		// Redis 失败：fallthrough 到内存计数器，保证服务可用
		common.SysLog(fmt.Sprintf("UserConcurrencyLimit: Redis incr failed, falling back to in-memory counter for user %d", userID))
	}

	counter := common.UserConcurrencyCounter()
	current := counter.Incr(userID)
	return current, func() {
		counter.Decr(userID)
	}
}
