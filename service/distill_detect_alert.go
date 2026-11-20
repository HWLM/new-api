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
	sent := false

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
			sent = true
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
			sent = true
		}
	}

	if sent {
		hit.AlertSent = true
	}
	return firstErr
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

	if out.Telegram.Status == "sent" || out.WeCom.Status == "sent" {
		return out
	}
	_ = ctx // 发送函数本身不接 context；保留参数以便将来改成可取消
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

// distillAlertTimeout 与判定调用无关，是告警发送的兜底上下文。
var _ = time.Second
