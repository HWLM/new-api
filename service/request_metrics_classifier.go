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
// 归一化字典留空,后续按生产实际遇到的错误逐步补充。

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

// NormalizeErrorCode 归一化原始错误码:命中字典则取字典值,否则按状态码兜底。
func NormalizeErrorCode(rawCode string, statusCode int) string {
	rawCode = strings.TrimSpace(rawCode)
	if v, ok := errorCodeNormalize[rawCode]; ok {
		return v
	}
	if rawCode != "" {
		return rawCode
	}
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
