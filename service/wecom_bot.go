package service

import (
	"bytes"
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

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
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
