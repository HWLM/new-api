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
package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type TgNotifySettings struct {
	BotToken string `json:"bot_token"`
	ChatId   string `json:"chat_id"`
	// 过载类告警（并发超限 / 拒答降级）专用接收群，空串表示回落到 ChatId。
	OverloadChatId string `json:"overload_chat_id"`
	// 独立开关：并发超限告警
	NotifyConcurrencyEnabled bool `json:"notify_concurrency_enabled"`
	// 独立开关：拒答降级告警
	NotifyRefusalDemoteEnabled bool `json:"notify_refusal_demote_enabled"`
}

// GetTgNotifySettings 返回当前 TG 通知群配置（仅管理员可调用）
func GetTgNotifySettings(c *gin.Context) {
	common.ApiSuccess(c, TgNotifySettings{
		BotToken:                   model.GetOptionString(service.OptionKeyTgBotToken),
		ChatId:                     model.GetOptionString(service.OptionKeyTgChatId),
		OverloadChatId:             model.GetOptionString(service.OptionKeyTgOverloadChatId),
		NotifyConcurrencyEnabled:   tgBoolOption(service.OptionKeyTgNotifyConcurrencyEnabled),
		NotifyRefusalDemoteEnabled: tgBoolOption(service.OptionKeyTgNotifyRefusalDemoteEnabled),
	})
}

// UpdateTgNotifySettings 保存 TG 通知群配置（仅管理员可调用）
func UpdateTgNotifySettings(c *gin.Context) {
	var req TgNotifySettings
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.UpdateOption(service.OptionKeyTgBotToken, req.BotToken); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption(service.OptionKeyTgChatId, req.ChatId); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption(service.OptionKeyTgOverloadChatId, req.OverloadChatId); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption(service.OptionKeyTgNotifyConcurrencyEnabled, boolOptionString(req.NotifyConcurrencyEnabled)); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption(service.OptionKeyTgNotifyRefusalDemoteEnabled, boolOptionString(req.NotifyRefusalDemoteEnabled)); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func tgBoolOption(key string) bool {
	switch model.GetOptionString(key) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

func boolOptionString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// TriggerTgNotifyManually 手动触发一次 TG 播报（用于测试，仅管理员可调用）。
// 忽略时间窗口、lastDate、重试计数 — 立即发送，并把发送结果原样返回。
func TriggerTgNotifyManually(c *gin.Context) {
	if err := service.TriggerVipReportManually(); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}

// TriggerVipLowBalanceManually 手动触发余额告警（仅管理员）。
// 用于测试 / 排查；忽略小时去重，立即扫描并发送。
func TriggerVipLowBalanceManually(c *gin.Context) {
	if err := service.TriggerVipLowBalanceManually(); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}
