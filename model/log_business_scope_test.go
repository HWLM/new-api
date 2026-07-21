package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	businessLogUserID       = 101
	businessLogDirectID     = 102
	businessLogNestedID     = 103
	businessLogUnrelatedID  = 104
	normalLogUserID         = 105
	normalLogDirectInviteID = 106
)

func seedBusinessLogUsers(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&[]User{
		{Id: businessLogUserID, Username: "business", AffCode: "business-aff", BusinessChannel: " enterprise "},
		{Id: businessLogDirectID, Username: "direct", AffCode: "direct-aff", InviterId: businessLogUserID},
		{Id: businessLogNestedID, Username: "nested", AffCode: "nested-aff", InviterId: businessLogDirectID},
		{Id: businessLogUnrelatedID, Username: "unrelated", AffCode: "unrelated-aff"},
		{Id: normalLogUserID, Username: "normal", AffCode: "normal-aff"},
		{Id: normalLogDirectInviteID, Username: "normal-direct", AffCode: "normal-direct-aff", InviterId: normalLogUserID},
	}).Error)
}

func TestGetUserLogScopeIDsUsesBusinessDirectInvitesOnly(t *testing.T) {
	truncateTables(t)
	seedBusinessLogUsers(t)

	businessScope, err := GetUserLogScopeIDs(businessLogUserID)
	require.NoError(t, err)
	require.Equal(t, []int{businessLogUserID, businessLogDirectID}, businessScope)

	normalScope, err := GetUserLogScopeIDs(normalLogUserID)
	require.NoError(t, err)
	require.Equal(t, []int{normalLogUserID}, normalScope)
}

func TestGetUserLogsAppliesBusinessScopeUsernameAndManageFiltering(t *testing.T) {
	truncateTables(t)
	seedBusinessLogUsers(t)

	require.NoError(t, LOG_DB.Create(&[]Log{
		{Id: 1, UserId: businessLogUserID, Username: "business", Type: LogTypeConsume, Content: "self-consume", Other: `{}`},
		{Id: 2, UserId: businessLogDirectID, Username: "direct", Type: LogTypeConsume, Content: "direct-consume", ChannelName: "private-channel", Other: `{"admin_info":{"secret":true},"audit_info":{"route":"/admin"},"login_method":"password"}`},
		{Id: 3, UserId: businessLogDirectID, Username: "direct", Type: LogTypeLogin, Content: "direct-login", Other: `{"login_method":"password","user_agent":"test-agent"}`},
		{Id: 4, UserId: businessLogNestedID, Username: "nested", Type: LogTypeConsume, Content: "nested-consume", Other: `{}`},
		{Id: 5, UserId: businessLogUnrelatedID, Username: "unrelated", Type: LogTypeConsume, Content: "unrelated-consume", Other: `{}`},
		{Id: 6, UserId: businessLogUserID, Username: "business", Type: LogTypeManage, Content: "self-manage", Other: `{}`},
		{Id: 7, UserId: businessLogDirectID, Username: "direct", Type: LogTypeManage, Content: "direct-manage", Other: `{}`},
		{Id: 8, UserId: normalLogUserID, Username: "normal", Type: LogTypeConsume, Content: "normal-consume", Other: `{}`},
		{Id: 9, UserId: normalLogDirectInviteID, Username: "normal-direct", Type: LogTypeConsume, Content: "normal-direct-consume", Other: `{}`},
	}).Error)

	scope, err := GetUserLogScopeIDs(businessLogUserID)
	require.NoError(t, err)

	logs, total, err := GetUserLogs(scope, LogTypeUnknown, 0, 0, "", "", "", 0, 100, "", "", "", true)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, logs, 3)
	assert.ElementsMatch(t, []string{"self-consume", "direct-consume", "direct-login"}, []string{logs[0].Content, logs[1].Content, logs[2].Content})
	for _, log := range logs {
		assert.NotEqual(t, LogTypeManage, log.Type)
		assert.Empty(t, log.ChannelName)
	}

	var directConsume *Log
	for _, log := range logs {
		if log.Content == "direct-consume" {
			directConsume = log
			break
		}
	}
	require.NotNil(t, directConsume)
	other, err := common.StrToMap(directConsume.Other)
	require.NoError(t, err)
	assert.NotContains(t, other, "admin_info")
	assert.NotContains(t, other, "audit_info")
	assert.Equal(t, "password", other["login_method"])

	directLogs, directTotal, err := GetUserLogs(scope, LogTypeUnknown, 0, 0, "", "direct", "", 0, 100, "", "", "", true)
	require.NoError(t, err)
	require.Equal(t, int64(2), directTotal)
	for _, log := range directLogs {
		assert.Equal(t, "direct", log.Username)
	}

	outsideLogs, outsideTotal, err := GetUserLogs(scope, LogTypeUnknown, 0, 0, "", "unrelated", "", 0, 100, "", "", "", true)
	require.NoError(t, err)
	assert.Zero(t, outsideTotal)
	assert.Empty(t, outsideLogs)

	manageLogs, manageTotal, err := GetUserLogs(scope, LogTypeManage, 0, 0, "", "", "", 0, 100, "", "", "", true)
	require.NoError(t, err)
	assert.Zero(t, manageTotal)
	assert.Empty(t, manageLogs)

	normalScope, err := GetUserLogScopeIDs(normalLogUserID)
	require.NoError(t, err)
	normalLogs, normalTotal, err := GetUserLogs(normalScope, LogTypeUnknown, 0, 0, "", "", "", 0, 100, "", "", "", true)
	require.NoError(t, err)
	require.Equal(t, int64(1), normalTotal)
	require.Len(t, normalLogs, 1)
	assert.Equal(t, "normal-consume", normalLogs[0].Content)
}

