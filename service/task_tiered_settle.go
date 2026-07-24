package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// settleTaskTieredExpr 用冻结的 tiered_expr 快照重跑表达式，实现视频/图片任务的
// 差额结算。设计目标：把上游返回的实际参数（resolution / duration / tokens）
// 覆盖到 pre-consume 阶段冻结的 TaskExprVars 上，然后走 billingexpr 的中央转换
// 路径重新计算 quota，最后调用 RecalculateTaskQuota 做差额补扣或退款。
//
// 该函数应在 settleTaskBillingOnComplete 里 PerCallBilling 短路之前调用；控制器
// 侧的持久化也已经把 tiered 场景下的 PerCallBilling 强制为 false，双重保护。
func settleTaskTieredExpr(ctx context.Context, task *model.Task, taskResult *relaycommon.TaskInfo) {
	bc := task.PrivateData.BillingContext
	if bc == nil || bc.TieredSnapshot == nil {
		return
	}

	snap := bc.TieredSnapshot
	vars := effectiveTaskExprVars(bc, taskResult)

	params := billingexpr.TokenParams{
		Seconds:    vars.Seconds,
		Resolution: vars.Resolution,
		Size:       vars.Size,
		HasVideo:   vars.HasVideo,
		HasImage:   vars.HasImage,
		N:          vars.N,
		Mode:       vars.Mode,
	}
	// 若上游返回了 token 用量，也一并塞进去 —— 表达式可选择性使用 p/c 做加价。
	if taskResult != nil {
		if taskResult.TotalTokens > 0 {
			params.P = float64(taskResult.TotalTokens)
			params.Len = float64(taskResult.TotalTokens)
		}
		if taskResult.CompletionTokens > 0 {
			params.C = float64(taskResult.CompletionTokens)
		}
	}

	request := billingexpr.RequestInput{}
	if len(bc.RequestBody) > 0 {
		request.Body = bc.RequestBody
	}

	tr, err := billingexpr.ComputeTieredQuotaWithRequest(snap, params, request)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("任务 %s tiered_expr 结算失败: %s（保留预扣额度 %s）",
			task.TaskID, err.Error(), logger.LogQuota(task.Quota)))
		return
	}

	actualQuota := tr.ActualQuotaAfterGroup
	if actualQuota < 0 {
		logger.LogError(ctx, fmt.Sprintf("任务 %s tiered_expr 结算得到非法负额度 %d，保留预扣", task.TaskID, actualQuota))
		return
	}
	reason := fmt.Sprintf("tiered_expr结算(tier=%s)", tr.MatchedTier)
	clamps := collectTieredClamps(tr.Clamp)

	// 让日志反映结算最终命中的档位与参数。只在此次退款/差额结算调用期间
	// 替换冻结值，完成后恢复，避免改变持久化的预扣快照。
	originalVars := bc.TieredExprVars
	bc.TieredExprVars = &vars
	defer func() { bc.TieredExprVars = originalVars }()
	if tr.MatchedTier != "" && tr.MatchedTier != snap.EstimatedTier {
		orig := snap.EstimatedTier
		snap.EstimatedTier = tr.MatchedTier
		defer func() { snap.EstimatedTier = orig }()
	}
	if actualQuota == 0 {
		// 表达式判定为免费 —— 全额退款。
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s tiered_expr 结算结果为 0，全额退还预扣 %s",
			task.TaskID, logger.LogQuota(task.Quota)))
		RefundTaskQuota(ctx, task, "tiered_expr结算为0")
		return
	}
	usage := TaskBillingUsage{}
	if taskResult != nil {
		usage.TotalTokens = taskResult.TotalTokens
		usage.CompletionTokens = taskResult.CompletionTokens
	}
	RecalculateTaskQuotaWithUsage(ctx, task, actualQuota, reason, usage, clamps...)
}

// effectiveTaskExprVars 把上游返回的实际 resolution / duration 覆盖到冻结变量。
// 上游没返回则沿用 pre-consume 冻结值；冻结值也没有则用零值（这时表达式若做
// 分档判断可能踩到 default 分支，属于表达式作者需要覆盖的情形）。
func effectiveTaskExprVars(bc *model.TaskBillingContext, taskResult *relaycommon.TaskInfo) model.TaskExprVars {
	var vars model.TaskExprVars
	if bc.TieredExprVars != nil {
		vars = *bc.TieredExprVars
	}
	if taskResult == nil {
		return vars
	}
	if taskResult.Resolution != "" {
		// 上游给的字符串可能是 "1080P" / "1920x1080" 等各种形式；同 pre-consume
		// 一样做归一化避免表达式作者要处理大小写和多形态。
		if norm := normalizeTaskResolution(taskResult.Resolution); norm != "" {
			vars.Resolution = norm
		}
	}
	if taskResult.DurationSeconds > 0 {
		s := float64(taskResult.DurationSeconds)
		if s > float64(relaycommon.MaxTaskDurationSeconds) {
			s = float64(relaycommon.MaxTaskDurationSeconds)
		}
		vars.Seconds = s
	}
	return vars
}

// normalizeTaskResolution 复用 taskcommon.NormalizeResolutionFromSize，
// 让 pre-consume 阶段的归一化和上游返回值的归一化走同一套规则。
func normalizeTaskResolution(s string) string {
	return taskcommon.NormalizeResolutionFromSize(s)
}

func collectTieredClamps(clamp *common.QuotaClamp) []*common.QuotaClamp {
	if clamp == nil {
		return nil
	}
	return []*common.QuotaClamp{clamp}
}
