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
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type tgSendMessageReq struct {
	ChatId string `json:"chat_id"`
	Text   string `json:"text"`
}

// SendTelegramMessage 调用 Telegram Bot API 发送文本消息。
// 参考: https://core.telegram.org/bots/api#sendmessage
func SendTelegramMessage(botToken, chatId, text string) error {
	if botToken == "" || chatId == "" {
		return errors.New("bot token or chat id is empty")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload, err := common.Marshal(tgSendMessageReq{ChatId: chatId, Text: text})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
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

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram api returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
