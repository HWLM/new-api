package setting

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// 请求-响应统计设置:
//   metrics.slow_response_ms          int    默认 1500
//   metrics.slow_ttft_ms              int    默认 1500
//   metrics.business_error_keywords   text   换行分隔
//   metrics.log_retention_days        int    默认 3
//
// 本包只提供数据缓存和应用函数,避免 model→setting→service→model 的循环依赖。
// service 包通过 setting.GetMetricsXxx 读取;controller 通过 ApplyMetricsOption 写入。

const (
	OptKeyMetricsSlowResponseMs   = "metrics.slow_response_ms"
	OptKeyMetricsSlowTTFTMs       = "metrics.slow_ttft_ms"
	OptKeyMetricsBusinessKws      = "metrics.business_error_keywords"
	OptKeyMetricsRetentionDays    = "metrics.log_retention_days"
	OptKeyMetricsAlertTgBotToken  = "metrics.alert_tg_bot_token"
	OptKeyMetricsAlertTgChatId    = "metrics.alert_tg_chat_id"

	defaultSlowResponseMs = 1500
	defaultSlowTTFTMs     = 1500
	defaultRetentionDays  = 3
)

type MetricsThresholds struct {
	SlowResponseMs int
	SlowTTFTMs     int
}

type AlertTGConfig struct {
	BotToken string
	ChatId   string
}

var (
	metricsThresholdsPtr atomic.Pointer[MetricsThresholds]
	metricsKeywordsPtr   atomic.Pointer[[]string]
	metricsRetentionDays atomic.Int64
	metricsAlertTGPtr    atomic.Pointer[AlertTGConfig]
)

func init() {
	metricsThresholdsPtr.Store(&MetricsThresholds{
		SlowResponseMs: defaultSlowResponseMs,
		SlowTTFTMs:     defaultSlowTTFTMs,
	})
	empty := []string{}
	metricsKeywordsPtr.Store(&empty)
	metricsRetentionDays.Store(defaultRetentionDays)
	metricsAlertTGPtr.Store(&AlertTGConfig{})
}

func GetMetricsAlertTG() AlertTGConfig {
	if p := metricsAlertTGPtr.Load(); p != nil {
		return *p
	}
	return AlertTGConfig{}
}

func GetMetricsThresholds() MetricsThresholds {
	if p := metricsThresholdsPtr.Load(); p != nil {
		return *p
	}
	return MetricsThresholds{SlowResponseMs: defaultSlowResponseMs, SlowTTFTMs: defaultSlowTTFTMs}
}

func GetMetricsBusinessKeywords() []string {
	if p := metricsKeywordsPtr.Load(); p != nil {
		return *p
	}
	return nil
}

func GetMetricsRetentionDays() int {
	v := int(metricsRetentionDays.Load())
	if v <= 0 {
		return defaultRetentionDays
	}
	return v
}

// MetricsOptionKeys 返回所有 metrics 相关 option key 列表,供 main.go 启动时加载。
func MetricsOptionKeys() []string {
	return []string{
		OptKeyMetricsSlowResponseMs,
		OptKeyMetricsSlowTTFTMs,
		OptKeyMetricsBusinessKws,
		OptKeyMetricsRetentionDays,
		OptKeyMetricsAlertTgBotToken,
		OptKeyMetricsAlertTgChatId,
	}
}

// ApplyMetricsOption 由 main.go 启动加载、controller PUT 修改时调用。
// 返回 true 表示这个 key 由本模块处理。
func ApplyMetricsOption(key, value string) bool {
	switch key {
	case OptKeyMetricsSlowResponseMs:
		v, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || v <= 0 {
			v = defaultSlowResponseMs
		}
		old := GetMetricsThresholds()
		metricsThresholdsPtr.Store(&MetricsThresholds{
			SlowResponseMs: v,
			SlowTTFTMs:     old.SlowTTFTMs,
		})
		return true
	case OptKeyMetricsSlowTTFTMs:
		v, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || v <= 0 {
			v = defaultSlowTTFTMs
		}
		old := GetMetricsThresholds()
		metricsThresholdsPtr.Store(&MetricsThresholds{
			SlowResponseMs: old.SlowResponseMs,
			SlowTTFTMs:     v,
		})
		return true
	case OptKeyMetricsBusinessKws:
		var keywords []string
		for _, line := range strings.Split(value, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				keywords = append(keywords, line)
			}
		}
		metricsKeywordsPtr.Store(&keywords)
		return true
	case OptKeyMetricsRetentionDays:
		v, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || v <= 0 {
			v = defaultRetentionDays
		}
		metricsRetentionDays.Store(int64(v))
		return true
	case OptKeyMetricsAlertTgBotToken:
		old := GetMetricsAlertTG()
		metricsAlertTGPtr.Store(&AlertTGConfig{
			BotToken: strings.TrimSpace(value),
			ChatId:   old.ChatId,
		})
		return true
	case OptKeyMetricsAlertTgChatId:
		old := GetMetricsAlertTG()
		metricsAlertTGPtr.Store(&AlertTGConfig{
			BotToken: old.BotToken,
			ChatId:   strings.TrimSpace(value),
		})
		return true
	}
	return false
}

func IsMetricsOptionKey(key string) bool {
	switch key {
	case OptKeyMetricsSlowResponseMs, OptKeyMetricsSlowTTFTMs, OptKeyMetricsBusinessKws,
		OptKeyMetricsRetentionDays, OptKeyMetricsAlertTgBotToken, OptKeyMetricsAlertTgChatId:
		return true
	}
	return false
}
