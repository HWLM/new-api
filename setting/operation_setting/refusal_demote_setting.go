package operation_setting

import (
	"github.com/QuantumNous/new-api/setting/config"
)

// RefusalDemoteSetting 控制「拒答比超限 → 临时降级到兜底渠道」功能。
//
// 判定口径：仅 Claude stop_reason=refusal 的请求计入分子，参见
// relay/channel/claude/relay-claude.go:maybeMarkClaudeRefusal。
// 分母是用户最近 WindowSize 次「文本类」relay 请求
// （text/claude/gemini/responses/responses_compaction）。
//
// 判定路径：
//  1. 请求结束后在 service.PostTextConsumeQuota 的异步钩子里更新滑窗；
//  2. 若窗口样本数 >= MinSamples 且拒答比 >= ThresholdPct/100，
//     写一个 TTL=DemoteSeconds 的降级标记；
//  3. 下一次请求进入 middleware.UserConcurrencyLimit 时检查标记，
//     命中则置 ContextKeyUserOverloaded=true，复用 Distribute 中间件
//     已有的 FallbackChannelId 重导逻辑。
type RefusalDemoteSetting struct {
	// Enabled 全局总开关；关闭时所有钩子短路返回。
	Enabled bool `json:"enabled"`
	// WindowSize 滑窗容量，统计用户最近 N 次文本类请求。
	WindowSize int `json:"window_size"`
	// MinSamples 样本量门槛，窗口未填满到该值不触发判定，避免冷启动误伤。
	MinSamples int `json:"min_samples"`
	// ThresholdPct 拒答比阈值，取值 0-100；窗口内拒答比 >= 该值触发降级。
	ThresholdPct int `json:"threshold_pct"`
	// DemoteSeconds 单次降级的持续秒数；默认 3600（1 小时）。
	DemoteSeconds int `json:"demote_seconds"`
	// WindowTTLSeconds 滑窗 Redis LIST 的保底 TTL，防长期不活跃用户占内存。
	WindowTTLSeconds int `json:"window_ttl_seconds"`
}

var refusalDemoteSetting = RefusalDemoteSetting{
	Enabled:          false,
	WindowSize:       100,
	MinSamples:       20,
	ThresholdPct:     30,
	DemoteSeconds:    3600,
	WindowTTLSeconds: 86400,
}

func init() {
	config.GlobalConfig.Register("refusal_demote_setting", &refusalDemoteSetting)
}

// GetRefusalDemoteSetting 返回当前全局配置副本指针；上层只读。
func GetRefusalDemoteSetting() *RefusalDemoteSetting {
	return &refusalDemoteSetting
}
