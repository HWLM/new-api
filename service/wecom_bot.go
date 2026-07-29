package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 企业微信群机器人 markdown 推送。
// 参考：https://developer.work.weixin.qq.com/document/path/91770
// URL 形如：https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
//
// 单条 markdown 内容长度上限 4096 字节；本项目告警文案短，无需分片。

type weComMarkdown struct {
	Content string `json:"content"`
}

type weComMarkdownReq struct {
	MsgType  string        `json:"msgtype"`
	Markdown weComMarkdown `json:"markdown"`
}

type weComRespCommon struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// SendWeComMarkdown 向企微群机器人发送 markdown。
// webhookURL 必须是 https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=... 格式，
// 会先走 SSRF 保护校验，禁止指向内网。
//
// 传输层用 GetSSRFProtectedHTTPClient 返回的受保护 client：
// 它的 Transport 会在**每次 dial**（包括重定向后的跳转）解析 IP 并对内网段拉黑，
// 关掉了"前置字符串校验 + 后置重定向/DNS rebind 绕过"的漏洞。
// 超时通过 context 传递（15s），避免共享 client 的 Timeout 被本处覆盖影响其他调用方。
func SendWeComMarkdown(webhookURL string, content string) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return errors.New("wecom webhook url is empty")
	}
	if content == "" {
		return errors.New("wecom markdown content is empty")
	}
	if err := ValidateSSRFProtectedFetchURL(webhookURL); err != nil {
		return fmt.Errorf("wecom webhook url rejected: %w", err)
	}

	payload, err := common.Marshal(weComMarkdownReq{
		MsgType:  "markdown",
		Markdown: weComMarkdown{Content: content},
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := GetSSRFProtectedHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("wecom webhook returned status %d: %s", resp.StatusCode, string(body))
	}
	// 企微成功也可能带 errcode!=0（如 91300 群机器人被禁言）
	var r weComRespCommon
	if err := common.Unmarshal(body, &r); err == nil && r.ErrCode != 0 {
		return fmt.Errorf("wecom webhook errcode=%d errmsg=%s", r.ErrCode, r.ErrMsg)
	}
	return nil
}
