package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// 蒸馏检测的管理端接口（docs/distill-detection-plan.md §6）。
//
// 三组：
//   - GET  /api/distill_detect/setting   读配置（含派生字段，供前端渲染）
//   - PUT  /api/distill_detect/setting   写配置（带校验；密钥不回显）
//   - GET  /api/distill_detect/hits      命中记录列表（运营拿 request_id 通知客户）
//   - POST /api/distill_detect/test      用一段文本试判一次（不落库、不告警）

// DistillSettingView 是返回给前端的配置视图。
//
// ⛔ JevApiKey 不回显真实值，只回「是否已配置」：管理页需要知道该不该让运营
// 重新填，但不能把密钥再送到浏览器。
type DistillSettingView struct {
	Enabled              bool    `json:"enabled"`
	ActivateRPM          int     `json:"activate_rpm"`
	JudgeIntervalSeconds int     `json:"judge_interval_seconds"`
	ConfirmHits          int     `json:"confirm_hits"`
	ConfirmWindowSeconds int     `json:"confirm_window_seconds"`
	Threshold            float64 `json:"threshold"`
	DemoteSeconds        int     `json:"demote_seconds"`
	ObserveOnly          bool    `json:"observe_only"`
	JevBaseURL           string  `json:"jev_base_url"`
	JevApiKeySet         bool    `json:"jev_api_key_set"`
	JevTimeoutMs         int     `json:"jev_timeout_ms"`
	MaxStateChars        int     `json:"max_state_chars"`
	HeadSampleChars      int     `json:"head_sample_chars"`
	// Questions 完整题库（含题面全文），题库是数据：管理页要看全文并编辑。
	Questions            []operation_setting.DistillQuestion `json:"questions"`
	AlertTelegram        bool                                `json:"alert_telegram"`
	AlertWeCom           bool                                `json:"alert_wecom"`
	TgBotTokenSet        bool                                `json:"tg_bot_token_set"`
	TgChatId             string                              `json:"tg_chat_id"`
	AlertThrottleSeconds int                                 `json:"alert_throttle_seconds"`
	WeComWebhookUrl      string                              `json:"wecom_webhook_url"`
	// DetectChannelIds 检测渠道白名单（规格 §16）。空数组 = 检测关闭。
	DetectChannelIds  []int `json:"detect_channel_ids"`
	FallbackChannelId int   `json:"fallback_channel_id"`
	DedupSeconds      int   `json:"dedup_seconds"`
}

// GetDistillDetectSetting 返回当前配置。
func GetDistillDetectSetting(c *gin.Context) {
	s := operation_setting.GetDistillDetectSetting()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": DistillSettingView{
			Enabled:              s.Enabled,
			ActivateRPM:          s.ActivateRPM,
			JudgeIntervalSeconds: s.JudgeIntervalSeconds,
			ConfirmHits:          s.ConfirmHits,
			ConfirmWindowSeconds: s.ConfirmWindowSeconds,
			Threshold:            s.Threshold,
			DemoteSeconds:        s.DemoteSeconds,
			ObserveOnly:          s.ObserveOnly,
			JevBaseURL:           s.JevBaseURL,
			JevApiKeySet:         strings.TrimSpace(s.JevApiKey) != "",
			JevTimeoutMs:         s.JevTimeoutMs,
			MaxStateChars:        s.MaxStateChars,
			HeadSampleChars:      s.HeadSampleChars,
			Questions:            s.Questions,
			AlertTelegram:        s.AlertTelegram,
			AlertWeCom:           s.AlertWeCom,
			TgBotTokenSet:        strings.TrimSpace(s.TgBotToken) != "",
			TgChatId:             s.TgChatId,
			AlertThrottleSeconds: s.AlertThrottleSeconds,
			WeComWebhookUrl:      s.WeComWebhookUrl,
			DetectChannelIds:     s.DetectChannelIds,
			FallbackChannelId:    s.FallbackChannelId,
			DedupSeconds:         s.DedupSeconds,
		},
	})
}

