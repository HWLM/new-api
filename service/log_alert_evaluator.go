package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// 错误日志告警评估器：每 60s tick 一次，仅 master 节点跑。
// 单条规则语义：若 (now - LastScanAt) >= IntervalMinutes 分钟则扫描一次；
// 窗口 = [LastScanAt, now)。扫描完更新水位。首次扫描前 LastScanAt = 规则创建时间。

const (
	logAlertEvalInterval = 60 * time.Second
	// 单次扫描日志数硬上限，防某个规则长时间未跑窗口过大爆内存
	logAlertMaxScanRows = 5000
	// 用户点 ack 后的静默时长
	logAlertAckedCooldown = 15 * time.Minute
	// 未配 env 时告警链接默认前缀
	logAlertDefaultBaseURL = "https://aiyunrouter.com"
)

func StartLogAlertEvaluator(ctx context.Context) {
	go runLogAlertEvaluator(ctx)
}

func runLogAlertEvaluator(ctx context.Context) {
	common.SysLog("log alert evaluator started")
	ticker := time.NewTicker(logAlertEvalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			common.SysLog("log alert evaluator stopped")
			return
		case <-ticker.C:
			if !common.IsMasterNode {
				continue
			}
			if !tryAcquireLogAlertLeader() {
				continue
			}
			evalLogAlertsOnce(ctx)
		}
	}
}

func evalLogAlertsOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()

	rules, err := model.ListEnabledLogAlertRules(ctx)
	if err != nil {
		common.SysError("log alert: load rules failed: " + err.Error())
		return
	}
	now := time.Now()
	for i := range rules {
		evalLogAlertRule(ctx, &rules[i], now)
	}
}

func evalLogAlertRule(ctx context.Context, rule *model.LogAlertRule, now time.Time) {
	interval := rule.IntervalMinutes
	if interval <= 0 {
		interval = 1
	}
	// 频率判定：距上次扫描不足 interval 分钟就跳过
	if rule.LastScanAt > 0 && now.Unix()-rule.LastScanAt < int64(interval*60) {
		return
	}
	windowFrom := rule.LastScanAt
	if windowFrom <= 0 {
		// 极端兜底：新规则的 LastScanAt 应由 controller 初始化为 CreatedAt，
		// 若历史数据出现 0，把窗口限制在最近一个 interval 内以避免扫全库。
		windowFrom = now.Unix() - int64(interval*60)
	}
	windowTo := now.Unix()

	logs, truncated, err := fetchErrorLogsForRule(ctx, rule, windowFrom, windowTo)
	if err != nil {
		common.SysError(fmt.Sprintf("log alert rule=%d fetch failed: %s", rule.Id, err.Error()))
		return
	}
	if truncated {
		common.SysError(fmt.Sprintf("log alert rule=%d hit row limit %d, window may be lagging", rule.Id, logAlertMaxScanRows))
	}

	// 过滤 + 分桶
	filters := parseLogAlertFilters(rule.Filters)
	buckets := bucketLogsForRule(rule, logs, filters)

	// 逐桶告警
	for _, b := range buckets {
		if len(b.Logs) == 0 {
			continue
		}
		cdRaw := BuildLogAlertCooldownKey(rule.Id, rule.ScopeType, b.ScopeValue)
		if LogAlertCooldownExists(cdRaw) {
			continue
		}
		sample := b.Logs[0] // 已按 id 降序，最新一条
		event := &model.LogAlertEvent{
			RuleId:        rule.Id,
			RuleName:      rule.Name,
			ScopeType:     rule.ScopeType,
			ScopeKey:      cdRaw,
			ScopeLabel:    b.ScopeLabel,
			HitCount:      len(b.Logs),
			SampleLogId:   sample.Id,
			SampleRequest: sample.RequestId,
			WindowFromAt:  windowFrom,
			WindowToAt:    windowTo,
			FiredAt:       now.Unix(),
			AckToken:      randAckToken(),
		}
		if err := model.CreateLogAlertEvent(ctx, event); err != nil {
			common.SysError(fmt.Sprintf("log alert rule=%d create event failed: %s", rule.Id, err.Error()))
			continue
		}
		if err := sendLogAlertNotify(rule, event, interval); err != nil {
			common.SysError(fmt.Sprintf("log alert rule=%d send failed: %s", rule.Id, err.Error()))
			// 发送失败也标记冷却，避免风暴；用户可去日志排查
		}
		_ = LogAlertCooldownMark(cdRaw, time.Duration(interval)*time.Minute)
	}

	// 更新水位（无论有没有告警，都要推进）
	if err := model.TouchLogAlertRuleLastScan(ctx, rule.Id, now.Unix()); err != nil {
		common.SysError(fmt.Sprintf("log alert rule=%d touch failed: %s", rule.Id, err.Error()))
	}
}

