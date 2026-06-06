package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// resolveTokenGroupList 解析当前请求 token 的分组优先级列表。
// 优先从 ContextKeyTokenGroupList 读取(由 auth 中间件写入),其次回退到字符串解析。
// 列表中的 "auto" 会就地展开为 GetUserAutoGroup(userGroup) 返回的具体分组,
// 最终结果去重并保留顺序。
func resolveTokenGroupList(ctx *gin.Context, tokenGroup string, userGroup string) []string {
	var raw []string
	if v, ok := common.GetContextKey(ctx, constant.ContextKeyTokenGroupList); ok {
		if list, ok := v.([]string); ok {
			raw = list
		}
	}
	if len(raw) == 0 && tokenGroup != "" {
		for _, p := range strings.Split(tokenGroup, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				raw = append(raw, p)
			}
		}
	}
	if len(raw) == 0 {
		return nil
	}

	expanded := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, g := range raw {
		if g == "auto" {
			for _, ag := range GetUserAutoGroup(userGroup) {
				if _, ok := seen[ag]; ok {
					continue
				}
				seen[ag] = struct{}{}
				expanded = append(expanded, ag)
			}
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		expanded = append(expanded, g)
	}
	return expanded
}

type RetryParam struct {
	Ctx          *gin.Context
	TokenGroup   string
	ModelName    string
	Retry        *int
	resetNextTry bool
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *RetryParam) ResetRetryNextTry() {
	p.resetNextTry = true
}

// CacheGetRandomSatisfiedChannel tries to get a random channel that satisfies the requirements.
// 尝试获取一个满足要求的随机渠道。
//
// For "auto" tokenGroup with cross-group Retry enabled:
// 对于启用了跨分组重试的 "auto" tokenGroup：
//
//   - Each group will exhaust all its priorities before moving to the next group.
//     每个分组会用完所有优先级后才会切换到下一个分组。
//
//   - Uses ContextKeyAutoGroupIndex to track current group index.
//     使用 ContextKeyAutoGroupIndex 跟踪当前分组索引。
//
//   - Uses ContextKeyAutoGroupRetryIndex to track the global Retry count when current group started.
//     使用 ContextKeyAutoGroupRetryIndex 跟踪当前分组开始时的全局重试次数。
//
//   - priorityRetry = Retry - startRetryIndex, represents the priority level within current group.
//     priorityRetry = Retry - startRetryIndex，表示当前分组内的优先级级别。
//
//   - When GetRandomSatisfiedChannel returns nil (priorities exhausted), moves to next group.
//     当 GetRandomSatisfiedChannel 返回 nil（优先级用完）时，切换到下一个分组。
//
// Example flow (2 groups, each with 2 priorities, RetryTimes=3):
// 示例流程（2个分组，每个有2个优先级，RetryTimes=3）：
//
//	Retry=0: GroupA, priority0 (startRetryIndex=0, priorityRetry=0)
//	         分组A, 优先级0
//
//	Retry=1: GroupA, priority1 (startRetryIndex=0, priorityRetry=1)
//	         分组A, 优先级1
//
//	Retry=2: GroupA exhausted → GroupB, priority0 (startRetryIndex=2, priorityRetry=0)
//	         分组A用完 → 分组B, 优先级0
//
//	Retry=3: GroupB, priority1 (startRetryIndex=2, priorityRetry=1)
//	         分组B, 优先级1
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	var channel *model.Channel
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)

	groupList := resolveTokenGroupList(param.Ctx, param.TokenGroup, userGroup)
	// 当 token 原始配置为 "auto" 但系统未启用 auto 分组时,展开后 groupList 为空,直接报错。
	if param.TokenGroup == "auto" && len(setting.GetAutoGroups()) == 0 {
		return nil, selectGroup, errors.New("auto groups is not enabled")
	}

	// 多分组优先级遍历:>1 个分组(包括 "auto" 展开后的情况)。
	if len(groupList) > 1 {
		// startGroupIndex: the group index to start searching from
		startGroupIndex := 0
		crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

		if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
			}
		}

		for i := startGroupIndex; i < len(groupList); i++ {
			currentGroup := groupList[i]
			// Calculate priorityRetry for current group
			priorityRetry := param.GetRetry()
			// If moved to a new group, reset priorityRetry
			if i > startGroupIndex {
				priorityRetry = 0
			}
			logger.LogDebug(param.Ctx, "Multi-group selecting: %s, priorityRetry: %d", currentGroup, priorityRetry)

			channel, _ = model.GetRandomSatisfiedChannel(currentGroup, param.ModelName, priorityRetry)
			if channel == nil {
				logger.LogDebug(param.Ctx, "No available channel in group %s for model %s at priorityRetry %d, trying next group", currentGroup, param.ModelName, priorityRetry)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				param.SetRetry(0)
				continue
			}
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, currentGroup)
			selectGroup = currentGroup
			logger.LogDebug(param.Ctx, "Multi-group selected: %s", currentGroup)

			// Prepare state for next retry
			if crossGroupRetry && priorityRetry >= common.RetryTimes {
				logger.LogDebug(param.Ctx, "Current group %s retries exhausted (priorityRetry=%d >= RetryTimes=%d), preparing switch to next group for next retry", currentGroup, priorityRetry, common.RetryTimes)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			}
			break
		}
	} else {
		// 单分组(或空列表):走原行为,保持完全等价。
		// groupList 可能为单元素;若为空则退化到 param.TokenGroup(可能是 userGroup)。
		queryGroup := param.TokenGroup
		if len(groupList) == 1 {
			queryGroup = groupList[0]
			selectGroup = queryGroup
		}
		channel, err = model.GetRandomSatisfiedChannel(queryGroup, param.ModelName, param.GetRetry())
		if err != nil {
			return nil, queryGroup, err
		}
	}
	return channel, selectGroup, nil
}