// DistillSettingUpdateRequest 是写配置的请求体。
//
// 指针字段 = 未提交；这样前端只改一个开关时不会把其它字段冲成零值。
type DistillSettingUpdateRequest struct {
	Enabled              *bool    `json:"enabled"`
	ActivateRPM          *int     `json:"activate_rpm"`
	JudgeIntervalSeconds *int     `json:"judge_interval_seconds"`
	ConfirmHits          *int     `json:"confirm_hits"`
	ConfirmWindowSeconds *int     `json:"confirm_window_seconds"`
	Threshold            *float64 `json:"threshold"`
	DemoteSeconds        *int     `json:"demote_seconds"`
	ObserveOnly          *bool    `json:"observe_only"`
	JevBaseURL           *string  `json:"jev_base_url"`
	JevApiKey            *string  `json:"jev_api_key"`
	JevTimeoutMs         *int     `json:"jev_timeout_ms"`
	MaxStateChars        *int     `json:"max_state_chars"`
	HeadSampleChars      *int     `json:"head_sample_chars"`
	// Questions 全量替换数组（不是增量补丁）：题库的增删改顺序都在一次提交里表达，
	// 后端校验后整体覆盖。
	Questions            *[]operation_setting.DistillQuestion `json:"questions"`
	AlertTelegram        *bool                                `json:"alert_telegram"`
	AlertWeCom           *bool                                `json:"alert_wecom"`
	TgBotToken           *string                              `json:"tg_bot_token"`
	TgChatId             *string                              `json:"tg_chat_id"`
	AlertThrottleSeconds *int                                 `json:"alert_throttle_seconds"`
	WeComWebhookUrl      *string                              `json:"wecom_webhook_url"`
	// DetectChannelIds 全量替换数组；传空数组 = 关闭检测（规格 §16）。
	DetectChannelIds  *[]int `json:"detect_channel_ids"`
	FallbackChannelId *int   `json:"fallback_channel_id"`
	DedupSeconds      *int   `json:"dedup_seconds"`
}

