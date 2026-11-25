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
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// 飞书自定义机器人文本推送。
// 参考：https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot
// webhook URL 形如 https://open.feishu.cn/open-apis/bot/v2/hook/{token}
// （国际版为 https://open.larksuite.com/open-apis/bot/v2/hook/{token}）。
//
// 签名校验（secret 非空时启用）：飞书官方示例用 hmac.new(string_to_sign) 只传一个
// 位置参数，即 HMAC 的 key = timestamp+"\n"+secret、message 为空，再 base64。
// ⛔ 这与常规「key=secret、message=string_to_sign」相反，务必用真实飞书机器人验证。

type feishuTextContent struct {
	Text string `json:"text"`
}

type feishuTextReq struct {
	MsgType   string           `json:"msg_type"`
	Content   feishuTextContent `json:"content"`
	Timestamp string           `json:"timestamp,omitempty"`
	Sign      string           `json:"sign,omitempty"`
}

type feishuResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// SendFeishuText 向飞书自定义机器人发送文本消息。
//
// webhookURL 必须是 open.feishu.cn 或 open.larksuite.com 的 bot hook 地址，先走
// 域名白名单再走 SSRF 保护校验，禁止指向内网。secret 非空时按飞书签名校验算法
// 在请求体里带 timestamp（秒级）与 sign。
func SendFeishuText(webhookURL string, secret string, text string) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return errors.New("feishu webhook url is empty")
	}
	if text == "" {
		return errors.New("feishu text content is empty")
	}
	if !strings.HasPrefix(webhookURL, "https://open.feishu.cn/open-apis/bot/v2/hook/") &&
		!strings.HasPrefix(webhookURL, "https://open.larksuite.com/open-apis/bot/v2/hook/") {
		return errors.New("feishu webhook url must be an open.feishu.cn or open.larksuite.com bot hook")
	}
	if err := ValidateSSRFProtectedFetchURL(webhookURL); err != nil {
		return fmt.Errorf("feishu webhook url rejected: %w", err)
	}

	req := feishuTextReq{MsgType: "text", Content: feishuTextContent{Text: text}}
	if secret = strings.TrimSpace(secret); secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		req.Timestamp = timestamp
		req.Sign = feishuSign(secret, timestamp)
	}

	payload, err := common.Marshal(req)
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
		return fmt.Errorf("feishu webhook returned status %d: %s", resp.StatusCode, string(body))
	}
	// 飞书成功也可能带 code != 0（如机器人被禁用）。
	var r feishuResp
	if err := common.Unmarshal(body, &r); err == nil && r.Code != 0 {
		return fmt.Errorf("feishu webhook code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}

// feishuSign 按飞书官方示例计算签名校验的 sign。
//
// ⛔ stringToSign = timestamp+"\n"+secret，HMAC 的 key 是 stringToSign 本身、
// message 为空，结果 base64。与钉钉/常见签名相反，保持与官方 Python 示例一致。
func feishuSign(secret string, timestamp string) string {
	stringToSign := timestamp + "\n" + secret
	h := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
