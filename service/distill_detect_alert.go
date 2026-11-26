package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 蒸馏命中的告警通道（规格 §7 / §14.2）。
//
// ⛔ 两个通道都只用蒸馏检测卡片自己的配置，完全不读系统通知那套：
//   - Telegram：DistillDetectSetting.TgBotToken / TgChatId（独立 bot）；
//   - 企业微信：DistillDetectSetting.WeComWebhookUrl（独立 webhook）。
// 早期版本复用 tg_notify_bot_token / tg_notify_overload_chat_id，会让运营改
// 蒸馏告警时把系统通知也一起改掉，反之亦然 —— 两套必须能各自独立配。
//
// ⛔ 每次命中都发（不是每轮降级发一次）：观察期需要看到「哪些请求被判中」，
// 节流会把中间的命中吞掉，运营反而拿不到完整名单。命中量本身很小（一个用户
// 一轮降级通常只有一次判定被采样）。

// NotifyDistillHit 按开关发告警。返回的错误仅用于日志；
// hit.AlertSent 在任一通道发送成功时置 true。
//
// ⛔ 节流按「用户」做，窗口 AlertThrottleSeconds（默认 600 秒，0 = 不节流）：
// 观察模式下没有「一轮降级只判一次」的抑制，30 秒桶 ≈ 2 判/分/人，
// 命中率 0.9 时不节流就是每分钟近两条 —— 一旦打开 TG/企微直接刷屏。
// 被节流掉的命中**照常写 hits**，完整名单在管理页，告警只负责喊人。
func NotifyDistillHit(hit *model.DistillDetectHit) error {
	if hit == nil {
		return nil
	}
	s := operation_setting.GetDistillDetectSetting()
	if s.AlertThrottleSeconds > 0 && !distillAlertTakeThrottle(hit.UserId, s.AlertThrottleSeconds) {
		return nil
	}
	text := buildDistillAlertText(hit, s)
	var firstErr error
	sentAdmin := false
	sentUser := false
	var adminChannels []string

	if s.AlertTelegram {
		token := strings.TrimSpace(s.TgBotToken)
		chatID := strings.TrimSpace(s.TgChatId)
		if token == "" || chatID == "" {
			common.SysLog("distill_detect: TG 告警已开启但 bot token / chat id 未配置，跳过")
		} else if err := SendTelegramMessageNoPreview(token, chatID, text); err != nil {
			common.SysError("distill_detect: TG 告警发送失败: " + err.Error())
			if firstErr == nil {
				firstErr = err
			}
		} else {
			sentAdmin = true
			adminChannels = append(adminChannels, "telegram")
		}
	}

	if s.AlertWeCom {
		webhook := strings.TrimSpace(s.WeComWebhookUrl)
		if webhook == "" {
			common.SysLog("distill_detect: 企微告警已开启但 webhook 未配置，跳过")
		} else if err := SendWeComMarkdown(webhook, text); err != nil {
			common.SysError("distill_detect: 企微告警发送失败: " + err.Error())
			if firstErr == nil {
				firstErr = err
			}
		} else {
			sentAdmin = true
			adminChannels = append(adminChannels, "wecom")
		}
	}

	// 用户侧告警：复用现有节流门，把用户可见的告警发到被判定用户自己维护的
	// 通知渠道（复用 NotifyType，支持 email/webhook/bark/gotify/飞书/钉钉）。
	// 全局开关/用户三态未放行、或渠道未配置时静默跳过。
	if userSent, err := notifyDistillHitToUser(hit); err != nil {
		common.SysError("distill_detect: 用户侧告警发送失败: " + err.Error())
		if firstErr == nil {
			firstErr = err
		}
	} else if userSent {
		sentUser = true
	}

	if sentAdmin {
		hit.AlertSent = true
	}
	if sentUser {
		hit.AlertSentUser = true
	}
	hit.AlertSentChannels = strings.Join(adminChannels, ",")
	// 什么都没发出去（通道全关/未配置/发送失败）：释放本窗口的节流名额，
	// 避免「既没告警、下一窗口也被抑制」。
	if !sentAdmin && !sentUser {
		distillAlertReleaseThrottle(hit.UserId)
	}
	return firstErr
}

