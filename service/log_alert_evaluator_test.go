package service

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatLogAlertTelegramWithRequestsUsesPlainLink(t *testing.T) {
	ackLink := "https://example.com/api/error-log-alerts/events/1/ack?t=token"
	message := FormatLogAlertTelegramWithRequests(
		"rule **name**",
		"user",
		"alice",
		3,
		5,
		ackLink,
		"req-1,req-2",
		false,
	)

	require.Contains(t, message, "rule **name**")
	require.Contains(t, message, "相关请求id：req-1,req-2")
	require.Contains(t, message, "链接："+ackLink)
	require.NotContains(t, message, "["+ackLink+"]("+ackLink+")")
	require.Equal(t, 1, strings.Count(message, ackLink))
}

func TestAddRequestIDDeduplicatesAndCapsAtTen(t *testing.T) {
	ids := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		addRequestID(&ids, "req-"+string(rune('a'+i)))
	}
	addRequestID(&ids, "req-a")

	require.Len(t, ids, 10)
	require.Equal(t, "req-a", ids[0])
	require.NotContains(t, ids, "req-k")
}

func TestSendLogAlertPlatformsContinuesAfterPlatformFailure(t *testing.T) {
	originalWeCom := sendLogAlertWeCom
	originalTelegram := sendLogAlertTelegram
	t.Cleanup(func() {
		sendLogAlertWeCom = originalWeCom
		sendLogAlertTelegram = originalTelegram
	})

	var telegramCalls atomic.Int32
	sendLogAlertWeCom = func(string, string) error {
		return errors.New("wecom unavailable")
	}
	sendLogAlertTelegram = func(string, string, string) error {
		telegramCalls.Add(1)
		return nil
	}
	rule := &model.LogAlertRule{Platforms: []model.LogAlertPlatformConfig{
		{Type: model.LogAlertPlatformWeComGroup, WebhookURL: "https://example.com/wecom"},
		{Type: model.LogAlertPlatformTelegram, BotToken: "token", ChatID: "-100"},
	}}

	failures := SendLogAlertPlatforms(rule, "wecom", "telegram")

	require.Equal(t, int32(1), telegramCalls.Load())
	assert.Equal(t, []string{"wecom_group[0]"}, failures)
}