func TestSumUsedQuotaAppliesBusinessScopeToAllStatistics(t *testing.T) {
	truncateTables(t)
	seedBusinessLogUsers(t)

	subTags := operation_setting.GetSubChannelTags()
	require.NotEmpty(t, subTags)
	subTag := subTags[0]
	require.NoError(t, DB.Create(&[]Channel{
		{Id: 501, Type: 1, Key: "sub-key", Name: "sub", Tag: &subTag},
		{Id: 502, Type: 1, Key: "other-key", Name: "other"},
	}).Error)

	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&[]Log{
		{Id: 11, UserId: businessLogUserID, Username: "business", Type: LogTypeConsume, ModelName: "model-a", TokenName: "token-a", Group: "vip", ChannelId: 501, Quota: 100, PromptTokens: 10, CompletionTokens: 5, CreatedAt: now, Other: `{"cache_tokens":7}`},
		{Id: 12, UserId: businessLogDirectID, Username: "direct", Type: LogTypeConsume, ModelName: "model-a", TokenName: "token-a", Group: "vip", ChannelId: 501, Quota: 200, PromptTokens: 20, CompletionTokens: 10, CreatedAt: now, Other: `{"cache_tokens":9}`},
		{Id: 13, UserId: businessLogDirectID, Username: "direct", Type: LogTypeConsume, ModelName: "model-a", TokenName: "token-a", Group: "vip", ChannelId: 502, Quota: 50, PromptTokens: 4, CompletionTokens: 5, CreatedAt: now, Other: `{}`},
		{Id: 14, UserId: businessLogDirectID, Username: "direct", Type: LogTypeConsume, ModelName: "model-b", TokenName: "token-b", Group: "default", ChannelId: 502, Quota: 25, PromptTokens: 3, CompletionTokens: 3, CreatedAt: now, Other: `{}`},
		{Id: 15, UserId: businessLogNestedID, Username: "nested", Type: LogTypeConsume, ChannelId: 501, Quota: 400, PromptTokens: 20, CompletionTokens: 20, CreatedAt: now, Other: `{}`},
		{Id: 16, UserId: businessLogUnrelatedID, Username: "unrelated", Type: LogTypeConsume, ChannelId: 501, Quota: 800, PromptTokens: 40, CompletionTokens: 40, CreatedAt: now, Other: `{}`},
		{Id: 17, UserId: businessLogDirectID, Username: "direct", Type: LogTypeConsume, ChannelId: 501, Quota: 70, PromptTokens: 7, CompletionTokens: 7, CreatedAt: now - 120, Other: `{}`},
		{Id: 18, UserId: normalLogUserID, Username: "normal", Type: LogTypeConsume, ChannelId: 502, Quota: 60, PromptTokens: 2, CompletionTokens: 3, CreatedAt: now, Other: `{}`},
		{Id: 19, UserId: normalLogDirectInviteID, Username: "normal-direct", Type: LogTypeConsume, ChannelId: 502, Quota: 600, PromptTokens: 20, CompletionTokens: 30, CreatedAt: now, Other: `{}`},
	}).Error)

	scope, err := GetUserLogScopeIDs(businessLogUserID)
	require.NoError(t, err)

	stat, err := SumUsedQuota(scope, LogTypeUnknown, now-10, now+10, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 375, stat.Quota)
	assert.Equal(t, 300, stat.SubQuota)
	assert.Equal(t, int64(45), stat.SubTokens)
	assert.Equal(t, 4, stat.Rpm)
	assert.Equal(t, 60, stat.Tpm)

	directStat, err := SumUsedQuota(scope, LogTypeUnknown, now-10, now+10, "", "direct", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 275, directStat.Quota)
	assert.Equal(t, 200, directStat.SubQuota)
	assert.Equal(t, int64(30), directStat.SubTokens)
	assert.Equal(t, 3, directStat.Rpm)
	assert.Equal(t, 45, directStat.Tpm)

	filteredStat, err := SumUsedQuota(scope, LogTypeUnknown, now-10, now+10, "model-a", "", "token-a", 0, "vip")
	require.NoError(t, err)
	assert.Equal(t, 350, filteredStat.Quota)
	assert.Equal(t, 300, filteredStat.SubQuota)
	assert.Equal(t, int64(45), filteredStat.SubTokens)
	assert.Equal(t, 3, filteredStat.Rpm)
	assert.Equal(t, 54, filteredStat.Tpm)

	outsideStat, err := SumUsedQuota(scope, LogTypeUnknown, now-10, now+10, "", "unrelated", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, Stat{}, outsideStat)

	normalScope, err := GetUserLogScopeIDs(normalLogUserID)
	require.NoError(t, err)
	normalStat, err := SumUsedQuota(normalScope, LogTypeUnknown, now-10, now+10, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 60, normalStat.Quota)
	assert.Zero(t, normalStat.SubQuota)
	assert.Zero(t, normalStat.SubTokens)
	assert.Equal(t, 1, normalStat.Rpm)
	assert.Equal(t, 5, normalStat.Tpm)
}
