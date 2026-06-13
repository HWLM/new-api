package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

// 请求-响应统计分析:HTTP handlers。
// 管理员端走 /api/metrics/*  自助端走 /api/metrics/self/* (自动注入当前用户过滤)。

func parseMetricsRange(c *gin.Context) (from, to int64, rangeStr string, ok bool) {
	rangeStr = c.DefaultQuery("range", "30m")
	from, to, err := service.ParseTimeRange(rangeStr, time.Now().Unix())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return 0, 0, "", false
	}
	return from, to, rangeStr, true
}

func currentUserIdOrZero(c *gin.Context) int {
	return c.GetInt("id")
}

func responseOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}

func responseErr(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"success": false, "message": msg})
}

// GET /api/metrics/overview
func GetMetricsOverview(c *gin.Context) {
	from, to, _, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	userIdFilter, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	result, err := service.QueryOverview(c.Request.Context(), from, to, userIdFilter)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/self/overview
func GetSelfMetricsOverview(c *gin.Context) {
	from, to, _, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	uid := currentUserIdOrZero(c)
	if uid <= 0 {
		responseErr(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	result, err := service.QueryOverview(c.Request.Context(), from, to, uid)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/users
func GetUserMetrics(c *gin.Context) {
	from, to, _, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	filter := service.UserMetricsFilter{
		Username:    c.DefaultQuery("username", ""),
		ChannelType: 0,
	}
	if ct, err := strconv.Atoi(c.DefaultQuery("channel_type", "0")); err == nil {
		filter.ChannelType = ct
	}
	rows, err := service.QueryUserMetrics(c.Request.Context(), from, to, page, size, filter)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	total, _ := service.QueryUserMetricsCount(c.Request.Context(), from, to, filter)
	responseOK(c, gin.H{
		"rows":  rows,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

// GET /api/metrics/platforms
func GetPlatformMetrics(c *gin.Context) {
	from, to, _, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	rows, err := service.QueryPlatformMetrics(c.Request.Context(), from, to)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/platform/:type/channels
func GetPlatformChannels(c *gin.Context) {
	from, to, _, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	t, err := strconv.Atoi(c.Param("type"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid type")
		return
	}
	userIdFilter, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	rows, err := service.QueryPlatformChannels(c.Request.Context(), t, from, to, userIdFilter)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/channel/:id/models
func GetChannelModels(c *gin.Context) {
	from, to, _, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	userIdFilter, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	rows, err := service.QueryChannelModels(c.Request.Context(), id, from, to, userIdFilter)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/trend
func GetMetricsTrend(c *gin.Context) {
	from, to, rangeStr, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	bucket := service.BucketSecondsForRange(rangeStr)
	result, err := service.QueryTrend(c.Request.Context(), from, to, bucket, 0)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/self/trend
func GetSelfMetricsTrend(c *gin.Context) {
	from, to, rangeStr, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	uid := currentUserIdOrZero(c)
	if uid <= 0 {
		responseErr(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	bucket := service.BucketSecondsForRange(rangeStr)
	result, err := service.QueryTrend(c.Request.Context(), from, to, bucket, uid)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/errors/top
func GetErrorsTop(c *gin.Context) {
	from, to, _, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	ct, _ := strconv.Atoi(c.DefaultQuery("channel_type", "0"))
	cid, _ := strconv.Atoi(c.DefaultQuery("channel_id", "0"))
	uid, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	rows, err := service.QueryErrorsTop(c.Request.Context(), from, to, limit, service.ErrorTopFilter{
		ChannelType: ct, ChannelId: cid, UserId: uid,
	})
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/errors/detail
func GetErrorsDetail(c *gin.Context) {
	from, to, _, ok := parseMetricsRange(c)
	if !ok {
		return
	}
	code := c.Query("error_code")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	rows, err := service.QueryErrorsDetail(c.Request.Context(), from, to, code, limit)
	if err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/settings
func GetMetricsSettings(c *gin.Context) {
	th := setting.GetMetricsThresholds()
	kws := setting.GetMetricsBusinessKeywords()
	rd := setting.GetMetricsRetentionDays()
	tg := setting.GetMetricsAlertTG()
	written, dropped, bufferUsed := service.GetRequestMetricsStats()
	responseOK(c, gin.H{
		"slow_response_ms":        th.SlowResponseMs,
		"slow_ttft_ms":            th.SlowTTFTMs,
		"business_error_keywords": kws,
		"log_retention_days":      rd,
		"alert_tg_bot_token":      tg.BotToken,
		"alert_tg_chat_id":        tg.ChatId,
		"writer_written":          written,
		"writer_dropped":          dropped,
		"writer_buffer_used":      bufferUsed,
	})
}

// PUT /api/metrics/settings
type updateMetricsSettingsReq struct {
	SlowResponseMs        *int      `json:"slow_response_ms,omitempty"`
	SlowTTFTMs            *int      `json:"slow_ttft_ms,omitempty"`
	BusinessErrorKeywords *[]string `json:"business_error_keywords,omitempty"`
	LogRetentionDays      *int      `json:"log_retention_days,omitempty"`
	AlertTgBotToken       *string   `json:"alert_tg_bot_token,omitempty"`
	AlertTgChatId         *string   `json:"alert_tg_chat_id,omitempty"`
}

func UpdateMetricsSettings(c *gin.Context) {
	var req updateMetricsSettingsReq
	if err := c.BindJSON(&req); err != nil {
		responseErr(c, http.StatusBadRequest, err.Error())
		return
	}
	updates := map[string]string{}
	if req.SlowResponseMs != nil {
		updates[setting.OptKeyMetricsSlowResponseMs] = strconv.Itoa(*req.SlowResponseMs)
	}
	if req.SlowTTFTMs != nil {
		updates[setting.OptKeyMetricsSlowTTFTMs] = strconv.Itoa(*req.SlowTTFTMs)
	}
	if req.BusinessErrorKeywords != nil {
		var lines []string
		for _, s := range *req.BusinessErrorKeywords {
			s = strings.TrimSpace(s)
			if s != "" {
				lines = append(lines, s)
			}
		}
		updates[setting.OptKeyMetricsBusinessKws] = strings.Join(lines, "\n")
	}
	if req.LogRetentionDays != nil {
		updates[setting.OptKeyMetricsRetentionDays] = strconv.Itoa(*req.LogRetentionDays)
	}
	if req.AlertTgBotToken != nil {
		updates[setting.OptKeyMetricsAlertTgBotToken] = strings.TrimSpace(*req.AlertTgBotToken)
	}
	if req.AlertTgChatId != nil {
		updates[setting.OptKeyMetricsAlertTgChatId] = strings.TrimSpace(*req.AlertTgChatId)
	}
	for k, v := range updates {
		if err := model.UpdateOption(k, v); err != nil {
			responseErr(c, http.StatusInternalServerError, err.Error())
			return
		}
		setting.ApplyMetricsOption(k, v)
	}
	GetMetricsSettings(c)
}

// POST /api/metrics/alert-test
// 用全局 TG 配置发一条测试消息,验证 token / chat id 是否正确。
// 支持 body 传入临时 token/chat 测试还没保存的配置:
//
//	{ "tg_bot_token": "...", "tg_chat_id": "..." }
//
// 任一为空则取全局配置。
func TestAlertNotification(c *gin.Context) {
	var req struct {
		TgBotToken string `json:"tg_bot_token"`
		TgChatId   string `json:"tg_chat_id"`
	}
	// body 可选,忽略解析错误
	_ = c.ShouldBindJSON(&req)

	tg := setting.GetMetricsAlertTG()
	botToken := strings.TrimSpace(req.TgBotToken)
	if botToken == "" {
		botToken = tg.BotToken
	}
	chatId := strings.TrimSpace(req.TgChatId)
	if chatId == "" {
		chatId = tg.ChatId
	}
	if botToken == "" || chatId == "" {
		responseErr(c, http.StatusBadRequest, "tg_bot_token / tg_chat_id is required (in body or settings)")
		return
	}

	text := "【测试】请求-响应统计分析告警测试\n如果您能看到这条消息,说明 TG 通知配置正确。"
	if err := service.SendTelegramMessage(botToken, chatId, text); err != nil {
		responseErr(c, http.StatusInternalServerError, "send failed: "+err.Error())
		return
	}
	responseOK(c, gin.H{"sent": true})
}