// notifyDistillHitToUser 把蒸馏命中告警发给被判定用户本人维护的通知渠道。
//
// 返回 (sent bool, err error)：sent 表示发送未出错。err 仅在发送失败时非空；
// 全局开关/用户三态未放行时返回 (false, nil)，静默跳过。
//
// 渠道复用客户在「通知方式」里自填的 NotifyType（email/webhook/bark/gotify/
// 飞书/钉钉）与对应配置 —— 管理员只决定「是否通知」（三态），不再重复维护渠道。
func notifyDistillHitToUser(hit *model.DistillDetectHit) (bool, error) {
	if hit == nil {
		return false, nil
	}
	// 全局开关 + 用户三态：默认不通知用户，避免打草惊蛇。
	if !IsDistillNotifyEffectiveForUser(hit.UserId) {
		return false, nil
	}
	s := operation_setting.GetDistillDetectSetting()
	user, err := model.GetUserById(hit.UserId, false)
	if err != nil || user == nil {
		return false, err
	}
	setting := user.GetSetting()

	// 发送前检查渠道是否已配置：未配置则静默跳过，不算发送成功，避免把
	// 「未配置跳过」误标为「已发送」。判定条件与 NotifyUser 内部保持一致。
	notifyType := setting.NotifyType
	if notifyType == "" {
		notifyType = dto.NotifyTypeEmail
	}
	switch notifyType {
	case dto.NotifyTypeEmail:
		if setting.NotificationEmail == "" && user.Email == "" {
			return false, nil
		}
	case dto.NotifyTypeWebhook:
		if setting.WebhookUrl == "" {
			return false, nil
		}
	case dto.NotifyTypeBark:
		if setting.BarkUrl == "" {
			return false, nil
		}
	case dto.NotifyTypeGotify:
		if setting.GotifyUrl == "" || setting.GotifyToken == "" {
			return false, nil
		}
	case dto.NotifyTypeFeishu:
		if setting.FeishuWebhookUrl == "" {
			return false, nil
		}
	case dto.NotifyTypeDingTalk:
		if setting.DingTalkWebhookUrl == "" {
			return false, nil
		}
	default:
		return false, nil
	}

	title, body := BuildDistillUserAlertText(hit, user.Username, s.ObserveOnly, setting.Language)

	// 复用通用通知链路：用户「通知方式」选哪个渠道，蒸馏告警就发哪个渠道。
	notify := dto.NewNotify(dto.NotifyTypeDistillHit, title, body, nil)
	if err := NotifyUser(user.Id, user.Email, setting, notify); err != nil {
		return false, err
	}
	hit.AlertSentUserChannel = notifyType
	return true, nil
}

// distillUserAlertTemplate 用户侧告警文案的多语言模板。
type distillUserAlertTemplate struct {
	Title   string
	Account string
	Request string
	Model   string
	Time    string
	Observe string
	Reroute string
	Sep     string
}

var distillUserAlertTemplates = map[string]distillUserAlertTemplate{
	"en": {
		Title:   "Distillation Detection Notice",
		Account: "Account",
		Request: "Request ID",
		Model:   "Model",
		Time:    "Time",
		Observe: "Our system detected request patterns under your account that look like batch training-data generation. Please review your usage. If you have questions, please contact the platform administrator.",
		Reroute: "Our system detected requests under your account for batch training-data generation. The related traffic has been temporarily routed to the fallback channel. Please comply with the usage policy. If you have questions, please contact the platform administrator.",
		Sep:     ": ",
	},
	"zh": {
		Title:   "【蒸馏检测】提示",
		Account: "账号",
		Request: "请求编号",
		Model:   "模型",
		Time:    "时间",
		Observe: "系统检测到你的账号下存在疑似批量生成训练数据的请求特征，请自查使用方式。如有疑问，请联系平台管理员。",
		Reroute: "系统检测到你的账号下存在批量生成训练数据的请求，相关流量已临时切换至兜底渠道，请规范使用。如有疑问，请联系平台管理员。",
		Sep:     "：",
	},
	"zh-TW": {
		Title:   "【蒸餾檢測】提示",
		Account: "帳號",
		Request: "請求編號",
		Model:   "模型",
		Time:    "時間",
		Observe: "系統偵測到你的帳號下存在疑似批次生成訓練資料的請求特徵，請自查使用方式。如有疑問，請聯絡平台管理員。",
		Reroute: "系統偵測到你的帳號下存在批次生成訓練資料的請求，相關流量已臨時切換至備援管道，請規範使用。如有疑問，請聯絡平台管理員。",
		Sep:     "：",
	},
}

// distillUserAlertTemplateFor 按用户语言偏好选模板；未知语言回落到简体中文。
func distillUserAlertTemplateFor(lang string) distillUserAlertTemplate {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(l, "en"):
		return distillUserAlertTemplates["en"]
	case strings.HasPrefix(l, "zh-tw"), strings.HasPrefix(l, "zh_tw"), strings.HasPrefix(l, "zh-hant"):
		return distillUserAlertTemplates["zh-TW"]
	default:
		return distillUserAlertTemplates["zh"]
	}
}

