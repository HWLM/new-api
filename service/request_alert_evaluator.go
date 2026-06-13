package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// 告警评估器:每 60s 跑一次,对启用的规则查最近 N 分钟指标,命中持续达阈值则触发。
// 仅在 master 节点跑,避免多实例重复告警。
// 简化实现:不引入 Redis SETNX,依赖 common.IsMasterNode 标记。

const (
	alertEvalInterval = 60 * time.Second
)

type breachState struct {
	lastFiredAt time.Time // 最近一次触发时间(用于 cooldown)
	lastEventId int64     // 最近一次 firing 事件 id(用于 resolve)
}

var (
	alertBreaches   = make(map[int]*breachState)
	alertBreachesMu sync.Mutex
)

func StartRequestAlertEvaluator(ctx context.Context) {
	go runAlertEvaluator(ctx)
}

func runAlertEvaluator(ctx context.Context) {
	common.SysLog("request alert evaluator started")
	ticker := time.NewTicker(alertEvalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			common.SysLog("request alert evaluator stopped")
			return
		case <-ticker.C:
			if !common.IsMasterNode {
				continue
			}
			evalOnce(ctx)
		}
	}
}

func evalOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	rules, err := model.ListEnabledRequestAlertRules(ctx)
	if err != nil {
		common.SysError("alert evaluator: load rules failed: " + err.Error())
		return
	}
	now := time.Now()
	for i := range rules {
		evalRule(ctx, &rules[i], now)
	}
}

func evalRule(ctx context.Context, rule *model.RequestAlertRule, now time.Time) {
	value, err := computeRuleMetric(ctx, rule, now)
	if err != nil {
		common.SysError(fmt.Sprintf("alert eval rule=%d failed: %s", rule.Id, err.Error()))
		return
	}
	hit := compareMetric(value, rule.Operator, rule.Threshold)

	alertBreachesMu.Lock()
	st, ok := alertBreaches[rule.Id]
	if !ok {
		st = &breachState{}
		alertBreaches[rule.Id] = st
	}
	alertBreachesMu.Unlock()

	if !hit {
		// 不命中:如有 firing 事件则 resolve
		if st.lastEventId > 0 {
			_ = model.ResolveAlertEvent(ctx, st.lastEventId, now.Unix())
			st.lastEventId = 0
		}
		return
	}

	// 命中即触发(窗口已是最近 sustained_minutes 分钟的数据,持续语义体现在 SQL 窗口大小)
	// cooldown 检查 — 防止短时间反复发同一告警
	if !st.lastFiredAt.IsZero() && now.Sub(st.lastFiredAt) < time.Duration(rule.CooldownMinutes)*time.Minute {
		return
	}

	// 触发
	payload := map[string]any{
		"value":     value,
		"threshold": rule.Threshold,
		"operator":  rule.Operator,
		"metric":    rule.Metric,
	}
	payloadBytes, _ := json.Marshal(payload)
	ev := &model.RequestAlertEvent{
		RuleId:      rule.Id,
		FiredAt:     now.Unix(),
		MetricValue: value,
		Status:      "firing",
		Payload:     string(payloadBytes),
	}
	if err := model.CreateRequestAlertEvent(ctx, ev); err != nil {
		common.SysError("alert event insert failed: " + err.Error())
		return
	}
	st.lastFiredAt = now
	st.lastEventId = ev.Id

	if err := sendAlertNotification(rule, value, now); err != nil {
		common.SysError(fmt.Sprintf("alert tg send failed rule=%d: %s", rule.Id, err.Error()))
	}
}

// computeRuleMetric 根据 rule 查最近 sustained_minutes 分钟(最少 1 分钟)的指标。
func computeRuleMetric(ctx context.Context, rule *model.RequestAlertRule, now time.Time) (float64, error) {
	minutes := rule.SustainedMinutes
	if minutes <= 0 {
		minutes = 1
	}
	from := now.Add(-time.Duration(minutes) * time.Minute).Unix()
	to := now.Unix()

	platforms := parsePlatforms(rule.Platforms)
	th := setting.GetMetricsThresholds()

	conds := []string{"created_at >= ?", "created_at < ?"}
	args := []any{from, to}
	if len(platforms) > 0 {
		conds = append(conds, "channel_type IN ?")
		args = append(args, platforms)
	}
	where := strings.Join(conds, " AND ")

	var sqlSelect string
	switch rule.Metric {
	case "avg_duration_ms":
		sqlSelect = "COALESCE(AVG(duration_ms), 0)"
	case "error_rate":
		sqlSelect = "COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT / NULLIF(COUNT(*), 0), 0)"
	case "slow_resp_rate":
		sqlSelect = fmt.Sprintf("COALESCE(SUM(CASE WHEN duration_ms > %d THEN 1 ELSE 0 END)::FLOAT / NULLIF(COUNT(*), 0), 0)", th.SlowResponseMs)
	default:
		return 0, fmt.Errorf("unsupported metric: %s", rule.Metric)
	}

	sql := "SELECT " + sqlSelect + " AS value FROM request_metrics_logs WHERE " + where
	var result struct{ Value float64 }
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&result).Error; err != nil {
		return 0, err
	}
	return result.Value, nil
}

func compareMetric(value float64, op string, threshold float64) bool {
	switch op {
	case "gt":
		return value > threshold
	case "eq":
		return value == threshold
	case "ge", "gte":
		return value >= threshold
	}
	return false
}

func parsePlatforms(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var arr []int
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr
	}
	return nil
}

func sendAlertNotification(rule *model.RequestAlertRule, value float64, now time.Time) error {
	platformNames := []string{"全部"}
	if ids := parsePlatforms(rule.Platforms); len(ids) > 0 {
		platformNames = make([]string, 0, len(ids))
		for _, id := range ids {
			platformNames = append(platformNames, ChannelTypeName(id))
		}
	}
	metricLabel, unit := metricLabelAndUnit(rule.Metric)
	opLabel := operatorLabel(rule.Operator)
	thresholdDisplay := formatMetricValue(rule.Metric, rule.Threshold)
	valueDisplay := formatMetricValue(rule.Metric, value)

	// 文案严格对齐原型: 【平台】指标告警 + N分钟内指标操作符阈值单位,请及时处理
	_ = valueDisplay
	_ = now
	text := fmt.Sprintf(
		"【%s】%s告警\n%d分钟内%s%s%s%s,请及时处理",
		strings.Join(platformNames, ","), metricLabel,
		rule.SustainedMinutes, metricLabel, opLabel, thresholdDisplay, unit,
	)

	// TG 配置优先用全局,fallback 到规则上(向后兼容)
	cfg := setting.GetMetricsAlertTG()
	botToken := cfg.BotToken
	chatId := cfg.ChatId
	if botToken == "" {
		botToken = rule.TgBotToken
	}
	if chatId == "" {
		chatId = rule.TgChatId
	}
	return SendTelegramMessage(botToken, chatId, text)
}

func metricLabelAndUnit(metric string) (string, string) {
	switch metric {
	case "avg_duration_ms":
		return "平均响应时长", "ms"
	case "error_rate":
		return "错误率", "%"
	case "slow_resp_rate":
		return "慢请求率", "%"
	}
	return metric, ""
}

func operatorLabel(op string) string {
	switch op {
	case "gt":
		return "大于"
	case "eq":
		return "等于"
	case "ge", "gte":
		return "大于等于"
	}
	return op
}

func formatMetricValue(metric string, v float64) string {
	switch metric {
	case "error_rate", "slow_resp_rate":
		return fmt.Sprintf("%.2f", v*100)
	}
	return fmt.Sprintf("%.0f", v)
}
