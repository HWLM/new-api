package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

// 请求-响应统计分析:HTTP handlers。
// 管理员端走 /api/metrics/*  自助端走 /api/metrics/self/* (自动注入当前用户过滤)。
//
// 时间窗口参数（统一）：
//   from / to                必填，unix 秒，左闭右开 [from, to)
//   compare_from / compare_to 可选，开启对比时一并传入；缺省即不对比

func parseMetricsWindow(c *gin.Context) (service.TimeWindow, bool) {
	win, err := service.ParseTimeWindow(
		c.Query("from"), c.Query("to"),
		c.Query("compare_from"), c.Query("compare_to"),
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return service.TimeWindow{}, false
	}
	return win, true
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
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	userIdFilter, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	result, err := service.QueryOverview(c.Request.Context(), win, userIdFilter)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/self/overview
func GetSelfMetricsOverview(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	uid := currentUserIdOrZero(c)
	if uid <= 0 {
		responseErr(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	result, err := service.QueryOverview(c.Request.Context(), win, uid)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/users
func GetUserMetrics(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
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
	rows, err := service.QueryUserMetrics(c.Request.Context(), win, page, size, filter)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	total, _ := service.QueryUserMetricsCount(c.Request.Context(), win.From, win.To, filter)
	responseOK(c, gin.H{
		"rows":  rows,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

// GET /api/metrics/platforms
func GetPlatformMetrics(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	rows, err := service.QueryPlatformMetrics(c.Request.Context(), win.From, win.To)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/platform/:type/channels
func GetPlatformChannels(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	t, err := strconv.Atoi(c.Param("type"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid type")
		return
	}
	userIdFilter, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	rows, err := service.QueryPlatformChannels(c.Request.Context(), t, win, userIdFilter)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/channel/:id/models
func GetChannelModels(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		responseErr(c, http.StatusBadRequest, "invalid id")
		return
	}
	userIdFilter, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	rows, err := service.QueryChannelModels(c.Request.Context(), id, win, userIdFilter)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/trend
func GetMetricsTrend(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	channelType, _ := strconv.Atoi(c.DefaultQuery("channel_type", "0"))
	userIdFilter, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	bucket := service.BucketSecondsForSpan(win.Span())
	result, err := service.QueryTrend(c.Request.Context(), win, bucket, userIdFilter, channelType)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/self/trend
func GetSelfMetricsTrend(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	uid := currentUserIdOrZero(c)
	if uid <= 0 {
		responseErr(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	channelType, _ := strconv.Atoi(c.DefaultQuery("channel_type", "0"))
	bucket := service.BucketSecondsForSpan(win.Span())
	result, err := service.QueryTrend(c.Request.Context(), win, bucket, uid, channelType)
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/errors/top
func GetErrorsTop(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	ct, _ := strconv.Atoi(c.DefaultQuery("channel_type", "0"))
	cid, _ := strconv.Atoi(c.DefaultQuery("channel_id", "0"))
	uid, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	rows, err := service.QueryErrorsTop(c.Request.Context(), win, limit, service.ErrorTopFilter{
		ChannelType: ct, ChannelId: cid, UserId: uid,
	})
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, rows)
}

// GET /api/metrics/errors/trend
// 单错误码折线，用于"失败 Top10 行点击"的右侧联动图；
// 也支持 error_codes=A,B,C 形式聚合多个 code 的趋势（用于"未选中行时" top10 汇总）。
func GetErrorTrend(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	codes := []string{}
	if single := strings.TrimSpace(c.Query("error_code")); single != "" {
		codes = append(codes, single)
	}
	if csv := strings.TrimSpace(c.Query("error_codes")); csv != "" {
		for _, s := range strings.Split(csv, ",") {
			if s = strings.TrimSpace(s); s != "" {
				codes = append(codes, s)
			}
		}
	}
	if len(codes) == 0 {
		responseErr(c, http.StatusBadRequest, "error_code or error_codes is required")
		return
	}
	ct, _ := strconv.Atoi(c.DefaultQuery("channel_type", "0"))
	cid, _ := strconv.Atoi(c.DefaultQuery("channel_id", "0"))
	uid, _ := strconv.Atoi(c.DefaultQuery("user_id", "0"))
	bucket := service.BucketSecondsForSpan(win.Span())
	result, err := service.QueryErrorTrend(c.Request.Context(), win, bucket, codes, service.ErrorTopFilter{
		ChannelType: ct, ChannelId: cid, UserId: uid,
	})
	if err != nil {
		responseErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	responseOK(c, result)
}

// GET /api/metrics/errors/detail
func GetErrorsDetail(c *gin.Context) {
	win, ok := parseMetricsWindow(c)
	if !ok {
		return
	}
	code := c.Query("error_code")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	rows, err := service.QueryErrorsDetail(c.Request.Context(), win.From, win.To, code, limit)
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

