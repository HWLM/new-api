package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 一键清空蒸馏检测的**运行态**（规格 §20）。
//
// 误判之后运营要能立刻解除，不用等 TTL。清的是：
//   - 降级标记（distill:demote:*）
//   - 确认计数（distill:confirm:*，滑动窗口 ZSET）
//   - 时间桶（distill:bucket:*）
//   - 去重窗口（distill:dedup:*）
//   - 告警节流（distill:alert:*）
//   - 每分钟请求量计数（distill:rpm:*，不清的话刷新那一分钟还会顶着阈值）
//
// ⛔ distill_detect_hits 历史记录**不删**：那是判定证据，误判之后要能复盘
// 「到底判了他什么」。
//
// ⛔ 两套后端都要清：线上是 Redis，但 Redis 没配或连不上时整条链路跑在进程内
// 兜底上，只清 Redis 等于点了没点。

// distillCacheScanBatch 每轮 SCAN 取的键数。
//
// ⛔ 用 SCAN 分批而不是 KEYS：KEYS 会扫全库并阻塞事件循环，线上有几十万键时
// 能把整个实例卡住。
const distillCacheScanBatch = 500

// distillCacheDeleteBatch 每次 DEL 的批量。太小网络往返多，太大阻塞主线程。
const distillCacheDeleteBatch = 200

// DistillCacheClearResult 是一次清理的结果。
type DistillCacheClearResult struct {
	// Cleared 实际清理的运行态条目数（Redis 键数 + 内存兜底条目数）。
	Cleared int `json:"cleared"`
	// Backend 本次用到的后端：redis / memory。
	Backend string `json:"backend"`
	// UpgradedUsers 被解除降级的用户数。
	UpgradedUsers int `json:"upgraded_users"`
}

// ClearDistillRuntimeState 清理运行态。userID <= 0 表示清全部用户。
func ClearDistillRuntimeState(ctx context.Context, userID int) DistillCacheClearResult {
	result := DistillCacheClearResult{Backend: "memory"}

	if common.RedisEnabled && common.RDB != nil {
		keys, demoted, err := clearDistillRedisState(ctx, userID)
		if err != nil {
			// Redis 出错也要把内存那份清掉，并且让调用方知道这次没走到 Redis。
			common.SysError(fmt.Sprintf("distill_detect: 清理 Redis 运行态失败 user=%d err=%v", userID, err))
		} else {
			result.Backend = "redis"
			result.Cleared += keys
			result.UpgradedUsers += demoted
		}
	}

	keys, demoted := clearDistillMemoryState(userID)
	result.Cleared += keys
	result.UpgradedUsers += demoted
	return result
}

// clearDistillRedisState 用 SCAN 找出本次范围内的键并分批删除。
func clearDistillRedisState(ctx context.Context, userID int) (cleared int, demoted int, err error) {
	cursor := uint64(0)
	for {
		scanCtx, cancel := context.WithTimeout(ctx, time.Second)
		keys, next, scanErr := common.RDB.Scan(scanCtx, cursor, "distill:*", distillCacheScanBatch).Result()
		cancel()
		if scanErr != nil {
			return cleared, demoted, scanErr
		}
		targets := make([]string, 0, len(keys))
		for _, key := range keys {
			if userID > 0 && !distillKeyBelongsToUser(key, userID) {
				continue
			}
			targets = append(targets, key)
			if strings.HasPrefix(key, distillDemoteKeyPre) {
				demoted++
			}
		}
		for start := 0; start < len(targets); start += distillCacheDeleteBatch {
			end := start + distillCacheDeleteBatch
			if end > len(targets) {
				end = len(targets)
			}
			delCtx, cancel := context.WithTimeout(ctx, time.Second)
			n, delErr := common.RDB.Del(delCtx, targets[start:end]...).Result()
			cancel()
			if delErr != nil {
				return cleared, demoted, delErr
			}
			cleared += int(n)
		}
		if next == 0 {
			return cleared, demoted, nil
		}
		cursor = next
	}
}

// distillKeyBelongsToUser 判断一个 distill:* 键是否属于该用户。
//
// ⛔ 不能一律用前缀匹配：`distill:alert:{user:12}` 是 `distill:alert:{user:123}`
// 的前缀，前缀匹配会把别人的键一起删掉。带尾部 `:` 的（rpm/bucket/dedup）
// 才用前缀，其余必须精确相等。
func distillKeyBelongsToUser(key string, userID int) bool {
	prefixes := []string{
		fmt.Sprintf("%s%d:", distillRPMKeyPrefix, userID),
		fmt.Sprintf("%s%d:", distillBucketKeyPrefix, userID),
		fmt.Sprintf("%s{user:%d}:", distillDedupKeyPrefix, userID),
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	exact := []string{
		fmt.Sprintf("%s{user:%d}", distillConfirmKeyPrefix, userID),
		fmt.Sprintf("%s{user:%d}", distillDemoteKeyPre, userID),
		fmt.Sprintf("%s{user:%d}", distillAlertThrottleKeyPrefix, userID),
	}
	for _, want := range exact {
		if key == want {
			return true
		}
	}
	return false
}

// clearDistillMemoryState 清进程内兜底那份。
func clearDistillMemoryState(userID int) (cleared int, demoted int) {
	keys, demotedUsers := distillMemoryRPM.clearState(userID)
	cleared += keys
	demoted += demotedUsers
	cleared += distillAlertClearThrottle(userID)
	return cleared, demoted
}