// BuildDistillUserAlertText 生成发给被判定用户本人的告警标题与正文（用户可见模板）。
//
// ⛔ 与 buildDistillAlertText 完全独立：只含账号/请求编号/模型/时间/说明，
// 绝不包含分数/阈值/渠道 id/兜底渠道 id 等运营口径 —— 否则等于把判定口径与
// 内部拓扑交给被判定方。标题与正文均按用户语言偏好本地化。
//
// ⛔ 折中方案：平台无法区分对方是终端蒸馏者还是二级代理，措辞保持中性（
// 「你的账号下」「疑似」），观察模式与重导模式用不同语气，避免打草惊蛇或
// 虚假指控。
func BuildDistillUserAlertText(hit *model.DistillDetectHit, username string, observeOnly bool, lang string) (title, body string) {
	tpl := distillUserAlertTemplateFor(lang)
	var b strings.Builder
	if strings.TrimSpace(username) != "" {
		b.WriteString(tpl.Account + tpl.Sep + username + "\n")
	}
	b.WriteString(tpl.Request + tpl.Sep + hit.RequestId + "\n")
	if strings.TrimSpace(hit.Model) != "" {
		b.WriteString(tpl.Model + tpl.Sep + hit.Model + "\n")
	}
	b.WriteString(tpl.Time + tpl.Sep + hit.SampledAt.Format("2006-01-02 15:04:05") + "\n")
	if observeOnly {
		b.WriteString(tpl.Observe)
	} else {
		b.WriteString(tpl.Reroute)
	}
	return tpl.Title, b.String()
}

// distillAlertThrottleKeyPrefix 告警节流键前缀。
const distillAlertThrottleKeyPrefix = "distill:alert:"

var (
	distillAlertThrottleMu sync.Mutex
	distillAlertThrottle   = map[int]time.Time{}
)

// distillAlertTakeThrottle 抢占该用户在本轮节流窗口里的第一条告警名额。
// 抢到返回 true（应当发送），窗口内已有记录返回 false。
//
// ⛔ 用 SETNX 而不是「读时间戳再写」：并发命中时后者会让多条同时判为「可以发」。
// Redis 不可用时退化为进程内记录，多实例下各发一条，仍好过完全刷屏。
func distillAlertTakeThrottle(userID, windowSeconds int) bool {
	if userID <= 0 {
		return false
	}
	key := fmt.Sprintf("%s{user:%d}", distillAlertThrottleKeyPrefix, userID)
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		ok, err := common.RDB.SetNX(ctx, key, "1", time.Duration(windowSeconds)*time.Second).Result()
		if err == nil {
			return ok
		}
		common.SysLog(fmt.Sprintf("distill_detect: alert throttle setnx failed user=%d err=%v, falling back to memory", userID, err))
	}
	distillAlertThrottleMu.Lock()
	defer distillAlertThrottleMu.Unlock()
	now := time.Now()
	if until, ok := distillAlertThrottle[userID]; ok && now.Before(until) {
		return false
	}
	distillAlertThrottle[userID] = now.Add(time.Duration(windowSeconds) * time.Second)
	return true
}

// distillAlertClearThrottle 清掉进程内的告警节流记录（规格 §20），返回清理条数。
// userID <= 0 表示清全部用户。
//
// ⛔ Redis 那份由 clearDistillRedisState 按 `distill:*` 一起删；这里只管内存。
func distillAlertClearThrottle(userID int) int {
	distillAlertThrottleMu.Lock()
	defer distillAlertThrottleMu.Unlock()
	if userID <= 0 {
		n := len(distillAlertThrottle)
		distillAlertThrottle = map[int]time.Time{}
		return n
	}
	if _, ok := distillAlertThrottle[userID]; !ok {
		return 0
	}
	delete(distillAlertThrottle, userID)
	return 1
}

// distillAlertReleaseThrottle 释放该用户本窗口的告警节流名额（Redis + 内存）。
// 用于「这次什么都没发出去」时回滚，避免失败也烧掉名额、抑制后续真正可发的命中。
func distillAlertReleaseThrottle(userID int) {
	if userID <= 0 {
		return
	}
	key := fmt.Sprintf("%s{user:%d}", distillAlertThrottleKeyPrefix, userID)
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		if err := common.RDB.Del(ctx, key).Err(); err != nil {
			common.SysLog(fmt.Sprintf("distill_detect: alert throttle release failed user=%d err=%v", userID, err))
		}
	}
	distillAlertThrottleMu.Lock()
	delete(distillAlertThrottle, userID)
	distillAlertThrottleMu.Unlock()
}

