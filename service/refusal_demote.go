package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// Refusal 判定口径：仅 Claude stop_reason=refusal 会把
// ContextKeyBillingExemptReason 置为该字面量，参见
// relay/channel/claude/relay-claude.go:claudeRefusalNoOutputExemptReason。
// 跨包契约常量；双向改动需同步两处。
const refusalExemptReasonClaudeNoOutput = "claude_refusal_no_output"

const (
	refusalWindowKeyPrefix  = "refusal:win:"
	refusalDemoteKeyPrefix  = "refusal:demote:"
	refusalRedisOpTimeoutMs = 100
)

func refusalWindowKey(userID int) string {
	return fmt.Sprintf("%s{user:%d}", refusalWindowKeyPrefix, userID)
}

func refusalDemoteKey(userID int) string {
	return fmt.Sprintf("%s{user:%d}", refusalDemoteKeyPrefix, userID)
}

// IsTextClassRelayFormat 判定是否属于「文本类」relay，这是降级功能的作用域：
// 只有这几类请求会被计入滑窗，也只有它们受降级影响。
// 其它 relay（image/audio/task/mj/rerank/embedding）不参与。
func IsTextClassRelayFormat(f types.RelayFormat) bool {
	switch f {
	case types.RelayFormatOpenAI,
		types.RelayFormatClaude,
		types.RelayFormatGemini,
		types.RelayFormatOpenAIResponses,
		types.RelayFormatOpenAIResponsesCompaction:
		return true
	}
	return false
}

// IsRefusalBillingExemptReason 判定 BillingExemptReason 是否属于「拒答」口径。
// 当前仅 Claude 的 stop_reason=refusal 命中。
func IsRefusalBillingExemptReason(reason string) bool {
	return reason == refusalExemptReasonClaudeNoOutput
}

// isRefusalDemoteEffectiveForUser 根据全局开关和 User.RefusalDemoteOverride
// 决定对该用户是否启用降级功能。三态语义：
//
//	-1  用户显式禁用（永不降级）
//	 0  跟随全局 Enabled
//	 1  用户显式启用（即便全局关闭）
func isRefusalDemoteEffectiveForUser(userID int) bool {
	s := operation_setting.GetRefusalDemoteSetting()
	override := 0
	if cache, err := model.GetUserCache(userID); err == nil && cache != nil {
		override = cache.RefusalDemoteOverride
	}
	if override < 0 {
		return false
	}
	if override > 0 {
		return true
	}
	return s.Enabled
}

// RecordRefusalOutcome 在每次「文本类」relay 请求结束后调用，向用户滑窗
// 推入一条样本，并在越过阈值时打上降级标记。
// 调用方必须自行判定本次请求是否属于文本类（IsTextClassRelayFormat）。
// 该函数无返回值，所有错误本地记日志不向上冒泡，调用方通常在 gopool.Go
// 中后台执行。
func RecordRefusalOutcome(ctx context.Context, userID int, refused bool) {
	if userID <= 0 {
		return
	}
	if !isRefusalDemoteEffectiveForUser(userID) {
		return
	}
	s := operation_setting.GetRefusalDemoteSetting()
	windowSize := s.WindowSize
	if windowSize <= 0 {
		windowSize = 100
	}
	minSamples := s.MinSamples
	if minSamples <= 0 {
		minSamples = 1
	}
	thresholdPct := s.ThresholdPct
	if thresholdPct <= 0 || thresholdPct > 100 {
		return
	}
	demoteSeconds := s.DemoteSeconds
	if demoteSeconds <= 0 {
		demoteSeconds = 3600
	}
	windowTTL := s.WindowTTLSeconds
	if windowTTL <= 0 {
		windowTTL = 86400
	}

	total, refusedCount := recordAndSnapshot(ctx, userID, refused, windowSize, time.Duration(windowTTL)*time.Second)
	if total < minSamples {
		return
	}
	// 阈值用整数百分比：refusedCount * 100 >= thresholdPct * total
	if refusedCount*100 < thresholdPct*total {
		return
	}
	// 命中：写降级标记。重复命中会刷新 TTL（LPUSH 继续累积，demote key 覆盖）。
	triggerDemotion(ctx, userID, total, refusedCount, thresholdPct, demoteSeconds)
}

func recordAndSnapshot(ctx context.Context, userID int, refused bool, windowSize int, windowTTL time.Duration) (total, refusedCount int) {
	if common.RedisEnabled && common.RDB != nil {
		if ok, t, r := recordAndSnapshotRedis(ctx, userID, refused, windowSize, windowTTL); ok {
			return t, r
		}
		// Redis 调用失败：退回到内存计数器，保证服务可用。
	}
	return common.UserRefusalTrackerInstance().Record(userID, refused, windowSize)
}

