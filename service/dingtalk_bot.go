/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 钉钉自定义机器人文本推送。
// 参考：https://open.dingtalk.com/document/orgapp/custom-robot-access
// webhook URL 形如 https://oapi.dingtalk.com/robot/send?access_token={token}。
//
// 加签（secret 非空时启用）：sign = urlencode(base64(HMAC-SHA256(key=secret,
// msg=timestamp+"\n"+secret)))，timestamp 为毫秒，追加到 URL 的 &timestamp&sign。

type dingTalkTextContent struct {
	Content string `json:"content"`
}

type dingTalkTextReq struct {
	MsgType string              `json:"msgtype"`
	Text    dingTalkTextContent `json:"text"`
}

type dingTalkResp struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// SendDingTalkText 向钉钉自定义机器人发送文本消息。
//
// webhookURL 必须是 oapi.dingtalk.com/robot/send 地址，先走域名白名单再走
// SSRF 保护校验，禁止指向内网。secret 非空时按钉钉加签算法追加签名参数。
func SendDingTalkText(webhookURL string, secret string, text string) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return errors.New("dingtalk webhook url is empty")
	}
	if text == "" {
		return errors.New("dingtalk text content is empty")
	}
	if !strings.HasPrefix(webhookURL, "https://oapi.dingtalk.com/robot/send") {
		return errors.New("dingtalk webhook url must be an oapi.dingtalk.com robot send address")
	}
	if err := ValidateSSRFProtectedFetchURL(webhookURL); err != nil {
		return fmt.Errorf("dingtalk webhook url rejected: %w", err)
	}

	if secret = strings.TrimSpace(secret); secret != "" {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		webhookURL = dingTalkSignURL(webhookURL, secret, timestamp)
	}

	payload, err := common.Marshal(dingTalkTextReq{MsgType: "text", Text: dingTalkTextContent{Content: text}})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := GetSSRFProtectedHTTPClient().Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dingtalk webhook returned status %d: %s", resp.StatusCode, string(body))
	}
	// 钉钉成功也可能带 errcode != 0（如机器人被禁言）。
	var r dingTalkResp
	if err := common.Unmarshal(body, &r); err == nil && r.ErrCode != 0 {
		return fmt.Errorf("dingtalk webhook errcode=%d errmsg=%s", r.ErrCode, r.ErrMsg)
	}
	return nil
}

// dingTalkSignURL 按钉钉加签算法计算 sign 并追加到 webhook URL。
func dingTalkSignURL(webhookURL string, secret string, timestamp string) string {
	stringToSign := timestamp + "\n" + secret
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(h.Sum(nil)))

	sep := "?"
	if strings.Contains(webhookURL, "?") {
		sep = "&"
	}
	return webhookURL + sep + "timestamp=" + timestamp + "&sign=" + sign
}
