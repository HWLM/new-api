package service

import (
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// 错误分类器:把请求结束时拿到的 (statusCode, errMsg, rawErrorCode) 归一化为:
//   - errorType 枚举(model.MetricsErrorType*)
//   - isBusiness 标记(用于过滤业务错误)
//   - normalizedErrorCode 字符串(用于 top10 分组)
//
// 业务关键词从 setting 包读(避免循环依赖)。
// 归一化策略:消息特征优先 → rawCode 字典 → rawCode 原值 → 状态码兜底。

// ClassifyError 判定错误类型和是否业务错误。
// statusCode < 400 时一律返回 (None, false)。
func ClassifyError(statusCode int, errMsg string) (errType int16, isBusiness bool) {
	if statusCode < 400 {
		return model.MetricsErrorTypeNone, false
	}

	if errMsg != "" {
		lower := strings.ToLower(errMsg)
		for _, kw := range setting.GetMetricsBusinessKeywords() {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return model.MetricsErrorTypeBusiness, true
			}
		}
	}

	switch {
	case statusCode == 402, statusCode == 403:
		return model.MetricsErrorTypeBusiness, true
	case statusCode == 429:
		return model.MetricsErrorTypeUpstream, false
	case statusCode >= 500 && statusCode < 600:
		return model.MetricsErrorTypeUpstream, false
	}

	lower := strings.ToLower(errMsg)
	if strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no such host") {
		return model.MetricsErrorTypeNetwork, false
	}

	return model.MetricsErrorTypeInternal, false
}

// 上游 raw error.code 归一化字典 — 留空,按需补充。
var errorCodeNormalize = map[string]string{}

// errorMessagePatterns 是按声明顺序匹配的错误消息特征 → 归一化 code 映射。
// 用小写包含匹配。更具体的特征放前面避免被宽匹配吞掉。
// 这里只覆盖生产中频繁出现、值得单独成桶的关键错误;通用 4xx/5xx 仍走状态码兜底。
var errorMessagePatterns = []struct {
	needle string
	code   string
}{
	// ===== Anthropic / Claude 协议错误 =====
	{"this model does not support assistant message prefill", "assistant_prefill_unsupported"},
	{"invalid `signature` in `thinking` block", "thinking_signature_invalid"},
	{"cache_control block must not come after", "cache_control_ttl_order_invalid"},
	{"strategy requires `thinking` to be enabled", "clear_thinking_requires_thinking"},
	{"this model does not support the effort parameter", "effort_parameter_unsupported"},
	{"previous_response_id is only supported", "previous_response_id_unsupported"},
	{"unable to download content from the provided url", "url_download_timeout"},

	// ===== Claude Code 客户端版本/账号策略 =====
	{"no available accounts: this group only allows claude code", "no_available_claude_code_account"},
	{"is below the minimum required version", "claude_code_version_outdated"},

	// ===== 速率 / 并发 =====
	{"too many pending requests", "pending_requests_overflow"},
	{"concurrency limit exceeded for account", "account_concurrency_limit"},
	{"would exceed your account's rate limit", "account_rate_limit"},
	{"upstream rate limit exceeded", "upstream_rate_limit"},

	// ===== 上游可用性 / 鉴权 =====
	{"upstream access forbidden", "upstream_access_forbidden"},
	{"upstream authentication failed", "upstream_auth_failed"},
	{"upstream service temporarily unavailable", "upstream_service_unavailable"},
	{"service temporarily unavailable", "service_unavailable"},
	{"upstream request failed", "upstream_request_failed"},

	// ===== 内容合规 =====
	{"内容审计命中风险规则", "content_moderation_violation"},
}

// MatchErrorCodeByMessage 在消息中按声明顺序匹配特征子串,命中则返回精细化 code。
// 没有命中返回空串,留给后续 rawCode/状态码兜底。
func MatchErrorCodeByMessage(msg string) string {
	if msg == "" {
		return ""
	}
	lower := strings.ToLower(msg)
	for _, p := range errorMessagePatterns {
		if strings.Contains(lower, p.needle) {
			return p.code
		}
	}
	return ""
}

// NormalizeErrorCode 归一化错误码。
// 优先级:
//  1. rawCode 命中归一化字典 — 显式重写
//  2. rawCode 是上游给的具体 code(非空且非 unknown_error / upstream_error 占位)— 原样保留
//  3. rawCode 是 unknown_error 占位或空 — 用消息特征精细分类(把以前都落进 unknown_error 的关键错误拆出来);
//     消息没命中任何特征时,保留 unknown_error 作为兜底桶,新出现的未知错误自动落这里
//  4. rawCode 完全为空时,走状态码兜底
func NormalizeErrorCode(rawCode string, statusCode int, errMessage string) string {
	rawCode = strings.TrimSpace(rawCode)

	if v, ok := errorCodeNormalize[rawCode]; ok {
		return v
	}
	if rawCode != "" && rawCode != "unknown_error" && rawCode != "upstream_error" {
		return rawCode
	}

	// 走到这里说明 rawCode 是 "" / unknown_error / upstream_error 占位 — 尝试用消息特征细分
	if code := MatchErrorCodeByMessage(errMessage); code != "" {
		return code
	}
	// 占位 rawCode 没命中特征 — 原样保留,unknown_error 是新错误的兜底桶
	if rawCode != "" {
		return rawCode
	}

	// rawCode 完全为空 — 状态码兜底
	switch {
	case statusCode == 429:
		return "rate_limit"
	case statusCode >= 500 && statusCode < 600:
		return "upstream_5xx"
	case statusCode == 401, statusCode == 403:
		return "upstream_auth_failed"
	case statusCode == 408:
		return "upstream_timeout"
	case statusCode == 402:
		return "upstream_quota_exhausted"
	case statusCode >= 400:
		return "invalid_request"
	}
	return "unknown"
}

// TruncateErrorMessage 截断错误消息到指定长度(默认 512,匹配 DB 列宽)。
func TruncateErrorMessage(msg string, max int) string {
	if max <= 0 {
		max = 512
	}
	msg = strings.TrimSpace(msg)
	if len(msg) <= max {
		return msg
	}
	return msg[:max]
}
