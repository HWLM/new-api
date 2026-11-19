package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 蒸馏命中的告警通道（规格 §7）。
//
// ⛔ 复用现有发送能力，但不复用它们的配置表：
//   - Telegram：沿用 OptionKeyTgBotToken / OptionKeyTgOverloadChatId；
//   - 企业微信：用蒸馏卡片里自己的 webhook（DistillDetectSetting.WeComWebhookUrl），
//     而不是 log_alert 的规则表 —— 那套是按错误日志过滤的规则引擎，语义无关。
//
// ⛔ 每次命中都发（不是每轮降级发一次）：观察期需要看到「哪些请求被判中」，
// 节流会把中间的命中吞掉，运营反而拿不到完整名单。命中量本身很小（一个用户
// 一轮降级通常只有一次判定被采样）。

// NotifyDistillHit 按开关发告警。返回的错误仅用于日志；
// hit.AlertSent 在任一通道发送成功时置 true。
func NotifyDistillHit(hit *model.DistillDetectHit) error {
	if hit == nil {
		return nil
	}
	s := operation_setting.GetDistillDetectSetting()
	text := buildDistillAlertText(hit, s)
	var firstErr error
	sent := false

	if s.AlertTelegram {
		token := model.GetOptionString(OptionKeyTgBotToken)
		chatID := strings.TrimSpace(model.GetOptionString(OptionKeyTgOverloadChatId))
		if token == "" || chatID == "" {
			common.SysLog("distill_detect: TG 告警已开启但 bot token / 群 ID 未配置，跳过")
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