// fetchErrorLogsForRule 走 LOG_DB 拉取 type=5 且落在时间窗内、可选按 scope 粗筛的日志。
// 只 SELECT 告警需要的最小字段集。返回结果按 id DESC 排序（便于取"最新一条"作为跳转样本）。
func fetchErrorLogsForRule(ctx context.Context, rule *model.LogAlertRule, from, to int64) ([]model.Log, bool, error) {
	q := model.LOG_DB.WithContext(ctx).
		Model(&model.Log{}).
		Select("id, user_id, username, token_id, token_name, channel_id, request_id, content, created_at").
		Where("type = ?", model.LogTypeError).
		Where("created_at >= ? AND created_at < ?", from, to)

	switch rule.ScopeType {
	case "user":
		names := parseScopeUsernames(rule.ScopeValues)
		if len(names) == 0 {
			return nil, false, nil
		}
		q = q.Where("username IN ?", names)
	case "token":
		ids := parseScopeTokenIds(rule.ScopeValues)
		if len(ids) == 0 {
			return nil, false, nil
		}
		q = q.Where("token_id IN ?", ids)
	case "channel":
		ids := parseScopeChannelIds(rule.ScopeValues)
		if len(ids) == 0 {
			return nil, false, nil
		}
		q = q.Where("channel_id IN ?", ids)
	case "all", "":
		// 全量
	default:
		return nil, false, fmt.Errorf("unsupported scope_type: %s", rule.ScopeType)
	}

	var logs []model.Log
	if err := q.Order("id DESC").Limit(logAlertMaxScanRows + 1).Find(&logs).Error; err != nil {
		return nil, false, err
	}
	truncated := false
	if len(logs) > logAlertMaxScanRows {
		logs = logs[:logAlertMaxScanRows]
		truncated = true
	}
	return logs, truncated, nil
}

// bucketLogsForRule 应用过滤条件，按 scope 分桶。
type logAlertBucket struct {
	ScopeValue string      // "all" / "42" / channel id
	ScopeLabel string      // 展示用（用户名 / 密钥名 / 渠道 id）
	Logs       []model.Log // id DESC
}

