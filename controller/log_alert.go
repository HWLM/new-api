package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// 错误日志告警：规则 CRUD + 测试推送 + 匿名 ack 端点。

type logAlertRuleReq struct {
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"interval_minutes"`
	WebhookUrl      string `json:"webhook_url"`
	Filters         string `json:"filters"`      // 原样 JSON 字符串
	ScopeType       string `json:"scope_type"`   // all|user|token|channel
	ScopeValues     string `json:"scope_values"` // 原样 JSON 字符串
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
	req.WebhookUrl = strings.TrimSpace(req.WebhookUrl)
	if req.WebhookUrl == "" {
		return fmt.Errorf("webhook url required")
	}
	if req.IntervalMinutes <= 0 {
		return fmt.Errorf("interval_minutes must be > 0")
	}
	scope := normalizeScopeType(req.ScopeType)
	if scope == "" {
		return fmt.Errorf("invalid scope_type")
	}
	req.ScopeType = scope
	return nil
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
	now := time.Now().Unix()
	rule := &model.LogAlertRule{
		Name:            req.Name,
		Enabled:         true, // 规则默认启用，前端不再提供开关
		IntervalMinutes: req.IntervalMinutes,
		WebhookUrl:      req.WebhookUrl,
		Filters:         req.Filters,
		ScopeType:       req.ScopeType,
		ScopeValues:     req.ScopeValues,
		LastScanAt:      now, // 首次扫描窗口 ~ IntervalMinutes 分钟，不会扫全库历史
		CreatedAt:       now,
		UpdatedAt:       now,
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
	rule.Name = req.Name
	// 保持原 Enabled 状态：启用/禁用只通过 /toggle 接口修改，避免编辑规则时被顺带覆盖
	rule.IntervalMinutes = req.IntervalMinutes
	rule.WebhookUrl = req.WebhookUrl
	rule.Filters = req.Filters
	rule.ScopeType = req.ScopeType
	rule.ScopeValues = req.ScopeValues
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
		rule.LastScanAt = time.Now().Unix() - int64(interval*60)
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
	msg := service.FormatLogAlertMarkdown(rule.Name, rule.ScopeType, scopeLabel, 0, rule.IntervalMinutes, previewLink, true)
	if err := service.SendWeComMarkdown(rule.WebhookUrl, msg); err != nil {
		responseErr(c, http.StatusBadGateway, err.Error())
		return
	}
	responseOK(c, gin.H{"sent": true})
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

// LookupUsersForLogAlert 按 keyword 搜索用户，供告警规则编辑页做用户下拉。
// 返回精简的 {id, username}，避免带出敏感字段。
// 路由：GET /api/error-log-alerts/lookup/users?keyword=xxx&limit=20
func LookupUsersForLogAlert(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var (
		users []*model.User
		err   error
	)
	if keyword == "" {
		users, _, err = model.GetAllUsers(&common.PageInfo{PageSize: limit, Page: 1})
	} else {
		users, _, err = model.SearchUsers(keyword, "", nil, nil, nil, nil, nil, nil, 0, limit)
	}
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		out = append(out, gin.H{"id": u.Id, "username": u.Username, "display_name": u.DisplayName})
	}
	responseOK(c, out)
}

// LookupTokensForLogAlert 列出指定用户下的密钥，供告警规则编辑页做密钥多选。
// 只返回 {id, name}，不返回密钥明文。
// 路由：GET /api/error-log-alerts/lookup/tokens?user_id=xxx
func LookupTokensForLogAlert(c *gin.Context) {
	userId, _ := strconv.Atoi(c.Query("user_id"))
	if userId <= 0 {
		responseErr(c, http.StatusBadRequest, "user_id required")
		return
	}
	tokens, err := model.GetAllUserTokens(userId, 0, 200)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]gin.H, 0, len(tokens))
	for _, tk := range tokens {
		out = append(out, gin.H{"id": tk.Id, "name": tk.Name})
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

	// 302 跳转到通用日志页，携带 request_id 用于前端定位/过滤
	base := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	// 前端通用日志页面路由（web/default）：/usage-logs/common
	// 附带 request_id 参数供前端过滤条读取（前端不实现该参数解析也不影响：至少打开了日志页）
	redirect := fmt.Sprintf("%s/usage-logs/common?request_id=%s", base, ev.SampleRequest)
	common.SysLog(fmt.Sprintf("log alert acked: event=%d rule=%d scope=%s → %s", ev.Id, ev.RuleId, ev.ScopeKey, redirect))
	c.Redirect(http.StatusFound, redirect)
}
