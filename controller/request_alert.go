package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// 告警规则 CRUD + 事件列表

func ListRequestAlertRules(c *gin.Context) {
	rules, err := model.ListRequestAlertRules(c.Request.Context())
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rules)
}

type alertRuleReq struct {
	Name             string  `json:"name"`
	Platforms        string  `json:"platforms"`         // JSON 数组字符串 "[1,14]"
	Metric           string  `json:"metric"`            // avg_duration_ms / slow_resp_rate / error_rate
	Operator         string  `json:"operator"`          // gt / eq
	Threshold        float64 `json:"threshold"`
	SustainedMinutes int     `json:"sustained_minutes"`
	CooldownMinutes  int     `json:"cooldown_minutes"`
	TgBotToken       string  `json:"tg_bot_token"`
	TgChatId         string  `json:"tg_chat_id"`
	Enabled          bool    `json:"enabled"`
}

func CreateRequestAlertRule(c *gin.Context) {
	var req alertRuleReq
	if err := c.BindJSON(&req); err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().Unix()
	rule := &model.RequestAlertRule{
		Name:             req.Name,
		Platforms:        req.Platforms,
		Metric:           req.Metric,
		Operator:         req.Operator,
		Threshold:        req.Threshold,
		SustainedMinutes: req.SustainedMinutes,
		CooldownMinutes:  req.CooldownMinutes,
		TgBotToken:       req.TgBotToken,
		TgChatId:         req.TgChatId,
		Enabled:          req.Enabled,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := model.CreateRequestAlertRule(c.Request.Context(), rule); err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rule)
}

func UpdateRequestAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	rule, err := model.GetRequestAlertRule(c.Request.Context(), id)
	if err != nil {
		responseErr(c, http.StatusNotFound, err.Error())
		return
	}
	var req alertRuleReq
	if err := c.BindJSON(&req); err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	rule.Name = req.Name
	rule.Platforms = req.Platforms
	rule.Metric = req.Metric
	rule.Operator = req.Operator
	rule.Threshold = req.Threshold
	rule.SustainedMinutes = req.SustainedMinutes
	rule.CooldownMinutes = req.CooldownMinutes
	rule.TgBotToken = req.TgBotToken
	rule.TgChatId = req.TgChatId
	rule.Enabled = req.Enabled
	rule.UpdatedAt = time.Now().Unix()
	if err := model.UpdateRequestAlertRule(c.Request.Context(), rule); err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rule)
}

func DeleteRequestAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	if err := model.DeleteRequestAlertRule(c.Request.Context(), id); err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, gin.H{"id": id})
}

func ListRequestAlertEvents(c *gin.Context) {
	from, _ := strconv.ParseInt(c.DefaultQuery("from", "0"), 10, 64)
	to, _ := strconv.ParseInt(c.DefaultQuery("to", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	events, err := model.ListRequestAlertEvents(c.Request.Context(), from, to, limit)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, events)
}
