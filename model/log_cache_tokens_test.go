package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Claude 语义请求的 other：含 usage_semantic + cache_tokens（读）+ cache_write_tokens（写）
const claudeOtherModern = `{"claude":true,"usage_semantic":"anthropic","cache_tokens":9506,"cache_write_tokens":28088,"cache_creation_tokens":28088,"cache_creation_tokens_5m":28088}`

// 老数据：无 cache_write_tokens，只有 cache_creation_tokens（无 5m/1h 拆分）
const claudeOtherLegacy = `{"claude":true,"usage_semantic":"anthropic","cache_tokens":9506,"cache_creation_tokens":28088}`

// OpenAI 语义请求的 other：有 cache_tokens 但只是明细拆分，prompt_tokens 已含缓存，绝不能重复加
const openaiOther = `{"cache_tokens":167680,"usage_semantic":"openai","billing_mode":"tiered_expr"}`

func TestClaudeCacheTokensFromOther_Modern(t *testing.T) {
	assert.Equal(t, int64(9506+28088), ClaudeCacheTokensFromOther(claudeOtherModern))
}

func TestClaudeCacheTokensFromOther_LegacyFallback(t *testing.T) {
	// 无 cache_write_tokens 时回退到 cache_creation_tokens
	assert.Equal(t, int64(9506+28088), ClaudeCacheTokensFromOther(claudeOtherLegacy))
}

func TestClaudeCacheTokensFromOther_OpenAICacheTokensIgnored(t *testing.T) {
	// OpenAI 请求即使有 cache_tokens 也返回 0，避免与已含缓存的 prompt_tokens 重复
	assert.Equal(t, int64(0), ClaudeCacheTokensFromOther(openaiOther))
}

func TestClaudeCacheTokensFromOther_SplitTakesMaxOfSum(t *testing.T) {
	// 5m+1h 拆分之和大于 cache_creation_tokens 时取拆分之和
	other := `{"claude":true,"cache_tokens":100,"cache_creation_tokens":1000,"cache_creation_tokens_5m":600,"cache_creation_tokens_1h":500}`
	assert.Equal(t, int64(100+1100), ClaudeCacheTokensFromOther(other))
}

func TestClaudeCacheTokensFromOther_CreationLargerThanSplit(t *testing.T) {
	// cache_creation_tokens 大于 5m+1h 之和时取 cache_creation_tokens
	other := `{"claude":true,"cache_tokens":100,"cache_creation_tokens":2000,"cache_creation_tokens_5m":600,"cache_creation_tokens_1h":500}`
	assert.Equal(t, int64(100+2000), ClaudeCacheTokensFromOther(other))
}

func TestClaudeCacheTokensFromOther_OnlyClaudeMarker(t *testing.T) {
	// 老数据可能只有 claude:true 没有 usage_semantic，仍应识别为 Claude
	other := `{"claude":true,"cache_tokens":30,"cache_write_tokens":50}`
	assert.Equal(t, int64(80), ClaudeCacheTokensFromOther(other))
}

func TestClaudeCacheTokensFromOther_EmptyAndGarbage(t *testing.T) {
	assert.Equal(t, int64(0), ClaudeCacheTokensFromOther(""))
	assert.Equal(t, int64(0), ClaudeCacheTokensFromOther("not-json"))
}

func TestClaudeCacheTokensFromOther_NegativeClamped(t *testing.T) {
	other := `{"claude":true,"cache_tokens":-5,"cache_write_tokens":-10}`
	assert.Equal(t, int64(0), ClaudeCacheTokensFromOther(other))
}

// ---------------------------------------------------------------------------
// sumLogsByTimeRange / SumClaudeCacheTokensByUsers 集成测试（内存 SQLite）
// ---------------------------------------------------------------------------

func insertConsumeLog(t *testing.T, userID int, createdAt int64, prompt, completion int, other string) {
	t.Helper()
	require.NoError(t, DB.Create(&Log{
		UserId:           userID,
		CreatedAt:        createdAt,
		Type:             LogTypeConsume,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		Other:            other,
	}).Error)
}

func TestSumLogsByTimeRange_ClaudeCacheAddedOpenAIUnchanged(t *testing.T) {
	truncateTables(t)

	const dayStart = 1786086000                                       // 2026-08-07 07:00 UTC 附近
	insertConsumeLog(t, 140, dayStart, 1, 241, claudeOtherModern)     // Claude：补 9506+28088
	insertConsumeLog(t, 66, dayStart, 169227, 145, openaiOther)       // OpenAI：不补
	insertConsumeLog(t, 140, dayStart, 10, 20, claudeOtherLegacy)     // 老数据 fallback：补 9506+28088
	insertConsumeLog(t, 999, dayStart-86400, 5, 5, claudeOtherModern) // 窗口外：不计入

	agg, err := sumLogsByTimeRange(dayStart, dayStart+86399)
	require.NoError(t, err)

	// Claude 用户：基础 tokens + 缓存补加
	assert.Equal(t, int64((1+241+9506+28088)+(10+20+9506+28088)), agg[140].Tokens)
	// OpenAI 用户：基础 tokens，不补 cache（prompt_tokens 已含）
	assert.Equal(t, int64(169227+145), agg[66].Tokens)
	// 窗口外用户不存在
	_, ok := agg[999]
	assert.False(t, ok)
}

func TestSumClaudeCacheTokensByUsers_UserFilter(t *testing.T) {
	truncateTables(t)

	const dayStart = 1786086000
	insertConsumeLog(t, 140, dayStart, 1, 241, claudeOtherModern)
	insertConsumeLog(t, 66, dayStart, 5, 5, claudeOtherLegacy)
	insertConsumeLog(t, 66, dayStart, 100, 100, openaiOther) // OpenAI 行应被过滤

	// 全部用户
	all, err := SumClaudeCacheTokensByUsers(dayStart, dayStart+86399, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(9506+28088), all[140])
	assert.Equal(t, int64(9506+28088), all[66], "OpenAI 行不应累加 cache")

	// 只看 user 140
	subset, err := SumClaudeCacheTokensByUsers(dayStart, dayStart+86399, []int{140})
	require.NoError(t, err)
	assert.Equal(t, int64(9506+28088), subset[140])
	_, ok := subset[66]
	assert.False(t, ok)

	// 时间窗口外
	out, err := SumClaudeCacheTokensByUsers(dayStart+86400, dayStart+2*86400, nil)
	require.NoError(t, err)
	assert.Empty(t, out)
}
