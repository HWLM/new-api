package middleware

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type ModelRequest struct {
	Model string `json:"model"`
	Group string `json:"group,omitempty"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		var channel *model.Channel
		channelId, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId)
		modelRequest, shouldSelectChannel, err := getModelRequest(c)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		if ok {
			id, err := strconv.Atoi(channelId.(string))
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			if channel.Status != common.ChannelStatusEnabled {
				abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorChannelDisabled))
				return
			}
		} else {
			// Select a channel for the user
			// check token model mapping
			modelLimitEnable := common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
			if modelLimitEnable {
				s, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
				if !ok {
					// token model limit is empty, all models are not allowed
					abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenNoModelAccess))
					return
				}
				var tokenModelLimit map[string]bool
				tokenModelLimit, ok = s.(map[string]bool)
				if !ok {
					tokenModelLimit = map[string]bool{}
				}
				matchName := ratio_setting.FormatMatchingModelName(modelRequest.Model) // match gpts & thinking-*
				if _, ok := tokenModelLimit[matchName]; !ok {
					abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenModelForbidden, map[string]any{"Model": modelRequest.Model}))
					return
				}
			}

			if shouldSelectChannel {
				if modelRequest.Model == "" {
					abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorModelNameRequired))
					return
				}
				var selectGroup string
				usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
				// check path is /pg/chat/completions
				if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
					playgroundRequest := &dto.PlayGroundRequest{}
					err = common.UnmarshalBodyReusable(c, playgroundRequest)
					if err != nil {
						abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidPlayground, map[string]any{"Error": err.Error()}))
						return
					}
					if playgroundRequest.Group != "" {
						if !service.GroupInUserUsableGroups(usingGroup, playgroundRequest.Group, c.GetInt("role")) && playgroundRequest.Group != usingGroup {
							abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorGroupAccessDenied))
							return
						}
						usingGroup = playgroundRequest.Group
						common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
					}
				}

				if preferredChannelID, found := service.GetPreferredChannelByAffinity(c, modelRequest.Model, usingGroup); found {
					affinityUsable := false
					preferred, err := model.CacheGetChannel(preferredChannelID)
					if err == nil && preferred != nil && preferred.Status == common.ChannelStatusEnabled &&
						channelSupportsRequestPath(preferred, c.Request.URL.Path, modelRequest.Model) {
						if usingGroup == "auto" {
							userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
							autoGroups := service.GetUserAutoGroup(userGroup, c.GetInt("role"))
							for _, g := range autoGroups {
								if model.IsChannelEnabledForGroupModel(g, modelRequest.Model, preferred.Id) {
									selectGroup = g
									common.SetContextKey(c, constant.ContextKeyAutoGroup, g)
									channel = preferred
									affinityUsable = true
									service.MarkChannelAffinityUsed(c, g, preferred.Id)
									break
								}
							}
						} else if model.IsChannelEnabledForGroupModel(usingGroup, modelRequest.Model, preferred.Id) {
							channel = preferred
							selectGroup = usingGroup
							affinityUsable = true
							service.MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
						}
					}
					if !affinityUsable && !service.ShouldKeepChannelAffinityOnChannelDisabled() {
						service.ClearCurrentChannelAffinityCache(c)
					}
				}

				if channel == nil {
					channel, selectGroup, err = service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
						Ctx:         c,
						ModelName:   modelRequest.Model,
						TokenGroup:  usingGroup,
						RequestPath: c.Request.URL.Path,
						Retry:       common.GetPointer(0),
						Preselect:   true,
					})
					if err != nil {
						showGroup := usingGroup
						if usingGroup == "auto" {
							showGroup = fmt.Sprintf("auto(%s)", selectGroup)
						}
						message := i18n.T(c, i18n.MsgDistributorGetChannelFailed, map[string]any{"Group": showGroup, "Model": modelRequest.Model, "Error": err.Error()})
						// 如果错误，但是渠道不为空，说明是数据库一致性问题
						//if channel != nil {
						//	common.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
						//	message = "数据库一致性已被破坏，请联系管理员"
						//}
						abortWithOpenAiMessage(c, http.StatusServiceUnavailable, message, types.ErrorCodeModelNotFound)
						return
					}
					if channel == nil {
						abortWithOpenAiMessage(c, http.StatusServiceUnavailable, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": modelRequest.Model}), types.ErrorCodeModelNotFound)
						return
					}
				}

				// 蒸馏检测（规格 §16）：只有本次分发到的渠道在检测白名单里才跑，
				// 其余渠道连每分钟请求量都不累加。
				// ⛔ 必须在下面的兜底重导之前：命中并确认的那一条要立刻改走兜底渠道。
				servedChannelID := channel.Id
				distillHit := runDistillDetection(c, channel, modelRequest.Model)
				if distillHit != nil {
					// 命中记录回填路由：原渠道 = 重导前选中的渠道，兜底渠道 id 看后面
					// 到底有没有真切（429 拒绝时为 0）。defer 是为了两条结局都覆盖到。
					defer func(hit *model.DistillDetectHit, originID int) {
						service.UpdateDistillHitRouting(hit, originID,
							common.GetContextKeyInt(c, constant.ContextKeyDistillFallbackChannelId))
					}(distillHit, servedChannelID)
				}

				// 蒸馏检测兜底：用户命中蒸馏判定后，整段流量切到「蒸馏检测」卡片里
				// 配置的**全局**兜底渠道。⛔ 与渠道自配的 FallbackChannelId、以及拒答
				// 降级的兜底都不是同一条链路，两者同时命中时蒸馏优先。
				// 兜底不可用 → 429，code 固定 distill_fallback_unavailable，文案与
				// 拒答降级的「风控降级」明确区分，运营一眼能认出是蒸馏兜底。
				if channel != nil && common.GetContextKeyBool(c, constant.ContextKeyDistillDemoted) {
					originalChannelID := channel.Id
					originGroup := selectGroup
					if fb := distillFallbackChannel(c, channel.Id, modelRequest.Model, selectGroup); fb != nil {
						common.SetContextKey(c, constant.ContextKeyFallbackFromChannelId, channel.Id)
						common.SetContextKey(c, constant.ContextKeyFallbackFromChannelName, channel.Name)
						common.SetContextKey(c, constant.ContextKeyFallbackFromGroup, originGroup)
						common.SetContextKey(c, constant.ContextKeyDistillFallbackChannelId, fb.channel.Id)
						channel = fb.channel
						selectGroup = fb.group
						// 计费分组（规格 §15）：与超限/拒答兜底不同，蒸馏兜底是强制降级，
						// 必须按兜底渠道的分组结算，否则用户在原分组下继续享受原倍率。
						common.SetContextKey(c, constant.ContextKeyUsingGroup, selectGroup)
						common.SetContextKey(c, constant.ContextKeyAutoGroup, selectGroup)
						common.SetContextKey(c, constant.ContextKeyDistillOriginGroup, originGroup)
						common.SetContextKey(c, constant.ContextKeyDistillFallbackGroup, selectGroup)
						common.SysLog(fmt.Sprintf("[distribute] user=%d distill demoted: channel %d (group=%s) -> distill fallback %d (billing group=%s)",
							c.GetInt("id"), originalChannelID, originGroup, fb.channel.Id, selectGroup))
					} else {
						common.SysLog(fmt.Sprintf("[distribute] user=%d rejected: distill demoted but fallback channel %d unusable",
							c.GetInt("id"), operation_setting.GetDistillDetectSetting().FallbackChannelId))
						abortWithOpenAiMessage(c, http.StatusTooManyRequests,
							"您账号当前处于风控降级状态（蒸馏检测），且无可用兜底渠道，请联系管理员",
							types.ErrorCodeDistillFallbackUnavailable)
						return
					}
				}

				// 兜底渠道重导：使用者当前 in-flight 超限，且命中渠道配置了
				// FallbackChannelId，则改走该兜底渠道。只做一跳，不递归。
				// 若超限但未配置兜底（或兜底本身不可用），直接 429 拒绝，
				// 保持「超限请求必须走兜底」的硬承诺。
				//
				// ⛔ 蒸馏兜底已经生效时不再走这一跳：否则兜底渠道若自己还配了
				// FallbackChannelId，就会变成「蒸馏兜底 → 再按超限跳到别处」的
				// 两跳链路，运维对账时根本看不出流量最后去了哪。蒸馏优先。
				distillRedirected := common.GetContextKeyInt(c, constant.ContextKeyDistillFallbackChannelId) > 0
				overloaded := common.GetContextKeyBool(c, constant.ContextKeyUserOverloaded)
				if channel != nil {
					common.SysLog(fmt.Sprintf("[distribute] user=%d model=%q selected channel=%d (fallback_channel_id=%d) overloaded=%v",
						c.GetInt("id"), modelRequest.Model, channel.Id, channel.FallbackChannelId, overloaded))
				}
				if overloaded && channel != nil && !distillRedirected {
					originalChannelID := channel.Id
					if fb := redirectToFallbackChannel(c, channel, modelRequest.Model, selectGroup); fb != nil {
						// 记录重导前的原渠道信息，供消费日志审计
						common.SetContextKey(c, constant.ContextKeyFallbackFromChannelId, channel.Id)
						common.SetContextKey(c, constant.ContextKeyFallbackFromChannelName, channel.Name)
						common.SetContextKey(c, constant.ContextKeyFallbackFromGroup, selectGroup)
						channel = fb.channel
						selectGroup = fb.group
						common.SetContextKey(c, constant.ContextKeyUsingGroup, selectGroup)
						common.SetContextKey(c, constant.ContextKeyAutoGroup, selectGroup)
						notifyOverloadTg(c, originalChannelID, fb.channel.Id, service.OverloadFallbackRedirected)
					} else {
						// 兜底不可用（未配置 / 自环 / 已禁用 / 不支持模型等）→ 429
						// 按触发来源区分返回文案：并发超限直说，拒答降级只给中性
						// 「风控降级」口径，不透露内部指标。
						common.SysLog(fmt.Sprintf("[distribute] user=%d rejected: overloaded without usable fallback (channel=%d fallback_channel_id=%d)",
							c.GetInt("id"), channel.Id, channel.FallbackChannelId))
						notifyOverloadTg(c, originalChannelID, 0, service.OverloadFallbackUnavailable)
						abortWithOpenAiMessage(c, http.StatusTooManyRequests, overloadRejectionMessage(c))
						return
					}
				}
			}
		}
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		SetupContextForSelectedChannel(c, channel, modelRequest.Model)
		c.Next()
		if channel != nil && c.Writer != nil && c.Writer.Status() < http.StatusBadRequest {
			service.RecordChannelAffinity(c, channel.Id)
		}
	}
}

// channelSupportsRequestPath reports whether a channel can serve the request path.
// Only Advanced Custom (type 58) channels are path-checked; all other channel types
// always pass. A type-58 channel is usable only when one of its routes matches.
func channelSupportsRequestPath(channel *model.Channel, requestPath string, requestModel string) bool {
	if channel == nil {
		return false
	}
	if channel.Type != constant.ChannelTypeAdvancedCustom {
		return true
	}
	config := channel.GetOtherSettings().AdvancedCustom
	return config != nil && config.SupportsPathForModel(requestPath, requestModel)
}

type fallbackResolution struct {
	channel *model.Channel
	group   string
}

// redirectToFallbackChannel 读取 channel.FallbackChannelId，校验兜底渠道可用并
// 支持当前模型后，返回替换后的渠道和分组。任何不满足条件的情况（未配置、自环、
// 已禁用、模型不支持）都返回 nil，保留原渠道。
//
// 计费分组策略：**兜底时保留使用者原分组（origGroup）**，而不是切到兜底渠道
// 所属的分组。原因：
//   - 兜底是后台强制路由，不应改变用户看到的计费口径（避免跨分组计费漂移）
//   - 兜底渠道可能根本没绑原分组（例如用户在 default、B 只绑 backup）；若切到
//     backup，使用者原本在 default 下的 GroupRatio 配置就失效，可能命中默认倍率
//   - 便于日志与账单对账：无论是否触发兜底，同一个请求的 group 字段始终是用户
//     本来会使用的那个
//
// 仅当 origGroup 为空或等于 "auto"（例如 token 使用 auto 分组、兜底渠道未参与
// 自动分组链时）才回退到兜底渠道自己的第一个分组。
//
// 仅做一跳，不处理兜底渠道自身的 FallbackChannelId，避免链式跳转。
func redirectToFallbackChannel(c *gin.Context, original *model.Channel, modelName, origGroup string) *fallbackResolution {
	if original == nil || original.FallbackChannelId <= 0 || original.FallbackChannelId == original.Id {
		return nil
	}
	fb, err := model.CacheGetChannel(original.FallbackChannelId)
	if err != nil || fb == nil {
		logger.LogWarn(c, fmt.Sprintf("fallback channel %d not found for channel %d: %v", original.FallbackChannelId, original.Id, err))
		return nil
	}
	if fb.Status != common.ChannelStatusEnabled {
		logger.LogWarn(c, fmt.Sprintf("fallback channel %d is disabled, keeping original channel %d", fb.Id, original.Id))
		return nil
	}
	if !channelSupportsModel(fb, modelName) {
		logger.LogWarn(c, fmt.Sprintf("fallback channel %d does not support model %q, keeping original channel %d", fb.Id, modelName, original.Id))
		return nil
	}
	if !channelSupportsRequestPath(fb, c.Request.URL.Path, modelName) {
		logger.LogWarn(c, fmt.Sprintf("fallback channel %d does not support request path %q, keeping original channel %d", fb.Id, c.Request.URL.Path, original.Id))
		return nil
	}
	group := pickFallbackChannelGroup(fb, origGroup)
	if group == "" {
		logger.LogWarn(c, fmt.Sprintf("fallback channel %d has no group configured and origGroup is empty, keeping original channel %d", fb.Id, original.Id))
		return nil
	}
	logger.LogInfo(c, fmt.Sprintf("overload redirect: user=%d model=%q from channel %d (group=%s) to channel %d (billing group=%s)",
		c.GetInt("id"), modelName, original.Id, origGroup, fb.Id, group))
	return &fallbackResolution{channel: fb, group: group}
}

// distillFallbackChannel 解析「蒸馏检测」卡片里配置的那个全局兜底渠道。
//
// 与 redirectToFallbackChannel 的区别（⛔ 不要合并成一套）：
//   - 来源不同：这里读的是 DistillDetectSetting.FallbackChannelId（全局一个），
//     不是 channel.FallbackChannelId（每个渠道自配）；
//   - 自环判定不同：全局兜底渠道可能恰好就是当前选中的渠道，那也**不是**错误配置
//     ——用户命中蒸馏时，若流量本来就在兜底渠道上，就继续留在原渠道即可，
//     不能因此返回 429（否则用户会因为「已经在我该去的渠道上」被拒）。
//     这种情况按「重导成功、原地不动」处理。
//
// 任何真正不可用的情况（未配置、渠道不存在、已禁用、不支持模型/路径、无分组）
// 返回 nil，由调用方回 429 distill_fallback_unavailable。
func distillFallbackChannel(c *gin.Context, currentChannelID int, modelName, origGroup string) *fallbackResolution {
	s := operation_setting.GetDistillDetectSetting()
	if s == nil || s.FallbackChannelId <= 0 {
		return nil
	}
	// 当前渠道就是兜底渠道：原地不动即可，视为已重导。
	// ⛔ 必须用调用方传进来的 currentChannelID：本函数在 SetupContextForSelectedChannel
	// 之前执行，那时 ContextKeyChannelId 还没写入，读它恒为 0（自环判断永远不成立）。
	if currentChannelID > 0 && currentChannelID == s.FallbackChannelId {
		if ch, err := model.CacheGetChannel(s.FallbackChannelId); err == nil && ch != nil {
			return &fallbackResolution{channel: ch, group: origGroup}
		}
	}
	fb, err := model.CacheGetChannel(s.FallbackChannelId)
	if err != nil || fb == nil {
		logger.LogWarn(c, fmt.Sprintf("distill fallback channel %d not found: %v", s.FallbackChannelId, err))
		return nil
	}
	if fb.Status != common.ChannelStatusEnabled {
		logger.LogWarn(c, fmt.Sprintf("distill fallback channel %d is disabled", fb.Id))
		return nil
	}
	if !channelSupportsModel(fb, modelName) {
		logger.LogWarn(c, fmt.Sprintf("distill fallback channel %d does not support model %q", fb.Id, modelName))
		return nil
	}
	if !channelSupportsRequestPath(fb, c.Request.URL.Path, modelName) {
		logger.LogWarn(c, fmt.Sprintf("distill fallback channel %d does not support request path %q", fb.Id, c.Request.URL.Path))
		return nil
	}
	group, ok := pickDistillFallbackGroup(c, fb, origGroup)
	if !ok {
		// §15：兜底渠道没有任何配了倍率的分组时，维持原分组并打 warn ——
		// 这时代码会按原分组结算，运营需要知道「兜底其实没改价」。
		return &fallbackResolution{channel: fb, group: group}
	}
	return &fallbackResolution{channel: fb, group: group}
}

// pickDistillFallbackGroup 决定蒸馏兜底后的**计费分组**（规格 §15）。
//
// ⛔ 与 pickFallbackChannelGroup 的策略不同，不要合并：
//   - 并发超限 / 拒答降级的兜底沿用用户原分组，是为了不打乱用户看到的计费口径；
//   - 蒸馏兜底是对**滥用行为**的强制降级，必须按兜底渠道的价格结算，否则用户
//     在原分组的倍率照旧生效，「转兜底」就只是换了上游、没换价（这是实测到的
//     bug：转到兜底分组后没按兜底的价格计费）。
//
// 取法：
//  1. 兜底渠道里有与用户原分组同名的分组 → 用它（口径最平滑）；
//  2. 否则取兜底渠道里第一个**配了倍率**的分组（没配倍率的分组会走默认 1.0，
//     看不出是兜底价）；
//  3. 都没有 → 维持原分组，返回 ok=false 让调用方打 warn。
func pickDistillFallbackGroup(c *gin.Context, ch *model.Channel, origGroup string) (group string, ok bool) {
	if ch == nil {
		return origGroup, false
	}
	groups := ch.GetGroups()
	if origGroup != "" && origGroup != "auto" {
		for _, g := range groups {
			if g == origGroup {
				return g, true
			}
		}
	}
	for _, g := range groups {
		if g == "" || g == "auto" {
			continue
		}
		if ratio_setting.ContainsGroupRatio(g) {
			return g, true
		}
	}
	logger.LogWarn(c, fmt.Sprintf("distill fallback channel %d has no group with a configured ratio (groups=%v), keeping origin group %q",
		ch.Id, groups, origGroup))
	return origGroup, false
}

// channelSupportsModel 以 "," 包裹两端做精确匹配，避免 "gpt-4" 误中 "gpt-4o"。
func channelSupportsModel(channel *model.Channel, modelName string) bool {
	if channel == nil || modelName == "" {
		return false
	}
	return strings.Contains(","+channel.Models+",", ","+modelName+",")
}

// pickFallbackChannelGroup 决定兜底命中时写入 ContextKeyUsingGroup/AutoGroup 的
// 分组。策略：
//  1. 使用者原分组是具体分组（非空、非 "auto"）→ 直接沿用，保持计费口径稳定。
//     即使兜底渠道的 Group 字段没绑该分组也成立：兜底是后台强制路由，不受
//     Ability/可见性约束。
//  2. 使用者原分组是 "auto" 或为空（例如 playground 走到了 auto 链但未定格）
//     → 退回使用兜底渠道自己的首个分组，至少能走通。
func pickFallbackChannelGroup(channel *model.Channel, preferred string) string {
	if preferred != "" && preferred != "auto" {
		return preferred
	}
	if channel == nil {
		return ""
	}
	groups := channel.GetGroups()
	if len(groups) == 0 {
		return ""
	}
	return groups[0]
}

// notifyOverloadTg 在 Distribute 决定「重导兜底 or 429」后，按本次请求命中的告警类型
// 异步发送 TG。并发超限与拒答降级可能同时命中（例如用户同时触发两套限流），
// 此时按独立开关分别发送，各自走各自的节流窗口。
func notifyOverloadTg(c *gin.Context, originalChannelID, fallbackChannelID int, outcome service.OverloadFallbackOutcome) {
	userID := c.GetInt("id")
	if userID <= 0 {
		return
	}
	if common.GetContextKeyBool(c, constant.ContextKeyConcurrencyOverloaded) {
		count := common.GetContextKeyInt(c, constant.ContextKeyConcurrencyCount)
		limit := common.GetContextKeyInt(c, constant.ContextKeyConcurrencyLimit)
		service.NotifyConcurrencyOverload(userID, count, limit, originalChannelID, fallbackChannelID, outcome)
	}
	if common.GetContextKeyBool(c, constant.ContextKeyRefusalDemoted) {
		refusedCount := common.GetContextKeyInt(c, constant.ContextKeyRefusalRefusedCount)
		windowCount := common.GetContextKeyInt(c, constant.ContextKeyRefusalDemoteWindow)
		s := operation_setting.GetRefusalDemoteSetting()
		service.NotifyRefusalDemoteTriggered(userID, refusedCount, windowCount, s.ThresholdPct, s.DemoteSeconds,
			originalChannelID, fallbackChannelID, outcome)
	}
}

// overloadRejectionMessage 组装 429 返回给客户端的中文提示。
// 并发超限：直说超限（这是用户自己的行为，提示有助于自纠）。
// 仅拒答降级：只给「风控降级」中性口径，不透露内部拒答比指标。
// 两者同时命中：合并一句。
func overloadRejectionMessage(c *gin.Context) string {
	concurrency := common.GetContextKeyBool(c, constant.ContextKeyConcurrencyOverloaded)
	refusal := common.GetContextKeyBool(c, constant.ContextKeyRefusalDemoted)
	switch {
	case concurrency && refusal:
		return "您账号当前并发请求数已超限，且处于风控降级状态，请联系管理员"
	case refusal:
		return "您账号当前处于风控降级状态，请联系管理员"
	default:
		return "您账号并发请求数已超限，请联系管理员"
	}
}

// getModelFromRequest 从请求中读取模型信息
// 根据 Content-Type 自动处理：
// - application/json
// - application/x-www-form-urlencoded
// - multipart/form-data
func getModelFromRequest(c *gin.Context) (*ModelRequest, error) {
	if strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		modelRequest, err := getModelFromJSONBody(c)
		if err != nil {
			return nil, errors.New(i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
		}
		return modelRequest, nil
	}

	var modelRequest ModelRequest
	err := common.UnmarshalBodyReusable(c, &modelRequest)
	if err != nil {
		return nil, errors.New(i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
	}
	return &modelRequest, nil
}

func getModelFromJSONBody(c *gin.Context) (*ModelRequest, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if !gjson.ValidBytes(requestBody) {
		return nil, errors.New("invalid JSON request body")
	}

	values := gjson.GetManyBytes(requestBody, "model", "group")
	model, err := getJSONStringValue(values[0], "model")
	if err != nil {
		return nil, err
	}
	group, err := getJSONStringValue(values[1], "group")
	if err != nil {
		return nil, err
	}

	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	c.Request.Body = io.NopCloser(storage)

	return &ModelRequest{
		Model: model,
		Group: group,
	}, nil
}

func getJSONStringValue(result gjson.Result, field string) (string, error) {
	if !result.Exists() || result.Type == gjson.Null {
		return "", nil
	}
	if result.Type != gjson.String {
		return "", fmt.Errorf("field %s must be a string", field)
	}
	return result.String(), nil
}

func getModelRequest(c *gin.Context) (*ModelRequest, bool, error) {
	var modelRequest ModelRequest
	shouldSelectChannel := true
	var err error
	if strings.Contains(c.Request.URL.Path, "/mj/") {
		relayMode := relayconstant.Path2RelayModeMidjourney(c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeMidjourneyTaskFetch ||
			relayMode == relayconstant.RelayModeMidjourneyTaskFetchByCondition ||
			relayMode == relayconstant.RelayModeMidjourneyNotify ||
			relayMode == relayconstant.RelayModeMidjourneyTaskImageSeed {
			shouldSelectChannel = false
		} else {
			midjourneyRequest := taskdto.MidjourneyRequest{}
			err = common.UnmarshalBodyReusable(c, &midjourneyRequest)
			if err != nil {
				return nil, false, errors.New(i18n.T(c, i18n.MsgDistributorInvalidMidjourney, map[string]any{"Error": err.Error()}))
			}
			midjourneyModel, mjErr, success := service.GetMjRequestModel(relayMode, &midjourneyRequest)
			if mjErr != nil {
				return nil, false, fmt.Errorf("%s", mjErr.Description)
			}
			if midjourneyModel == "" {
				if !success {
					return nil, false, fmt.Errorf("%s", i18n.T(c, i18n.MsgDistributorInvalidParseModel))
				} else {
					// task fetch, task fetch by condition, notify
					shouldSelectChannel = false
				}
			}
			modelRequest.Model = midjourneyModel
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/suno/") {
		relayMode := relayconstant.Path2RelaySuno(c.Request.Method, c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeSunoFetch ||
			relayMode == relayconstant.RelayModeSunoFetchByID {
			shouldSelectChannel = false
		} else {
			modelName := service.CoverTaskActionToModelName(constant.TaskPlatformSuno, c.Param("action"))
			modelRequest.Model = modelName
		}
		c.Set("platform", string(constant.TaskPlatformSuno))
		c.Set("relay_mode", relayMode)
	} else if strings.HasPrefix(c.Request.URL.Path, "/api/v3/contents/generations/tasks") {
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			modelRequest.Model = req.Model
			relayMode = relayconstant.RelayModeVideoSubmit
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
			modelRequest.Model = getTaskOriginModelName(c)
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos/") && strings.HasSuffix(c.Request.URL.Path, "/remix") {
		relayMode := relayconstant.RelayModeVideoSubmit
		c.Set("relay_mode", relayMode)
		shouldSelectChannel = false
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos") {
		//curl https://api.openai.com/v1/videos \
		//  -H "Authorization: Bearer $OPENAI_API_KEY" \
		//  -F "model=sora-2" \
		//  -F "prompt=A calico cat playing a piano on stage"
		//	-F input_reference="@image.jpg"
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			relayMode = relayconstant.RelayModeVideoSubmit
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			if req != nil {
				modelRequest.Model = req.Model
			}
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
			modelRequest.Model = getTaskOriginModelName(c)
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/video/generations") {
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			modelRequest.Model = req.Model
			relayMode = relayconstant.RelayModeVideoSubmit
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
			modelRequest.Model = getTaskOriginModelName(c)
		}
		if _, ok := c.Get("relay_mode"); !ok {
			c.Set("relay_mode", relayMode)
		}
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models/") || strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
		// Gemini API 路径处理: /v1beta/models/gemini-2.0-flash:generateContent
		relayMode := relayconstant.RelayModeGemini
		modelName := extractModelNameFromGeminiPath(c.Request.URL.Path)
		if modelName != "" {
			modelRequest.Model = modelName
		}
		c.Set("relay_mode", relayMode)
	} else if !strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") && !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/realtime") {
		//wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01
		modelRequest.Model = c.Query("model")
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/moderations") {
		if modelRequest.Model == "" {
			modelRequest.Model = "text-moderation-stable"
		}
	}
	if strings.HasSuffix(c.Request.URL.Path, "embeddings") {
		if modelRequest.Model == "" {
			modelRequest.Model = c.Param("model")
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/images/generations") {
		modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "dall-e")
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1/images/edits") {
		//modelRequest.Model = common.GetStringIfEmpty(c.PostForm("model"), "gpt-image-1")
		contentType := c.ContentType()
		if slices.Contains([]string{gin.MIMEPOSTForm, gin.MIMEMultipartPOSTForm}, contentType) {
			req, err := getModelFromRequest(c)
			if err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/audio") {
		relayMode := relayconstant.RelayModeAudioSpeech
		if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/speech") {

			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "tts-1")
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/translations") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranslation
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranscription
		}
		c.Set("relay_mode", relayMode)
	}
	if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
		// playground chat completions
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
		modelRequest.Group = req.Group
		common.SetContextKey(c, constant.ContextKeyTokenGroup, modelRequest.Group)
	}

	if strings.HasPrefix(c.Request.URL.Path, "/v1/responses/compact") && modelRequest.Model != "" {
		modelRequest.Model = ratio_setting.WithCompactModelSuffix(modelRequest.Model)
	}
	return &modelRequest, shouldSelectChannel, nil
}

// 修复 #4834: GET /v1/video/generations/:task_id && /v1/video/:task_id 此前不解析 model，
// 当 token 启用「可用模型限制」时，下游 modelLimitEnable 校验会因
// modelRequest.Model 为空而误报 "This token has no access to model"。
// 从已存储的任务记录中回填 OriginModelName 即可让校验走在正确的模型上。
func getTaskOriginModelName(c *gin.Context) string {
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return ""
	}

	taskId := c.Param("task_id")
	if taskId == "" {
		// jimeng adapter
		taskId = c.GetString("task_id")
	}
	if taskId == "" {
		return ""
	}

	userId := c.GetInt("id")
	if task, exist, err := model.GetByTaskId(userId, taskId); err == nil && exist && task != nil {
		return task.Properties.OriginModelName
	}
	return ""
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	c.Set("original_model", modelName) // for retry
	if channel == nil {
		return types.NewError(errors.New("channel is nil"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	paramOverride := channel.GetParamOverride()
	headerOverride := channel.GetHeaderOverride()
	if mergedParam, applied := service.ApplyChannelAffinityOverrideTemplate(c, paramOverride); applied {
		paramOverride = mergedParam
	}
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, paramOverride)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, headerOverride)
	if nil != channel.OpenAIOrganization && *channel.OpenAIOrganization != "" {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, *channel.OpenAIOrganization)
	}
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())

	key, index, newAPIError := channel.GetNextEnabledKey()
	if newAPIError != nil {
		return newAPIError
	}
	if channel.ChannelInfo.IsMultiKey {
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, index)
	} else {
		// 必须设置为 false，否则在重试到单个 key 的时候会导致日志显示错误
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
	}
	// c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)

	// TODO: api_version统一
	switch channel.Type {
	case constant.ChannelTypeAzure:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeVertexAi:
		c.Set("region", channel.Other)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeGemini:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeAli:
		c.Set("plugin", channel.Other)
	case constant.ChannelCloudflare:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", channel.Other)
	}
	return nil
}

// extractModelNameFromGeminiPath 从 Gemini API URL 路径中提取模型名
// 输入格式: /v1beta/models/gemini-2.0-flash:generateContent
// 输出: gemini-2.0-flash
func extractModelNameFromGeminiPath(path string) string {
	// 查找 "/models/" 的位置
	modelsPrefix := "/models/"
	modelsIndex := strings.Index(path, modelsPrefix)
	if modelsIndex == -1 {
		return ""
	}

	// 从 "/models/" 之后开始提取
	startIndex := modelsIndex + len(modelsPrefix)
	if startIndex >= len(path) {
		return ""
	}

	// 查找 ":" 的位置，模型名在 ":" 之前
	colonIndex := strings.Index(path[startIndex:], ":")
	if colonIndex == -1 {
		// 如果没有找到 ":"，返回从 "/models/" 到路径结尾的部分
		return path[startIndex:]
	}

	// 返回模型名部分
	return path[startIndex : startIndex+colonIndex]
}
