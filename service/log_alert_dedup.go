package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

// 错误日志告警的冷却/去重存储。
// 语义：以 (rule_id, scope_type, scope_value) 为 key，未过期即视为"已告警过，跳过"。
//
// 双路径实现：
//   - Redis 启用时走 Redis（多节点共享冷却窗口）。
//   - Redis 未启用时用进程内 sync.Map + 后台 goroutine 定期清理。
//
// Mark 用于 首次触发，TTL = rule.IntervalMinutes 分钟。
// Extend 用于 用户点了 ack 链接，覆盖为 15 分钟。

const logAlertDedupPrefix = "log_alert_cd:"

// 内存路径存储
type logAlertMemEntry struct {
	ExpireAt time.Time
}

var (
	logAlertMemStore   sync.Map
	logAlertMemCleanup sync.Once
)

func logAlertRedisKey(rawKey string) string {
	return logAlertDedupPrefix + rawKey
}

// LogAlertCooldownExists rawKey 形如 "{rule_id}:{scope_type}:{scope_value}"。
func LogAlertCooldownExists(rawKey string) bool {
	if common.RedisEnabled {
		v, err := common.RedisGet(logAlertRedisKey(rawKey))
		if err != nil {
			// redis: nil 表示不存在；其他错误保守放行（宁可多发一次也不能吞掉）
			return false
		}
		return v != ""
	}
	logAlertMemCleanup.Do(startLogAlertMemCleanup)
	v, ok := logAlertMemStore.Load(rawKey)
	if !ok {
		return false
	}
	e, ok := v.(logAlertMemEntry)
	if !ok {
		logAlertMemStore.Delete(rawKey)
		return false
	}
	if time.Now().After(e.ExpireAt) {
		logAlertMemStore.Delete(rawKey)
		return false
	}
	return true
}

// LogAlertCooldownMark 写入冷却 key（首次触发）。
func LogAlertCooldownMark(rawKey string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Minute
	}
	if common.RedisEnabled {
		return common.RedisSet(logAlertRedisKey(rawKey), "1", ttl)
	}
	logAlertMemCleanup.Do(startLogAlertMemCleanup)
	logAlertMemStore.Store(rawKey, logAlertMemEntry{ExpireAt: time.Now().Add(ttl)})
	return nil
}

// LogAlertCooldownExtend 用户点了 ack 后延长冷却（覆盖为新 TTL）。
func LogAlertCooldownExtend(rawKey string, ttl time.Duration) error {
	return LogAlertCooldownMark(rawKey, ttl)
}

// BuildLogAlertCooldownKey 生成规范化 rawKey：
//
//	{rule_id}:{scope_type}:{scope_value}
//
// scope_value 建议使用具体 id（user_id / token_id / channel_id）或 "all"。
func BuildLogAlertCooldownKey(ruleId int, scopeType, scopeValue string) string {
	if scopeValue == "" {
		scopeValue = "all"
	}
	return fmt.Sprintf("%d:%s:%s", ruleId, scopeType, scopeValue)
}

// 内存清理：每小时扫一次，删除已过期条目
func startLogAlertMemCleanup() {
	gopool.Go(func() {
		for {
			time.Sleep(time.Hour)
			now := time.Now()
			logAlertMemStore.Range(func(k, v any) bool {
				if e, ok := v.(logAlertMemEntry); ok {
					if now.After(e.ExpireAt) {
						logAlertMemStore.Delete(k)
					}
				}
				return true
			})
		}
	})
}