// UpdateDistillDetectSetting 写配置。
//
// 校验规则（越界的值一律拒绝，而不是静默钳制）：静默钳制会让运营以为改动
// 生效了，实际跑的是另一个数 —— 判定类参数尤其不能这样。
func UpdateDistillDetectSetting(c *gin.Context) {
	var req DistillSettingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数格式错误")
		return
	}
	s := operation_setting.GetDistillDetectSetting()

	if req.ActivateRPM != nil {
		if *req.ActivateRPM < 0 || *req.ActivateRPM > 100000 {
			common.ApiErrorMsg(c, "每分钟请求量阈值必须在 0~100000 之间")
			return
		}
	}
	if req.JudgeIntervalSeconds != nil {
		if *req.JudgeIntervalSeconds < 1 || *req.JudgeIntervalSeconds > 3600 {
			common.ApiErrorMsg(c, "判定间隔必须在 1~3600 秒之间")
			return
		}
	}
	if req.ConfirmHits != nil {
		if *req.ConfirmHits < 1 || *req.ConfirmHits > 100 {
			common.ApiErrorMsg(c, "命中确认次数必须在 1~100 之间")
			return
		}
	}
	if req.ConfirmWindowSeconds != nil {
		if *req.ConfirmWindowSeconds < 30 || *req.ConfirmWindowSeconds > 86400 {
			common.ApiErrorMsg(c, "确认窗口必须在 30 秒 ~ 24 小时之间")
			return
		}
	}
	if req.Threshold != nil {
		if *req.Threshold <= 0 || *req.Threshold > 1 {
			common.ApiErrorMsg(c, "判定阈值必须在 (0, 1] 之间")
			return
		}
	}
	if req.DemoteSeconds != nil {
		if *req.DemoteSeconds < 60 || *req.DemoteSeconds > 30*24*3600 {
			common.ApiErrorMsg(c, "降级时长必须在 60 秒 ~ 30 天之间")
			return
		}
	}
	if req.JevTimeoutMs != nil {
		if *req.JevTimeoutMs < 200 || *req.JevTimeoutMs > 60000 {
			common.ApiErrorMsg(c, "判定超时必须在 200ms ~ 60s 之间")
			return
		}
	}
	if req.MaxStateChars != nil {
		if *req.MaxStateChars < 500 || *req.MaxStateChars > 200000 {
			common.ApiErrorMsg(c, "正文上限必须在 500 ~ 200000 字符之间")
			return
		}
	}
	if req.HeadSampleChars != nil {
		if *req.HeadSampleChars < 0 || *req.HeadSampleChars > 200000 {
			common.ApiErrorMsg(c, "头部采样字符数必须在 0 ~ 200000 之间")
			return
		}
	}
	if req.JevBaseURL != nil {
		base := strings.TrimSpace(*req.JevBaseURL)
		if base != "" && !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			common.ApiErrorMsg(c, "判定服务地址必须以 http:// 或 https:// 开头")
			return
		}
	}
	if req.WeComWebhookUrl != nil {
		hook := strings.TrimSpace(*req.WeComWebhookUrl)
		if hook != "" && !strings.HasPrefix(hook, "https://qyapi.weixin.qq.com/") {
			common.ApiErrorMsg(c, "企业微信 webhook 必须是 qyapi.weixin.qq.com 地址")
			return
		}
	}
	if req.FallbackChannelId != nil {
		if *req.FallbackChannelId < 0 {
			common.ApiErrorMsg(c, "兜底渠道 ID 不能为负")
			return
		}
		if *req.FallbackChannelId > 0 {
			if _, err := model.GetChannelById(*req.FallbackChannelId, false); err != nil {
				common.ApiErrorMsg(c, fmt.Sprintf("兜底渠道 %d 不存在", *req.FallbackChannelId))
				return
			}
		}
	}
	if req.DedupSeconds != nil {
		if *req.DedupSeconds < 0 || *req.DedupSeconds > 86400 {
			common.ApiErrorMsg(c, "去重窗口必须在 0 ~ 86400 秒之间")
			return
		}
	}
	// 检测渠道白名单（规格 §16）：非正数由 Normalize 丢掉，这里只校验渠道真的存在。
	// 保存后再被删除的渠道 id 会在运行时被忽略（DetectsChannel 只比 id），不报错 ——
	// 删渠道是另一件事，不该把整张配置卡住不让改。
	if req.DetectChannelIds != nil {
		for _, id := range operation_setting.NormalizeDistillDetectChannelIds(*req.DetectChannelIds) {
			if _, err := model.GetChannelById(id, false); err != nil {
				common.ApiErrorMsg(c, fmt.Sprintf("检测渠道 %d 不存在", id))
				return
			}
		}
	}
	// ⛔ 检测渠道与兜底渠道互斥：同一渠道既被判定又当兜底，就是「判定 → 切到自己
	// → 再判定」的自环。按**合并后**的最终配置校验，而不是只看本次提交的字段 ——
	// 否则先存检测渠道、再单独存兜底渠道就能绕过。
	effectiveDetect := s.DetectChannelIds
	if req.DetectChannelIds != nil {
		effectiveDetect = operation_setting.NormalizeDistillDetectChannelIds(*req.DetectChannelIds)
	}
	effectiveFallback := s.FallbackChannelId
	if req.FallbackChannelId != nil {
		effectiveFallback = *req.FallbackChannelId
	}
	if err := operation_setting.CheckDistillChannelConflict(effectiveDetect, effectiveFallback); err != nil {
		msg := err.Error()
		var conflict *operation_setting.DistillChannelConflictError
		if errors.As(err, &conflict) {
			// 带上渠道名，运营才知道是哪一个；取名失败就退回只给 id。
			if ch, chErr := model.GetChannelById(conflict.ChannelID, false); chErr == nil && ch != nil && ch.Name != "" {
				msg = fmt.Sprintf("渠道 %d（%s）不能同时作为检测渠道和兜底渠道，请先从一端移除", conflict.ChannelID, ch.Name)
			}
		}
		common.ApiErrorMsg(c, msg)
		return
	}
	if req.AlertThrottleSeconds != nil {
		if *req.AlertThrottleSeconds < 0 || *req.AlertThrottleSeconds > 86400 {
			common.ApiErrorMsg(c, "告警节流窗口必须在 0 ~ 86400 秒之间（0 表示不节流）")
			return
		}
	}
	// 题库校验：越界一律拒绝。⛔ 尤其是「一题都不启用」必须拒绝 —— 那样判定
	// 永远不命中，功能静默失效而管理页看起来一切正常。
	if req.Questions != nil {
		questions := operation_setting.NormalizeDistillQuestions(*req.Questions)
		if err := operation_setting.ValidateDistillQuestions(questions); err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
	}
	if req.TgChatId != nil {
		if len([]rune(strings.TrimSpace(*req.TgChatId))) > 64 {
			common.ApiErrorMsg(c, "Telegram chat id 过长")
			return
		}
	}

	// 只覆盖提交了的字段。
	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	if req.ActivateRPM != nil {
		s.ActivateRPM = *req.ActivateRPM
	}
	if req.JudgeIntervalSeconds != nil {
		s.JudgeIntervalSeconds = *req.JudgeIntervalSeconds
	}
	if req.ConfirmHits != nil {
		s.ConfirmHits = *req.ConfirmHits
	}
	if req.ConfirmWindowSeconds != nil {
		s.ConfirmWindowSeconds = *req.ConfirmWindowSeconds
	}
	if req.Threshold != nil {
		s.Threshold = *req.Threshold
	}
	if req.DemoteSeconds != nil {
		s.DemoteSeconds = *req.DemoteSeconds
	}
	if req.ObserveOnly != nil {
		s.ObserveOnly = *req.ObserveOnly
	}
	if req.JevBaseURL != nil {
		s.JevBaseURL = strings.TrimSpace(*req.JevBaseURL)
	}
	// ⛔ 密钥留空 = 不改；要清空必须显式传一个特殊值，避免前端表单没加载出来
	// 就把密钥抹掉。这里约定：传空串视为「不改」，传 "__clear__" 才是清空。
	if req.JevApiKey != nil {
		key := strings.TrimSpace(*req.JevApiKey)
		switch key {
		case "":
			// 不改
		case "__clear__":
			s.JevApiKey = ""
		default:
			s.JevApiKey = key
		}
	}
	if req.JevTimeoutMs != nil {
		s.JevTimeoutMs = *req.JevTimeoutMs
	}
	if req.MaxStateChars != nil {
		s.MaxStateChars = *req.MaxStateChars
	}
	if req.HeadSampleChars != nil {
		s.HeadSampleChars = *req.HeadSampleChars
	}
	if req.Questions != nil {
		s.Questions = operation_setting.NormalizeDistillQuestions(*req.Questions)
	}
	if req.AlertTelegram != nil {
		s.AlertTelegram = *req.AlertTelegram
	}
	if req.AlertWeCom != nil {
		s.AlertWeCom = *req.AlertWeCom
	}
	// ⛔ 与 jev_api_key 同一套约定：留空 = 不改，传 "__clear__" 才是清空。
	// 不能把「前端表单没加载出来」当成「要清空」。
	if req.TgBotToken != nil {
		token := strings.TrimSpace(*req.TgBotToken)
		switch token {
		case "":
			// 不改
		case "__clear__":
			s.TgBotToken = ""
		default:
			s.TgBotToken = token
		}
	}
	if req.TgChatId != nil {
		s.TgChatId = strings.TrimSpace(*req.TgChatId)
	}
	if req.AlertThrottleSeconds != nil {
		s.AlertThrottleSeconds = *req.AlertThrottleSeconds
	}
	if req.WeComWebhookUrl != nil {
		s.WeComWebhookUrl = strings.TrimSpace(*req.WeComWebhookUrl)
	}
	if req.DetectChannelIds != nil {
		s.DetectChannelIds = operation_setting.NormalizeDistillDetectChannelIds(*req.DetectChannelIds)
	}
	if req.FallbackChannelId != nil {
		s.FallbackChannelId = *req.FallbackChannelId
	}
	if req.DedupSeconds != nil {
		s.DedupSeconds = *req.DedupSeconds
	}

	// 落库：走网关配置那套 kv（option 表），重启后仍生效。
	if err := persistDistillSetting(); err != nil {
		common.ApiErrorMsg(c, "保存失败："+err.Error())
		return
	}
	GetDistillDetectSetting(c)
}