func recordAndSnapshotRedis(ctx context.Context, userID int, refused bool, windowSize int, windowTTL time.Duration) (ok bool, total, refusedCount int) {
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(refusalRedisOpTimeoutMs)*time.Millisecond)
	defer cancel()
	key := refusalWindowKey(userID)
	bit := "0"
	if refused {
		bit = "1"
	}
	pipe := common.RDB.TxPipeline()
	pipe.LPush(callCtx, key, bit)
	pipe.LTrim(callCtx, key, 0, int64(windowSize-1))
	pipe.Expire(callCtx, key, windowTTL)
	lr := pipe.LRange(callCtx, key, 0, int64(windowSize-1))
	if _, err := pipe.Exec(callCtx); err != nil {
		common.SysLog(fmt.Sprintf("refusal_demote: redis pipe failed user=%d err=%v", userID, err))
		return false, 0, 0
	}
	items, err := lr.Result()
	if err != nil {
		common.SysLog(fmt.Sprintf("refusal_demote: redis lrange failed user=%d err=%v", userID, err))
		return false, 0, 0
	}
	total = len(items)
	for _, s := range items {
		if s == "1" {
			refusedCount++
		}
	}
	return true, total, refusedCount
}

func triggerDemotion(ctx context.Context, userID, total, refusedCount, thresholdPct, demoteSeconds int) {
	ratio := float64(refusedCount) / float64(total)
	if common.RedisEnabled && common.RDB != nil {
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(refusalRedisOpTimeoutMs)*time.Millisecond)
		defer cancel()
		if err := common.RDB.Set(callCtx, refusalDemoteKey(userID), "1", time.Duration(demoteSeconds)*time.Second).Err(); err != nil {
			common.SysLog(fmt.Sprintf("refusal_demote: set demote key failed user=%d err=%v, falling back to in-memory", userID, err))
			common.UserRefusalTrackerInstance().MarkDemoted(userID, time.Now().Add(time.Duration(demoteSeconds)*time.Second))
		}
	} else {
		common.UserRefusalTrackerInstance().MarkDemoted(userID, time.Now().Add(time.Duration(demoteSeconds)*time.Second))
	}
	logger.LogWarn(ctx, fmt.Sprintf("refusal_demote: user=%d ratio=%.2f (%d/%d) threshold=%d%% duration=%ds",
		userID, ratio, refusedCount, total, thresholdPct, demoteSeconds))
}

// IsUserDemoted 在渠道分发前检查用户是否处于拒答降级态。
// 返回 (demoted, ratio, windowCount)：ratio 仅在当前窗口可读时非零，
// 便于审计日志展示；无法读到窗口时 ratio=0, windowCount=0。
func IsUserDemoted(c *gin.Context, userID int) (demoted bool, ratio float64, windowCount int) {
	if userID <= 0 {
		return false, 0, 0
	}
	if !isRefusalDemoteEffectiveForUser(userID) {
		return false, 0, 0
	}
	demoted = isDemotedBackend(c, userID)
	if !demoted {
		return false, 0, 0
	}
	total, refusedCount := snapshotWindow(c, userID)
	if total > 0 {
		ratio = float64(refusedCount) / float64(total)
	}
	return true, ratio, total
}

func isDemotedBackend(ctx context.Context, userID int) bool {
	if common.RedisEnabled && common.RDB != nil {
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(refusalRedisOpTimeoutMs)*time.Millisecond)
		defer cancel()
		n, err := common.RDB.Exists(callCtx, refusalDemoteKey(userID)).Result()
		if err == nil {
			return n > 0
		}
		common.SysLog(fmt.Sprintf("refusal_demote: redis exists failed user=%d err=%v, falling back", userID, err))
	}
	return common.UserRefusalTrackerInstance().IsDemoted(userID)
}

func snapshotWindow(ctx context.Context, userID int) (total, refusedCount int) {
	if common.RedisEnabled && common.RDB != nil {
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(refusalRedisOpTimeoutMs)*time.Millisecond)
		defer cancel()
		items, err := common.RDB.LRange(callCtx, refusalWindowKey(userID), 0, -1).Result()
		if err == nil {
			for _, s := range items {
				if s == "1" {
					refusedCount++
				}
			}
			return len(items), refusedCount
		}
	}
	return common.UserRefusalTrackerInstance().Snapshot(userID)
}