// DistillAlertChannelResult 是一个告警通道的测试结果。
//
// Status 取值：
//   - "sent"            发送成功
//   - "not_configured"  通道已启用但 token / chat id / webhook 没填
//   - "disabled"        通道开关没开
//   - "failed"          发送失败，Error 里带原因
type DistillAlertChannelResult struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// DistillAlertTestResult 是 POST /alert_test 的返回体。
type DistillAlertTestResult struct {
	Telegram DistillAlertChannelResult `json:"telegram"`
	WeCom    DistillAlertChannelResult `json:"wecom"`
}

// SendDistillTestAlert 用当前配置给每个通道发一条测试消息。
//
// ⛔ 与真实告警的区别：不看节流、不写 hits、不改任何状态。运营配完当场点一下
// 就知道通不通 —— 蒸馏命中稀疏，靠等真实命中来验证通道不现实。
//
// ⛔ 通道开关关着时**不发**，返回 disabled 而不是偷偷发一条：否则「关掉了
// 还在收消息」会让人以为开关坏了。
func SendDistillTestAlert(ctx context.Context) DistillAlertTestResult {
	s := operation_setting.GetDistillDetectSetting()
	var out DistillAlertTestResult

	if !s.AlertTelegram {
		out.Telegram = DistillAlertChannelResult{Status: "disabled"}
	} else if strings.TrimSpace(s.TgBotToken) == "" || strings.TrimSpace(s.TgChatId) == "" {
		out.Telegram = DistillAlertChannelResult{Status: "not_configured"}
	} else if err := SendTelegramMessageNoPreview(strings.TrimSpace(s.TgBotToken), strings.TrimSpace(s.TgChatId), distillAlertTestText()); err != nil {
		out.Telegram = DistillAlertChannelResult{Status: "failed", Error: err.Error()}
	} else {
		out.Telegram = DistillAlertChannelResult{Status: "sent"}
	}

	if !s.AlertWeCom {
		out.WeCom = DistillAlertChannelResult{Status: "disabled"}
	} else if strings.TrimSpace(s.WeComWebhookUrl) == "" {
		out.WeCom = DistillAlertChannelResult{Status: "not_configured"}
	} else if err := SendWeComMarkdown(strings.TrimSpace(s.WeComWebhookUrl), distillAlertTestText()); err != nil {
		out.WeCom = DistillAlertChannelResult{Status: "failed", Error: err.Error()}
	} else {
		out.WeCom = DistillAlertChannelResult{Status: "sent"}
	}

	return out
}

// distillAlertTestText 是测试消息正文。开头就写明是测试，避免运营群里误判。
func distillAlertTestText() string {
	return "【蒸馏检测】告警通道测试\n" +
		"如果你在群里看到这条消息，说明该通道配置正确，蒸馏命中时可以正常收到告警。\n" +
		"本条为手动测试，不是真实命中。"
}

// buildDistillAlertText 组装告警正文（规格 §7 的格式）。
func buildDistillAlertText(hit *model.DistillDetectHit, s *operation_setting.DistillDetectSetting) string {
	username := ""
	if cache, err := model.GetUserCache(hit.UserId); err == nil && cache != nil {
		username = cache.Username
	}
	who := fmt.Sprintf("%d", hit.UserId)
	if username != "" {
		who = fmt.Sprintf("%s(%d)", username, hit.UserId)
	}

	var b strings.Builder
	b.WriteString("【蒸馏检测】命中\n")
	fmt.Fprintf(&b, "用户：%s\n", who)
	fmt.Fprintf(&b, "请求编号：%s\n", hit.RequestId)
	fmt.Fprintf(&b, "分数：%.2f（阈值 %.2f）\n", hit.Score, s.Threshold)
	if hit.Model != "" {
		fmt.Fprintf(&b, "模型：%s\n", hit.Model)
	}
	if hit.ChannelId > 0 {
		if hit.FallbackId > 0 {
			fmt.Fprintf(&b, "渠道：%d → 兜底 %d\n", hit.ChannelId, hit.FallbackId)
		} else {
			fmt.Fprintf(&b, "渠道：%d\n", hit.ChannelId)
		}
	}
	fmt.Fprintf(&b, "时间：%s\n", hit.SampledAt.Format("2006-01-02 15:04:05"))
	if s.ObserveOnly {
		b.WriteString("模式：观察（未重导流量）\n")
	}
	return b.String()
}

// buildDistillRequest 造一个带 Bearer 的 POST。
// 密钥为空时不发 Authorization，兼容不鉴权的自建判定服务。
func buildDistillRequest(ctx context.Context, url string, body []byte, apiKey string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(apiKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req, nil
}

// readAllLimited 读取响应体，超过 limit 字节即停止（判定响应不应该很大）。
func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}
	return buf, nil
}