// persistDistillSetting 把配置写进 option 表并刷新内存。
//
// ⛔ 走 config.GlobalConfig.SaveToDB —— 与 refusal_demote_setting 完全同一条路，
// 不自己造一份序列化，避免两套格式在重启后打架。SaveToDB 用反射按 json tag
// 展开成 `distill_detect_setting.<field>` 的 kv，启动时 LoadFromDB 再按同一套
// 规则灌回来，因此字段只要加在这里就自动持久化。
func persistDistillSetting() error {
	return config.GlobalConfig.SaveToDB(func(key, value string) error {
		return model.UpdateOption(key, value)
	})
}

// DistillHitView 是命中记录的对外形状。
type DistillHitView struct {
	Id          int64   `json:"id"`
	UserId      int     `json:"user_id"`
	Username    string  `json:"username"`
	RequestId   string  `json:"request_id"`
	Model       string  `json:"model"`
	ChannelId   int     `json:"channel_id"`
	FallbackId  int     `json:"fallback_id"`
	Score       float64 `json:"score"`
	Categories  string  `json:"categories"`
	DocChars    int     `json:"doc_chars"`
	SampledAt   string  `json:"sampled_at"`
	DemoteUntil string  `json:"demote_until"`
	AlertSent   bool    `json:"alert_sent"`
}