func bucketLogsForRule(rule *model.LogAlertRule, logs []model.Log, filters []logAlertFilterItem) []*logAlertBucket {
	byKey := make(map[string]*logAlertBucket)
	order := []string{}
	for _, lg := range logs {
		if !matchLogAlertFilters(lg.Content, filters) {
			continue
		}
		var scopeValue, scopeLabel string
		switch rule.ScopeType {
		case "user":
			scopeValue = fmt.Sprintf("%d", lg.UserId)
			scopeLabel = lg.Username
			if scopeLabel == "" {
				scopeLabel = scopeValue
			}
		case "token":
			scopeValue = fmt.Sprintf("%d", lg.TokenId)
			scopeLabel = lg.TokenName
			if scopeLabel == "" {
				scopeLabel = scopeValue
			}
		case "channel":
			scopeValue = fmt.Sprintf("%d", lg.ChannelId)
			scopeLabel = scopeValue
		default:
			scopeValue = "all"
			scopeLabel = "所有"
		}
		b, ok := byKey[scopeValue]
		if !ok {
			b = &logAlertBucket{ScopeValue: scopeValue, ScopeLabel: scopeLabel}
			byKey[scopeValue] = b
			order = append(order, scopeValue)
		}
		b.Logs = append(b.Logs, lg)
	}
	out := make([]*logAlertBucket, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

// ============== 过滤 ==============

type logAlertFilterItem struct {
	Code     string   `json:"code"`
	Keywords []string `json:"keywords"`
}

func parseLogAlertFilters(raw string) []logAlertFilterItem {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var items []logAlertFilterItem
	if err := common.UnmarshalJsonStr(raw, &items); err != nil {
		return nil
	}
	// 清理空白
	cleaned := make([]logAlertFilterItem, 0, len(items))
	for _, it := range items {
		it.Code = strings.TrimSpace(it.Code)
		kws := make([]string, 0, len(it.Keywords))
		for _, k := range it.Keywords {
			k = strings.TrimSpace(k)
			if k != "" {
				kws = append(kws, k)
			}
		}
		it.Keywords = kws
		if it.Code == "" && len(it.Keywords) == 0 {
			continue
		}
		cleaned = append(cleaned, it)
	}
	return cleaned
}

// matchLogAlertFilters 语义：
//   - Filters 空 → 全部命中（不过滤）
//   - 行匹配 = (code 空 || content 含 "status_code={code},")
//     && (keywords 空 || 任一 kw 满足 strings.Contains(content, kw))
//   - 多行 OR
func matchLogAlertFilters(content string, items []logAlertFilterItem) bool {
	if len(items) == 0 {
		return true
	}
	for _, it := range items {
		codeMatched := it.Code == "" || strings.Contains(content, "status_code="+it.Code+",")
		if !codeMatched {
			continue
		}
		kwMatched := len(it.Keywords) == 0
		if !kwMatched {
			for _, kw := range it.Keywords {
				if strings.Contains(content, kw) {
					kwMatched = true
					break
				}
			}
		}
		if kwMatched {
			return true
		}
	}
	return false
}

// ============== ScopeValues 解析 ==============

func parseScopeUsernames(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var arr []string
	if err := common.UnmarshalJsonStr(raw, &arr); err != nil {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, s := range arr {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseScopeChannelIds(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var arr []int
	if err := common.UnmarshalJsonStr(raw, &arr); err == nil {
		return arr
	}
	// 兼容 ["1","2"] 字符串形式
	var strArr []string
	if err := common.UnmarshalJsonStr(raw, &strArr); err == nil {
		out := make([]int, 0, len(strArr))
		for _, s := range strArr {
			var v int
			if _, e := fmt.Sscanf(s, "%d", &v); e == nil {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}

// parseScopeTokenIds 支持两种载荷：
//
//	1. 扁平数组 [1,2,3]
//	2. 前端级联 [{user_id: <int>, token_ids: [<int>...]}] → 展平后返回
func parseScopeTokenIds(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// 先试扁平
	var flat []int
	if err := common.UnmarshalJsonStr(raw, &flat); err == nil {
		return flat
	}
	// 再试对象数组
	var groups []struct {
		UserId   int   `json:"user_id"`
		TokenIds []int `json:"token_ids"`
	}
	if err := common.UnmarshalJsonStr(raw, &groups); err == nil {
		out := []int{}
		for _, g := range groups {
			out = append(out, g.TokenIds...)
		}
		return out
	}
	return nil
}

// ============== 告警发送 ==============

// sendLogAlertNotify 组装文案并推送到企微群机器人。
func sendLogAlertNotify(rule *model.LogAlertRule, event *model.LogAlertEvent, interval int) error {
	content := FormatLogAlertMarkdown(rule.Name, rule.ScopeType, event.ScopeLabel, event.HitCount, interval, buildLogAlertAckLink(event), false)
	return SendWeComMarkdown(rule.WebhookUrl, content)
}

// FormatLogAlertMarkdown 生成企微群机器人 markdown 文案，供正式告警和测试推送共用。
//
// 文案模板：
//
//	监控对象=all：
//	    **【错误告警】**
//	    监控规则：{ruleName}   ← ruleName 非空时展示
//	    监控对象：所有
//	    X 分钟内出现 N 条报错信息
//
//	监控对象=user/token/channel（附 ack 链接）：
//	    **【错误告警】**
//	    监控规则：{ruleName}
//	    监控对象：{密钥|用户|渠道}（{scope_label}）
//	    X 分钟内出现 N 条报错信息
//	    链接：{server}/api/error-log-alerts/events/{id}/ack?t={token}
//
// isTest=true 时：标题追加"（测试）"，正文替换为固定测试提示；scope_label 与 ackLink 仍按类型展示。
func FormatLogAlertMarkdown(ruleName, scopeType, scopeLabel string, hitCount, interval int, ackLink string, isTest bool) string {
	title := "**【错误告警】**"
	if isTest {
		title = "**【错误告警】（测试）**"
	}

	var scopeLine string
	scoped := true
	switch scopeType {
	case "user":
		if strings.TrimSpace(scopeLabel) != "" {
			scopeLine = fmt.Sprintf("监控对象：用户（%s）", scopeLabel)
		} else {
			scopeLine = "监控对象：用户"
		}
	case "token":
		if strings.TrimSpace(scopeLabel) != "" {
			scopeLine = fmt.Sprintf("监控对象：密钥（%s）", scopeLabel)
		} else {
			scopeLine = "监控对象：密钥"
		}
	case "channel":
		if strings.TrimSpace(scopeLabel) != "" {
			scopeLine = fmt.Sprintf("监控对象：渠道（%s）", scopeLabel)
		} else {
			scopeLine = "监控对象：渠道"
		}
	default:
		scopeLine = "监控对象：所有"
		scoped = false
	}

	var body string
	if isTest {
		body = "这是一条测试消息，用于验证 webhook 配置正确。"
	} else {
		body = fmt.Sprintf("%d 分钟内出现 %d 条报错信息", interval, hitCount)
	}

	lines := []string{title}
	if n := strings.TrimSpace(ruleName); n != "" {
		lines = append(lines, "监控规则："+n)
	}
	lines = append(lines, scopeLine, body)
	if scoped && strings.TrimSpace(ackLink) != "" {
		// 企微机器人 markdown：纯文本 URL 不会被识别，必须走 [text](url)。
		// 用户希望消息里明文展示完整链接又能点击，因此显示文字与 url 保持一致。
		lines = append(lines, fmt.Sprintf("链接：[%s](%s)", ackLink, ackLink))
	}
	return strings.Join(lines, "\n")
}

// FirstScopeLabelForRule 从规则的 ScopeValues 里提取第一个用于展示的 label。
// 供测试推送使用（展示"用户/密钥/渠道（xxx）"里的 xxx）。
// - user   → 第一个 username
// - channel → 第一个 channel id（字符串形式）
// - token  → 优先取第一个 token 的名称（GetTokenById 查库），失败退回 "#{id}"
func FirstScopeLabelForRule(rule *model.LogAlertRule) string {
	switch rule.ScopeType {
	case "user":
		if names := parseScopeUsernames(rule.ScopeValues); len(names) > 0 {
			return names[0]
		}
	case "channel":
		if ids := parseScopeChannelIds(rule.ScopeValues); len(ids) > 0 {
			return fmt.Sprintf("%d", ids[0])
		}
	case "token":
		if ids := parseScopeTokenIds(rule.ScopeValues); len(ids) > 0 {
			if tk, err := model.GetTokenById(ids[0]); err == nil && tk != nil && strings.TrimSpace(tk.Name) != "" {
				return tk.Name
			}
			return fmt.Sprintf("#%d", ids[0])
		}
	}
	return ""
}

// resolveLogAlertBaseURL 统一决策告警链接的 base：
//
//	优先级：env LOG_ALERT_BASE_URL > logAlertDefaultBaseURL
//
// 面板主域名（system_setting.ServerAddress）与告警链接域名往往需要拆开
// （例如 API 走 cnapi.xxx，面板走 xxx），所以不再回退到 ServerAddress，
// 直接落到本文件常量。dev/本地需要不同地址时用 env 覆盖。
func resolveLogAlertBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("LOG_ALERT_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return logAlertDefaultBaseURL
}

// BuildLogAlertPreviewLink 生成"测试推送"用的示例链接，指向系统通用日志页。
// 不带 ack token；仅用于展示消息结构。
func BuildLogAlertPreviewLink(rule *model.LogAlertRule) string {
	return fmt.Sprintf("%s/usage-logs/common?preview_rule=%d", resolveLogAlertBaseURL(), rule.Id)
}

// buildLogAlertAckLink 生成匿名 ack 链接。
func buildLogAlertAckLink(event *model.LogAlertEvent) string {
	return fmt.Sprintf("%s/api/error-log-alerts/events/%d/ack?t=%s", resolveLogAlertBaseURL(), event.Id, event.AckToken)
}

// randAckToken 生成 8 字节 hex（16 字符）。
func randAckToken() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// 极端兜底：用时间戳
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// LogAlertAckedCooldown 供 controller ack 端点复用的常量。
var LogAlertAckedCooldown = logAlertAckedCooldown
