package constant

type ContextKey string

const (
	ContextKeyTokenCountMeta  ContextKey = "token_count_meta"
	ContextKeyPromptTokens    ContextKey = "prompt_tokens"
	ContextKeyEstimatedTokens ContextKey = "estimated_tokens"

	ContextKeyOriginalModel     ContextKey = "original_model"
	ContextKeyRequestStartTime  ContextKey = "request_start_time"
	ContextKeySeedanceV3Request ContextKey = "seedance_v3_request"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyTokenGroupList         ContextKey = "token_group_list"
	ContextKeyTokenSpecificChannelId ContextKey = "specific_channel_id"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenCrossGroupRetry   ContextKey = "token_cross_group_retry"

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelOrganization      ContextKey = "channel_organization"
	ContextKeyChannelAutoBan           ContextKey = "auto_ban"
	ContextKeyChannelModelMapping      ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey        ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex     ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey               ContextKey = "channel_key"

	ContextKeyAutoGroup           ContextKey = "auto_group"
	ContextKeyAutoGroupIndex      ContextKey = "auto_group_index"
	ContextKeyAutoGroupRetryIndex ContextKey = "auto_group_retry_index"

	/* user related keys */
	ContextKeyUserId         ContextKey = "id"
	ContextKeyUserSetting    ContextKey = "user_setting"
	ContextKeyUserQuota      ContextKey = "user_quota"
	ContextKeyUserStatus     ContextKey = "user_status"
	ContextKeyUserEmail      ContextKey = "user_email"
	ContextKeyUserGroup      ContextKey = "user_group"
	ContextKeyUsingGroup     ContextKey = "group"
	ContextKeyUserName       ContextKey = "username"
	ContextKeyUserOverloaded ContextKey = "user_overloaded"

	// ContextKeyRefusalDemoted 标记本次请求因「用户拒答比超限」被降级到兜底渠道。
	// 由 middleware.UserConcurrencyLimit 在渠道分发前写入；同时也会写
	// ContextKeyUserOverloaded=true 以复用 Distribute 中间件已有的
	// FallbackChannelId 重导逻辑。
	ContextKeyRefusalDemoted ContextKey = "refusal_demoted"
	// ContextKeyRefusalDemoteRatio 命中降级时记录当前滑窗的拒答比
	// （0~1 的 float64），仅用于审计日志。
	ContextKeyRefusalDemoteRatio ContextKey = "refusal_demote_ratio"
	// ContextKeyRefusalDemoteWindow 命中降级时记录当前滑窗样本总数。
	ContextKeyRefusalDemoteWindow ContextKey = "refusal_demote_window"
	// ContextKeyConcurrencyOverloaded 标记本次请求因「用户并发超限」而被
	// 置为 ContextKeyUserOverloaded；仅用于区分 fallback_from 的触发来源，
	// 不影响 Distribute 中间件的实际行为。
	ContextKeyConcurrencyOverloaded ContextKey = "concurrency_overloaded"

	// ContextKeyDistillDemoted 标记本次请求因「用户命中蒸馏检测」被重导到
	// **蒸馏兜底渠道**（DistillDetectSetting.FallbackChannelId，全局一个），
	// 与各渠道自配的 FallbackChannelId、以及拒答降级的兜底都不是同一条链路。
	//
	// ⛔ 不能被拒答降级的 ContextKeyUserOverloaded 覆盖：两者同时命中时，
	// 蒸馏优先 —— 蒸馏是用户在批量薅模型输出，比拒答更该被切走。
	ContextKeyDistillDemoted ContextKey = "distill_demoted"
	// ContextKeyDistillDemotedScore 命中蒸馏时这次判定的最高分，仅供审计日志。
	ContextKeyDistillDemotedScore ContextKey = "distill_demoted_score"
	// ContextKeyDistillFallbackChannelId 本次请求「实际被蒸馏兜底切到了哪个渠道」。
	// 重导成功才写；兜底不可用（走了 429）时不写，据此区分两种结局。
	ContextKeyDistillFallbackChannelId ContextKey = "distill_fallback_channel_id"
	// ContextKeyDistillOriginGroup / ContextKeyDistillFallbackGroup 记录蒸馏兜底
	// 前后的**计费分组**，供消费日志对账时一眼看出这笔按谁的价格结算。
	// 只在真的换了渠道时写；分组没变（例如原分组在兜底渠道里也有）时两者相同。
	ContextKeyDistillOriginGroup   ContextKey = "distill_origin_group"
	ContextKeyDistillFallbackGroup ContextKey = "distill_fallback_group"
	// ContextKeyConcurrencyCount / ContextKeyConcurrencyLimit 记录并发超限命中
	// 时的 in-flight 计数和上限，供 Distribute 中间件在决定「重导兜底/429」后
	// 异步发 TG 时拼消息用。
	ContextKeyConcurrencyCount ContextKey = "concurrency_count"
	ContextKeyConcurrencyLimit ContextKey = "concurrency_limit"
	// ContextKeyRefusalRefusedCount 命中降级时记录当前滑窗内「拒答」样本数，
	// 配合 ContextKeyRefusalDemoteWindow 还原 refused/total 用于 TG 文案。
	ContextKeyRefusalRefusedCount ContextKey = "refusal_demote_refused_count"

	// 兜底渠道重导的审计字段。仅在 Distribute 把请求从原渠道改到渠道的
	// FallbackChannelId 时写入；消费日志生成阶段读出并落到 other.fallback_from
	// 顶层字段，供前端日志列表展示「原渠道 → 当前渠道」。
	ContextKeyFallbackFromChannelId   ContextKey = "fallback_from_channel_id"
	ContextKeyFallbackFromChannelName ContextKey = "fallback_from_channel_name"
	ContextKeyFallbackFromGroup       ContextKey = "fallback_from_group"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"

	// ContextKeyFileSourcesToCleanup stores file sources that need cleanup when request ends
	ContextKeyFileSourcesToCleanup ContextKey = "file_sources_to_cleanup"

	// ContextKeyAdminRejectReason stores an admin-only reject/block reason extracted from upstream responses.
	// It is not returned to end users, but can be persisted into consume/error logs for debugging.
	ContextKeyAdminRejectReason ContextKey = "admin_reject_reason"

	// ContextKeyBillingExemptReason marks a successful upstream response that must not be charged,
	// e.g. a Messages API refusal that produced no output (Anthropic does not bill it and its usage
	// numbers are informational only). Neither quota nor token usage is recorded for such a request.
	ContextKeyBillingExemptReason ContextKey = "billing_exempt_reason"

	// ContextKeyLanguage stores the user's language preference for i18n
	ContextKeyLanguage ContextKey = "language"
	ContextKeyIsStream ContextKey = "is_stream"

	// ContextKeyAuditLogged marks that the current request has already recorded
	// a manage/operation audit log inside the handler. When set, the admin-audit
	// fallback in authHelper (finishAdminAudit) skips its record to avoid
	// duplicate entries.
	ContextKeyAuditLogged ContextKey = "audit_logged"

	// ContextKeyRefundReason 记录本次请求触发预扣退还的原因（字符串枚举），
	// 由 relay handler 层在决定「豁免计费」时写入。会被 processChannelError
	// 写入 logs 表的 other.refund_reason 字段，供跨系统对账（例如 sub2api 侧
	// 反查、判定「sub2api 已扣但 newApi 已退」）识别。
	//
	// 现有取值：
	//   - "upstream_stream_error"     上游 SSE 明确以错误事件终止（event: error 等）
	//   - "client_aborted_no_data"    客户端在收到任何上游数据前就断开
	ContextKeyRefundReason ContextKey = "refund_reason"
)

// Refund reason 枚举值（与 ContextKeyRefundReason 配套）。
const (
	RefundReasonUpstreamStreamError = "upstream_stream_error"
	RefundReasonClientAbortedNoData = "client_aborted_no_data"
)
