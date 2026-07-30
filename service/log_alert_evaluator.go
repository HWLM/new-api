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

	"gorm.io/gorm"
)

// 错误日志告警评估器：每 60s tick 一次，仅 master 节点跑。
// 单条规则语义：若 (now - LastScanAt) >= IntervalMinutes 分钟则扫描一次；
// 窗口 = [LastScanAt, now)。扫描完更新水位。首次扫描前 LastScanAt = 规则创建时间。

const (
	logAlertEvalInterval = 60 * time.Second
	// 单条规则的处理超时（每 rule 独立，避免慢查询拖垮其他 rule）
	logAlertRulePerRuleTimeout = 30 * time.Second
	// 流式扫描单批大小；桶只存聚合值，因此内存与批大小成正比、与总量无关
	logAlertBatchSize = 1000
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
	// 硬截止：evalOnce 总执行时间必须 < leader TTL，防止即使心跳失败也不会跨节点重叠。
	// 到点 ctx.Done() 会让 evalLogAlertRule 里的 FindInBatches 自然中止本 rule；
	// 未处理完的 rule 落到下一 tick 继续，冷却机制保证不会重复告警。
	onceCtx, cancelOnce := context.WithTimeout(parent, logAlertEvalOnceHardDeadline)
	defer cancelOnce()

	// 心跳：evalOnce 期间每 30s 续一次 TTL，避免慢查询把 TTL 拖爆。
	// 心跳失败 → 立即 cancel 本轮 evalOnce，让 rule 循环退出，本节点主动让位。
	heartbeatCtx, cancelHeartbeat := context.WithCancel(onceCtx)
	defer cancelHeartbeat()
	go func() {
		t := time.NewTicker(logAlertLeaderHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-t.C:
				if !renewLogAlertLeader() {
					common.SysError("log alert leader: renew returned false, cancel evalOnce to yield")
					cancelOnce()
					return
				}
			}
		}
	}()
	// evalOnce 结束后主动释放锁，正常路径下别的节点无需等 TTL 就能接手。
	defer releaseLogAlertLeader()

	// 用 parent 拉规则列表；每条规则的执行走独立 timeout，避免一条慢查询拖死其他规则
	listCtx, cancel := context.WithTimeout(onceCtx, 10*time.Second)
	rules, err := model.ListEnabledLogAlertRules(listCtx)
	cancel()
	if err != nil {
		common.SysError("log alert: load rules failed: " + err.Error())
		return
	}
	now := time.Now()
	for i := range rules {
		if onceCtx.Err() != nil {
			return
		}
		ruleCtx, ruleCancel := context.WithTimeout(onceCtx, logAlertRulePerRuleTimeout)
		evalLogAlertRule(ruleCtx, &rules[i], now)
		ruleCancel()
	}
}