// ListDistillHits 按用户 / 时间 / 请求编号查命中记录。
func ListDistillHits(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	query := model.DB.Model(&model.DistillDetectHit{})
	if uid := c.Query("user_id"); uid != "" {
		id, err := strconv.Atoi(uid)
		if err != nil {
			common.ApiErrorMsg(c, "user_id 不是数字")
			return
		}
		query = query.Where("user_id = ?", id)
	}
	if rid := strings.TrimSpace(c.Query("request_id")); rid != "" {
		query = query.Where("request_id LIKE ?", "%"+rid+"%")
	}
	if from := strings.TrimSpace(c.Query("from")); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			query = query.Where("sampled_at >= ?", t)
		}
	}
	if to := strings.TrimSpace(c.Query("to")); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			query = query.Where("sampled_at <= ?", t)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var rows []model.DistillDetectHit
	if err := query.Order("sampled_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	items := make([]DistillHitView, 0, len(rows))
	for _, r := range rows {
		view := DistillHitView{
			Id:         r.Id,
			UserId:     r.UserId,
			RequestId:  r.RequestId,
			Model:      r.Model,
			ChannelId:  r.ChannelId,
			FallbackId: r.FallbackId,
			Score:      r.Score,
			Categories: r.Categories,
			DocChars:   r.DocChars,
			SampledAt:  r.SampledAt.Format(time.RFC3339),
			AlertSent:  r.AlertSent,
		}
		if r.DemoteUntil != nil {
			view.DemoteUntil = r.DemoteUntil.Format(time.RFC3339)
		}
		if cache, err := model.GetUserCache(r.UserId); err == nil && cache != nil {
			view.Username = cache.Username
		}
		items = append(items, view)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items": items,
			"total": total,
			"page":  page,
		},
	})
}

// DistillTestRequest 是试判请求体。
type DistillTestRequest struct {
	// Text 直接给一段正文；Body 给一段完整的请求 JSON（优先用它）。
	Text string `json:"text"`
	Body string `json:"body"`
}

// TestDistill 用给定文本试判一次：不落库、不告警、不改任何状态。
// 运营用它确认「密钥对不对、题面在这个模型上判得准不准」。
func TestDistill(c *gin.Context) {
	var req DistillTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数格式错误")
		return
	}
	raw := []byte(req.Body)
	var doc string
	if len(raw) > 0 {
		doc = service.DistillDocumentForTest(string(raw))
	} else {
		doc = strings.TrimSpace(req.Text)
	}
	if doc == "" {
		common.ApiErrorMsg(c, "请提供 text 或 body")
		return
	}
	verdict := service.JudgeDistill(c.Request.Context(), doc)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"hit":          verdict.Hit,
			"score":        verdict.Score,
			"categories":   verdict.Categories,
			"doc_chars":    verdict.DocChars,
			"skipped":      verdict.Skipped,
			"observe_only": operation_setting.GetDistillDetectSetting().ObserveOnly,
		},
	})
}

// TestDistillAlert 用当前告警配置发一条测试消息，逐通道返回实际结果。
//
// 运营配完 webhook / bot token 后当场点一下就知道通不通，不用等真实命中
// 才发现配错了 —— 蒸馏命中本来就稀疏，靠等命中来验证通道是折磨人。
//
// ⛔ 不受 AlertThrottleSeconds 约束：测试按钮按一下就发一条，节流是防刷屏，
// 不是防测试。也不写 hits、不改任何状态。
func TestDistillAlert(c *gin.Context) {
	results := service.SendDistillTestAlert(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    results,
	})
}
