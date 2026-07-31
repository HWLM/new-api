package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogAlertRuleAlertPlatformsSupportsLegacyWebhook(t *testing.T) {
	rule := &LogAlertRule{WebhookUrl: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test"}

	platforms, err := rule.AlertPlatforms()

	require.NoError(t, err)
	require.Equal(t, []LogAlertPlatformConfig{{
		Type:       LogAlertPlatformWeComGroup,
		WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test",
	}}, platforms)
}

func TestLogAlertRuleAlertPlatformsReadsMultipleDestinations(t *testing.T) {
	rule := &LogAlertRule{PlatformConfigs: `[
		{"type":"wecom_group","webhook_url":"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test"},
		{"type":"telegram_bot","chat_id":"-100123","bot_token":"token"}
	]`}

	platforms, err := rule.AlertPlatforms()

	require.NoError(t, err)
	require.Len(t, platforms, 2)
	require.Equal(t, LogAlertPlatformTelegram, platforms[1].Type)
	require.Equal(t, "-100123", platforms[1].ChatID)
}