func evalLogAlertRule(ctx context.Context, rule *model.LogAlertRule, now time.Time) {
	interval := rule.IntervalMinutes
	if interval <= 0 {
		interval = 1
	}
	nowUnix := now.Unix()

	// 每日活跃时段判定：默认 0~1439 = 全天；不活跃时段跳过并推水位，
	// 避免时段外累积的错误在"下一个活跃时段第一次 tick"被一次性推送。
	if !isLogAlertRuleActiveNow(rule, now) {
		_ = model.TouchLogAlertRuleLastScan(ctx, rule.Id, nowUnix)
		return
	}

	// 时钟回拨兜底：LastScanAt 若在未来（NTP 回拨、机器/DB 时间不同步），
	// 差值恒为负会永远小于阈值 → 规则再也不跑。强制把水位拉回最近一个 interval。
	if rule.LastScanAt > nowUnix {
		common.SysError(fmt.Sprintf("log alert rule=%d LastScanAt=%d > now=%d, clock skew? clamp to now-interval",
			rule.Id, rule.LastScanAt, nowUnix))
		rule.LastScanAt = nowUnix - int64(interval)*60
	}

	// 频率判定：距上次扫描不足 interval 分钟就跳过
	if rule.LastScanAt > 0 && nowUnix-rule.LastScanAt < int64(interval)*60 {
		return
	}
	windowFrom := rule.LastScanAt
	if windowFrom <= 0 {
		// 极端兜底：新规则的 LastScanAt 应由 controller 初始化为 CreatedAt，
		// 若历史数据出现 0，把窗口限制在最近一个 interval 内以避免扫全库。
		windowFrom = nowUnix - int64(interval)*60
	}
	windowTo := nowUnix

	q, hasScope := buildLogAlertBaseQuery(ctx, rule, windowFrom, windowTo)
	if !hasScope {
		// scope 目标为空 / 非法：本窗口没有可扫的记录，仍推进水位避免下次窗口无限扩大
		_ = model.TouchLogAlertRuleLastScan(ctx, rule.Id, windowTo)
		return
	}

	filters, filterOK := parseLogAlertFilters(rule.Filters)
	if !filterOK {
		// filters 存在但解析失败：保守不告警（返回 false 会让 matchLogAlertFilters 永远不命中），
		// 避免"损坏配置 → 全量告警风暴"。水位仍推进，下 tick 若配置修好即恢复。
		common.SysError(fmt.Sprintf("log alert rule=%d filters malformed, skip this tick", rule.Id))
		_ = model.TouchLogAlertRuleLastScan(ctx, rule.Id, windowTo)
		return
	}

	// 流式聚合：桶只存 O(1) 大小的聚合值，与日志条数无关，天然无内存上限
	aggs := map[string]*logAlertAgg{}
	order := make([]string, 0, 16)

	var batch []model.Log
	err := q.
		Select("id, user_id, username, token_id, token_name, channel_id, request_id, content").
		Order("id ASC"). // 升序：批内游标天然递增，SampleId 覆写等于保留最新
		FindInBatches(&batch, logAlertBatchSize, func(_ *gorm.DB, _ int) error {
			for i := range batch {
				lg := &batch[i]
				// 排除语义：命中任一 filter 行 = 用户想屏蔽的噪音，跳过不告警；
				// 只有"没命中任何 filter"的日志才计入告警桶。
				if matchLogAlertFilters(lg.Content, filters) {
					continue
				}
				sv, sl := scopeKeyFor(rule, lg)
				a, ok := aggs[sv]
				if !ok {
					a = &logAlertAgg{ScopeValue: sv, ScopeLabel: sl}
					aggs[sv] = a
					order = append(order, sv)
				}
				a.HitCount++
				a.SampleId = lg.Id // 升序扫描：最后写入的即窗口内最新一条
				a.SampleReq = lg.RequestId
			}
			return ctx.Err() // 超时/取消 → 中止本批循环
		}).Error

	if err != nil {
		common.SysError(fmt.Sprintf("log alert rule=%d scan failed: %s", rule.Id, err.Error()))
		// 不推水位：下 tick 从同一 windowFrom 重扫；重复告警由冷却机制拦住
		return
	}

	// 逐桶告警
	for _, sv := range order {
		a := aggs[sv]
		cdRaw := BuildLogAlertCooldownKey(rule.Id, rule.ScopeType, a.ScopeValue)
		if LogAlertCooldownExists(cdRaw) {
			continue
		}
		event := &model.LogAlertEvent{
			RuleId:        rule.Id,
			RuleName:      rule.Name,
			ScopeType:     rule.ScopeType,
			ScopeKey:      cdRaw,
			ScopeLabel:    a.ScopeLabel,
			HitCount:      a.HitCount,
			SampleLogId:   a.SampleId,
			SampleRequest: a.SampleReq,
			WindowFromAt:  windowFrom,
			WindowToAt:    windowTo,
			FiredAt:       nowUnix,
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

	// 全部批次跑完 → 推进水位
	if err := model.TouchLogAlertRuleLastScan(ctx, rule.Id, windowTo); err != nil {
		common.SysError(fmt.Sprintf("log alert rule=%d touch failed: %s", rule.Id, err.Error()))
	}
}

// logAlertAgg 每个 scope 桶的聚合值。桶不再 hold 原始 []Log，因此没有 5000 条硬上限。
type logAlertAgg struct {
	ScopeValue string
	ScopeLabel string
	HitCount   int
	SampleId   int
	SampleReq  string
}

// buildLogAlertBaseQuery 拼装 rule 对应的 WHERE 子句。
// 第二个返回值 hasScope=false 表示 scope 目标为空/非法，本 rule 本窗口无可扫记录。
func buildLogAlertBaseQuery(ctx context.Context, rule *model.LogAlertRule, from, to int64) (*gorm.DB, bool) {
	q := model.LOG_DB.WithContext(ctx).
		Model(&model.Log{}).
		Where("type = ?", model.LogTypeError).
		Where("created_at >= ? AND created_at < ?", from, to)

	switch rule.ScopeType {
	case "user":
		names := parseScopeUsernames(rule.ScopeValues)
		if len(names) == 0 {
			return nil, false
		}
		q = q.Where("username IN ?", names)
	case "token":
		ids := parseScopeTokenIds(rule.ScopeValues)
		if len(ids) == 0 {
			return nil, false
		}
		q = q.Where("token_id IN ?", ids)
	case "channel":
		ids := parseScopeChannelIds(rule.ScopeValues)
		if len(ids) == 0 {
			return nil, false
		}
		q = q.Where("channel_id IN ?", ids)
	case "all", "":
		// 全量
	default:
		return nil, false
	}
	return q, true
}

// isLogAlertRuleActiveNow 判定 now 是否落在规则的"每日活跃时段"内。
// 语义：minute-of-day ∈ [ActiveStartMinute, ActiveEndMinute]。
//   - 默认 0~1439 → 全天活跃
//   - 越界（历史脏数据 / 非法配置）→ 视为全天活跃，兼容旧规则
//   - start > end → 视为全天活跃（不支持跨天，避免语义歧义）
func isLogAlertRuleActiveNow(rule *model.LogAlertRule, now time.Time) bool {
	start, end := rule.ActiveStartMinute, rule.ActiveEndMinute
	if start < 0 || end < 0 || start > 1439 || end > 1439 || start > end {
		return true
	}
	m := now.Hour()*60 + now.Minute()
	return m >= start && m <= end
}

// scopeKeyFor 为一条日志计算所属桶的 (scopeValue, scopeLabel)。
func scopeKeyFor(rule *model.LogAlertRule, lg *model.Log) (string, string) {
	switch rule.ScopeType {
	case "user":
		v := fmt.Sprintf("%d", lg.UserId)
		if lg.Username != "" {
			return v, lg.Username
		}
		return v, v
	case "token":
		v := fmt.Sprintf("%d", lg.TokenId)
		if lg.TokenName != "" {
			return v, lg.TokenName
		}
		return v, v
	case "channel":
		v := fmt.Sprintf("%d", lg.ChannelId)
		return v, v
	default:
		return "all", "所有"
	}
}

// ============== 过滤 ==============

type logAlertFilterItem struct {
	Code     string   `json:"code"`
	Keywords []string `json:"keywords"`
}

// parseLogAlertFilters 返回 (filters, ok)：
//   - raw == "" → ({}, true)：无过滤器，全量命中，视为合法配置
//   - JSON 合法 → (cleaned, true)
//   - JSON 非法 → (nil, false)：调用方应保守跳过本 tick，不能当作"不过滤"
func parseLogAlertFilters(raw string) ([]logAlertFilterItem, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	var items []logAlertFilterItem
	if err := common.UnmarshalJsonStr(raw, &items); err != nil {
		return nil, false
	}
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
	return cleaned, true
}

// matchLogAlertFilters 返回 true 表示 content 命中了任一 filter 行 = 满足过滤条件。
// 调用侧的排除语义：命中 → 视为噪音、跳过不告警；未命中 → 计入告警桶。
//   - Filters 空 → 没有排除条件，全部日志正常参与告警 → 返回 false
//   - 行匹配 = (code 空 || content 含 "status_code={code},")
//     && (keywords 空 || 任一 kw 满足 strings.Contains(content, kw))
//   - 多行 OR：任一行命中即视为命中
func matchLogAlertFilters(content string, items []logAlertFilterItem) bool {
	if len(items) == 0 {
		return false
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

// ResolveLogAlertBaseURL 统一决策告警链接的 base：
//
//	优先级：env LOG_ALERT_BASE_URL > logAlertDefaultBaseURL
//
// 面板主域名（system_setting.ServerAddress）与告警链接域名往往需要拆开
// （例如 API 走 cnapi.xxx，面板走 xxx），所以不再回退到 ServerAddress，
// 直接落到本文件常量。dev/本地需要不同地址时用 env 覆盖。
//
// 导出后 controller 里 ack 跳转也走同一个 base，避免"告警链接指向 A，302 跳到 B"。
func ResolveLogAlertBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("LOG_ALERT_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return logAlertDefaultBaseURL
}

// BuildLogAlertPreviewLink 生成"测试推送"用的示例链接，指向系统通用日志页。
// 不带 ack token；仅用于展示消息结构。
func BuildLogAlertPreviewLink(rule *model.LogAlertRule) string {
	return fmt.Sprintf("%s/usage-logs/common?preview_rule=%d", ResolveLogAlertBaseURL(), rule.Id)
}

// buildLogAlertAckLink 生成匿名 ack 链接。
func buildLogAlertAckLink(event *model.LogAlertEvent) string {
	return fmt.Sprintf("%s/api/error-log-alerts/events/%d/ack?t=%s", ResolveLogAlertBaseURL(), event.Id, event.AckToken)
}

// randAckToken 生成 8 字节 hex（16 字符）。
// crypto/rand 失败时返回空串，让调用方感知并处理（不给可预测 fallback token）。
func randAckToken() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		common.SysError("log alert: crypto/rand failed, ack token empty: " + err.Error())
		return ""
	}
	return hex.EncodeToString(buf)
}

// LogAlertAckedCooldown 供 controller ack 端点复用的常量。
var LogAlertAckedCooldown = logAlertAckedCooldown
