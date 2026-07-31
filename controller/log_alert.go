package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// 错误日志告警：规则 CRUD + 测试推送 + 匿名 ack 端点。

const (
	// 规则的合法数值边界：
	//   - 频率：1 分钟 ~ 1 天。上限防止用户传 math.MaxInt 导致 "永不扫描" / "首扫扫全库"。
	//   - 单个 scope 目标数：防止 WHERE ... IN (...) 拼出超长语句 / 命中 max_allowed_packet。
	logAlertMinIntervalMinutes = 1
	logAlertMaxIntervalMinutes = 24 * 60
	logAlertMaxScopeValues     = 200
	logAlertMaxRuleNameLen     = 128
)

type logAlertRuleReq struct {
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"interval_minutes"`
	WebhookUrl      string `json:"webhook_url"`
	Filters         string `json:"filters"`      // 原样 JSON 字符串
	ScopeType       string `json:"scope_type"`   // all|user|token|channel
	ScopeValues     string `json:"scope_values"` // 原样 JSON 字符串
	// 每日活跃时段用指针区分"未传"和"显式零值"：
	//   老客户端不传字段 → nil → 兜底全天 (0, 1439)
	//   新客户端传 (0, 0)  → 单点时段（合法配置，不再被静默扩成全天）
	PlatformConfigs   []model.LogAlertPlatformConfig `json:"platform_configs"`
	ActiveStartMinute *int                           `json:"active_start_minute"`
	ActiveEndMinute   *int                           `json:"active_end_minute"`
}

func normalizeScopeType(t string) string {
	t = strings.TrimSpace(strings.ToLower(t))
	switch t {
	case "user", "token", "channel", "all":
		return t
	case "":
		return "all"
	}
	return ""
}

func validateLogAlertReq(req *logAlertRuleReq) error {
	if len(req.PlatformConfigs) == 0 && strings.TrimSpace(req.WebhookUrl) != "" {
		req.PlatformConfigs = []model.LogAlertPlatformConfig{{
			Type:       model.LogAlertPlatformWeComGroup,
			WebhookURL: req.WebhookUrl,
		}}
	}
	if len(req.PlatformConfigs) == 0 {
		return fmt.Errorf("at least one platform configuration is required")
	}
	if len(req.PlatformConfigs) > 20 {
		return fmt.Errorf("too many platform configurations (max 20)")
	}
	for i := range req.PlatformConfigs {
		platform := &req.PlatformConfigs[i]
		platform.Type = strings.TrimSpace(strings.ToLower(platform.Type))
		platform.WebhookURL = strings.TrimSpace(platform.WebhookURL)
		platform.ChatID = strings.TrimSpace(platform.ChatID)
		platform.BotToken = strings.TrimSpace(platform.BotToken)
		switch platform.Type {
		case model.LogAlertPlatformWeComGroup:
			if platform.WebhookURL == "" {
				return fmt.Errorf("platform_configs[%d]: webhook_url required", i)
			}
		case model.LogAlertPlatformTelegram:
			if platform.ChatID == "" || platform.BotToken == "" {
				return fmt.Errorf("platform_configs[%d]: chat_id and bot_token required", i)
			}
		default:
			return fmt.Errorf("platform_configs[%d]: unsupported type", i)
		}
	}
	if len([]rune(req.Name)) > logAlertMaxRuleNameLen {
		return fmt.Errorf("name too long (max %d characters)", logAlertMaxRuleNameLen)
	}
	if req.IntervalMinutes < logAlertMinIntervalMinutes || req.IntervalMinutes > logAlertMaxIntervalMinutes {
		return fmt.Errorf("interval_minutes must be in [%d, %d]", logAlertMinIntervalMinutes, logAlertMaxIntervalMinutes)
	}
	scope := normalizeScopeType(req.ScopeType)
	if scope == "" {
		return fmt.Errorf("invalid scope_type")
	}
	req.ScopeType = scope

	if err := validateLogAlertFilters(req.Filters); err != nil {
		return err
	}
	if err := validateLogAlertScopeValues(scope, req.ScopeValues); err != nil {
		return err
	}
	// 每日活跃时段：指针字段区分"未传"和"显式零值"。
	//   老前端/老接口不传 → nil → 兜底填全天 (0, 1439)
	//   新前端显式 (0, 0)  → 单点时段，合法
	// 规范化到非 nil 后走统一的 [0, 1439] 与 start<=end 校验，方便下游 deref。
	if req.ActiveStartMinute == nil && req.ActiveEndMinute == nil {
		zero, end := 0, 1439
		req.ActiveStartMinute, req.ActiveEndMinute = &zero, &end
	} else if req.ActiveStartMinute == nil {
		zero := 0
		req.ActiveStartMinute = &zero
	} else if req.ActiveEndMinute == nil {
		end := 1439
		req.ActiveEndMinute = &end
	}
	if *req.ActiveStartMinute < 0 || *req.ActiveStartMinute > 1439 ||
		*req.ActiveEndMinute < 0 || *req.ActiveEndMinute > 1439 {
		return fmt.Errorf("active_start_minute / active_end_minute must be in [0, 1439]")
	}
	if *req.ActiveStartMinute > *req.ActiveEndMinute {
		return fmt.Errorf("active_start_minute must be <= active_end_minute")
	}
	return nil
}

func legacyWebhookURL(platforms []model.LogAlertPlatformConfig) string {
	for _, platform := range platforms {
		if platform.Type == model.LogAlertPlatformWeComGroup {
			return platform.WebhookURL
		}
	}
	return ""
}

// validateLogAlertFilters 提前把 filters 的 JSON 结构走一遍，防止 DB 里存进非法 JSON：
// evaluator 一旦解析失败会"保守跳过本 tick"，规则等于永远不告警——用户察觉不到。
// 空字符串合法（等价于"不过滤"）。
func validateLogAlertFilters(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var items []struct {
		Code     string   `json:"code"`
		Keywords []string `json:"keywords"`
	}
	if err := common.UnmarshalJsonStr(raw, &items); err != nil {
		return fmt.Errorf("filters must be a JSON array of {code, keywords[]}: %s", err.Error())
	}
	return nil
}

// validateLogAlertScopeValues 校验 scope_values 与 scope_type 组合合法：
//   - all    → 必须为空
//   - user   → JSON 字符串数组，非空
//   - channel→ JSON 数字数组（或字符串数字），正数、非空
//   - token  → 扁平数字数组 或 [{user_id, token_ids[]}]，展平后非空
//
// 同时限制单个 scope 目标数不超过 logAlertMaxScopeValues。
func validateLogAlertScopeValues(scope, raw string) error {
	raw = strings.TrimSpace(raw)
	switch scope {
	case "all":
		if raw != "" && raw != "[]" && raw != "null" {
			return fmt.Errorf("scope_values must be empty when scope_type=all")
		}
		return nil
	case "user":
		var arr []string
		if err := common.UnmarshalJsonStr(raw, &arr); err != nil {
			return fmt.Errorf("scope_values for user must be a JSON array of usernames: %s", err.Error())
		}
		clean := 0
		for _, s := range arr {
			if strings.TrimSpace(s) != "" {
				clean++
			}
		}
		if clean == 0 {
			return fmt.Errorf("scope_values must not be empty when scope_type=user")
		}
		if clean > logAlertMaxScopeValues {
			return fmt.Errorf("scope_values too many (max %d)", logAlertMaxScopeValues)
		}
		return nil
	case "channel":
		var ids []int
		if err := common.UnmarshalJsonStr(raw, &ids); err != nil {
			var strArr []string
			if e := common.UnmarshalJsonStr(raw, &strArr); e != nil {
				return fmt.Errorf("scope_values for channel must be a JSON array of channel ids: %s", err.Error())
			}
			ids = ids[:0]
			for _, s := range strArr {
				var v int
				if _, e := fmt.Sscanf(strings.TrimSpace(s), "%d", &v); e == nil {
					ids = append(ids, v)
				}
			}
		}
		clean := 0
		for _, id := range ids {
			if id > 0 {
				clean++
			}
		}
		if clean == 0 {
			return fmt.Errorf("scope_values must contain at least one positive channel id")
		}
		if clean > logAlertMaxScopeValues {
			return fmt.Errorf("scope_values too many (max %d)", logAlertMaxScopeValues)
		}
		return nil
	case "token":
		// 先试扁平
		var flat []int
		if err := common.UnmarshalJsonStr(raw, &flat); err == nil {
			total := 0
			for _, id := range flat {
				if id > 0 {
					total++
				}
			}
			if total == 0 {
				return fmt.Errorf("scope_values must contain at least one positive token id")
			}
			if total > logAlertMaxScopeValues {
				return fmt.Errorf("scope_values too many (max %d)", logAlertMaxScopeValues)
			}
			return nil
		}
		// 再试对象数组
		var groups []struct {
			UserId   int   `json:"user_id"`
			TokenIds []int `json:"token_ids"`
		}
		if err := common.UnmarshalJsonStr(raw, &groups); err != nil {
			return fmt.Errorf("scope_values for token must be a flat id array or [{user_id, token_ids[]}]: %s", err.Error())
		}
		total := 0
		for _, g := range groups {
			for _, id := range g.TokenIds {
				if id > 0 {
					total++
				}
			}
		}
		if total == 0 {
			return fmt.Errorf("scope_values must contain at least one positive token id")
		}
		if total > logAlertMaxScopeValues {
			return fmt.Errorf("scope_values too many (max %d)", logAlertMaxScopeValues)
		}
		return nil
	}
	return fmt.Errorf("unsupported scope_type: %s", scope)
}

func ListLogAlertRules(c *gin.Context) {
	rules, err := model.ListLogAlertRules(c.Request.Context())
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rules)
}

func GetLogAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	rule, err := model.GetLogAlertRule(c.Request.Context(), id)
	if err != nil {
		responseErr(c, http.StatusNotFound, err.Error())
		return
	}
	responseOK(c, rule)
}

func CreateLogAlertRule(c *gin.Context) {
	var req logAlertRuleReq
	if err := c.BindJSON(&req); err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateLogAlertReq(&req); err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	platformConfigs, err := common.Marshal(req.PlatformConfigs)
	if err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().Unix()
	rule := &model.LogAlertRule{
		Name:              req.Name,
		Enabled:           true, // 规则默认启用，前端不再提供开关
		IntervalMinutes:   req.IntervalMinutes,
		WebhookUrl:        legacyWebhookURL(req.PlatformConfigs),
		PlatformConfigs:   string(platformConfigs),
		Platforms:         req.PlatformConfigs,
		Filters:           req.Filters,
		ScopeType:         req.ScopeType,
		ScopeValues:       req.ScopeValues,
		ActiveStartMinute: *req.ActiveStartMinute,
		ActiveEndMinute:   *req.ActiveEndMinute,
		LastScanAt:        now, // 首次扫描窗口 ~ IntervalMinutes 分钟，不会扫全库历史
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := model.CreateLogAlertRule(c.Request.Context(), rule); err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rule)
}

func UpdateLogAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	rule, err := model.GetLogAlertRule(c.Request.Context(), id)
	if err != nil {
		responseErr(c, http.StatusNotFound, err.Error())
		return
	}
	var req logAlertRuleReq
	if err := c.BindJSON(&req); err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateLogAlertReq(&req); err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	platformConfigs, err := common.Marshal(req.PlatformConfigs)
	if err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	rule.Name = req.Name
	// 保持原 Enabled 状态：启用/禁用只通过 /toggle 接口修改，避免编辑规则时被顺带覆盖
	rule.IntervalMinutes = req.IntervalMinutes
	rule.WebhookUrl = legacyWebhookURL(req.PlatformConfigs)
	rule.PlatformConfigs = string(platformConfigs)
	rule.Platforms = req.PlatformConfigs
	rule.Filters = req.Filters
	rule.ScopeType = req.ScopeType
	rule.ScopeValues = req.ScopeValues
	rule.ActiveStartMinute = *req.ActiveStartMinute
	rule.ActiveEndMinute = *req.ActiveEndMinute
	rule.UpdatedAt = time.Now().Unix()
	if err := model.UpdateLogAlertRule(c.Request.Context(), rule); err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rule)
}

func DeleteLogAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	if err := model.DeleteLogAlertRule(c.Request.Context(), id); err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, gin.H{"id": id})
}

// ToggleLogAlertRule 启用/禁用规则。
//   - 禁用：Enabled=false 即可，评估器 ListEnabledLogAlertRules 自动跳过。
//   - 启用：Enabled=true，并把 LastScanAt 重置为 now-interval*60，
//     使下一 tick 的窗口只覆盖"配置的告警频率分钟内"的最新日志，
//     避免扫描禁用期间累积的历史错误一次全推。
//
// 路由：PUT /api/error-log-alerts/rules/:id/toggle
// 请求体：{"enabled": true|false}
func ToggleLogAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.BindJSON(&req); err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	rule, err := model.GetLogAlertRule(c.Request.Context(), id)
	if err != nil {
		responseErr(c, http.StatusNotFound, err.Error())
		return
	}
	rule.Enabled = req.Enabled
	if req.Enabled {
		interval := rule.IntervalMinutes
		if interval <= 0 {
			interval = 1
		}
		rule.LastScanAt = time.Now().Unix() - int64(interval)*60
	}
	rule.UpdatedAt = time.Now().Unix()
	if err := model.UpdateLogAlertRule(c.Request.Context(), rule); err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rule)
}

// TestLogAlertRule 立即推送一条测试消息到规则配置的 webhook，不写 event、不占用冷却。
// 消息结构与真实告警一致：带 scope label 与示例链接，正文替换为测试提示。
func TestLogAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	rule, err := model.GetLogAlertRule(c.Request.Context(), id)
	if err != nil {
		responseErr(c, http.StatusNotFound, err.Error())
		return
	}
	scopeLabel := service.FirstScopeLabelForRule(rule)
	previewLink := service.BuildLogAlertPreviewLink(rule)
	weComMsg := service.FormatLogAlertMarkdown(rule.Name, rule.ScopeType, scopeLabel, 0, rule.IntervalMinutes, previewLink, true)
	telegramMsg := service.FormatLogAlertTelegram(rule.Name, rule.ScopeType, scopeLabel, 0, rule.IntervalMinutes, previewLink, true)
	failures := service.SendLogAlertPlatforms(rule, weComMsg, telegramMsg)
	failureNames := make([]string, 0, len(failures))
	for _, failure := range failures {
		if failure.PlatformType == "" {
			failureNames = append(failureNames, "platform config")
			continue
		}
		failureNames = append(failureNames, fmt.Sprintf("%s[%d]", failure.PlatformType, failure.Index))
	}
	responseOK(c, gin.H{
		"sent":                    len(failures) == 0,
		"failed_platforms":        failureNames,
		"failed_platform_details": failures,
	})
}

func ListLogAlertEvents(c *gin.Context) {
	ruleId, _ := strconv.Atoi(c.DefaultQuery("rule_id", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	events, err := model.ListLogAlertEvents(c.Request.Context(), ruleId, limit)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, events)
}

// LookupUsersForLogAlert 用户下拉：支持"打开即列出全量 + 输入 keyword 过滤"两种交互。
// 用分页模型承载：page/page_size 由前端控制，接口不做上限 clamp，
// 让前端可自行决定"一次拉多少 / 是否继续加载下一页"。
// 空 keyword 时走 SearchUsers 传空词（等价于 LIKE '%%'），一并复用其分页与 total 统计。
// 返回结构 {items, page, page_size, total}，前端据 total 判是否还有下一页。
// 路由：GET /api/error-log-alerts/lookup/users?keyword=xxx&page=1&page_size=20
func LookupUsersForLogAlert(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize <= 0 {
		pageSize = 20
	}
	startIdx := (page - 1) * pageSize
	users, total, err := model.SearchUsers(keyword, "", nil, nil, nil, nil, nil, nil, startIdx, pageSize)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]gin.H, 0, len(users))
	for _, u := range users {
		items = append(items, gin.H{"id": u.Id, "username": u.Username, "display_name": u.DisplayName})
	}
	responseOK(c, gin.H{
		"items":     items,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	})
}

// LookupTokensForLogAlert 列出指定用户下的**全部密钥**，供告警规则编辑页做密钥多选。
// 只 SELECT id, name：告警链路用不到密钥明文，避免把 Key 加载进内存
// （不用 GetAllUserTokens，那个方法返回完整 Token 实体含 Key 字段）。
// 不做条数上限：Limit(-1) 移除 LIMIT 子句，等价于取全量；避免"密钥数超过 N 就选不到"这种隐式截断。
// 路由：GET /api/error-log-alerts/lookup/tokens?user_id=xxx
func LookupTokensForLogAlert(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	if userId <= 0 {
		responseErr(c, http.StatusBadRequest, "user_id required")
		return
	}
	var out []struct {
		Id   int    `json:"id"`
		Name string `json:"name"`
	}
	err := model.DB.WithContext(c.Request.Context()).
		Model(&model.Token{}).
		Select("id, name").
		Where("user_id = ?", userId).
		Order("id desc").
		Find(&out).Error
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, out)
}

// AckLogAlertEvent 匿名端点。校验 token → 更新 AckedAt → 冷却延长为 15 分钟 → 302 跳转通用日志页面。
// 路由：GET /api/error-log-alerts/events/:id/ack?t={token}
func AckLogAlertEvent(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid event id")
		return
	}
	token := strings.TrimSpace(c.Query("t"))
	if token == "" {
		c.String(http.StatusBadRequest, "missing ack token")
		return
	}
	ev, err := model.GetLogAlertEvent(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, "event not found")
		return
	}
	if ev.AckToken == "" || ev.AckToken != token {
		c.String(http.StatusForbidden, "invalid ack token")
		return
	}
	now := time.Now().Unix()
	if ev.AckedAt == 0 {
		_ = model.AckLogAlertEvent(c.Request.Context(), ev.Id, now)
	}
	// 延长冷却到 15 分钟
	_ = service.LogAlertCooldownExtend(ev.ScopeKey, service.LogAlertAckedCooldown)

	// 302 跳转到通用日志页，携带 request_id 用于前端定位/过滤。
	// base 复用 ResolveLogAlertBaseURL()：与告警链接自身的 base 一致，
	// 避免"链接指向 A 域名 → 302 跳到 B 域名（或 localhost）"这类割裂。
	// 前端通用日志页面路由（web/default）：/usage-logs/common
	base := service.ResolveLogAlertBaseURL()
	redirect := fmt.Sprintf("%s/usage-logs/common?request_id=%s", base, url.QueryEscape(ev.SampleRequest))
	common.SysLog(fmt.Sprintf("log alert acked: event=%d rule=%d scope=%s", ev.Id, ev.RuleId, ev.ScopeKey))
	c.Redirect(http.StatusFound, redirect)
}
