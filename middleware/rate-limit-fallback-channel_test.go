package middleware

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 速率超限兜底改走「请求所在渠道自配的 FallbackChannelId」（规格 §22）。
//
// ⛔ 这一组的核心诉求：平台同时有 Gemini 与 OpenAI 渠道，兜底必须与该请求的
// 模型/路径对得上，绝不能把 Gemini 请求送进只认 OpenAI 的渠道。
// 下面每条用例都对应一条 429 条件：缺配置、禁用、模型不支持、自环。

func setupChannelFallbackTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	prevDB := model.DB
	prevType := common.MainDatabaseType()
	prevMem := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		model.DB = prevDB
		common.SetMainDatabaseType(prevType)
		common.MemoryCacheEnabled = prevMem
	})
	return db
}

func mustCreateChannel(t *testing.T, db *gorm.DB, id int, name, models, group string, status, fallbackID int) {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{
		Id:                id,
		Name:              name,
		Key:               "sk-test",
		Status:            status,
		Models:            models,
		Group:             group,
		FallbackChannelId: fallbackID,
	}).Error)
}

func newFallbackTestContext(t *testing.T) *gin.Context {
	t.Helper()
	c, _ := newDistillTestContext(http.MethodPost, "/v1/messages", `{}`)
	return c
}

func TestRateLimitFallbackHopsToTheChannelsOwnFallback(t *testing.T) {
	db := setupChannelFallbackTestDB(t)
	// 1: 主体渠道，claude 模型，兜底指向 2
	mustCreateChannel(t, db, 1, "anthropic-main", "claude-3-5-sonnet", "default", common.ChannelStatusEnabled, 2)
	// 2: 兜底渠道，同一模型；自己也配了兜底 3（用来验证不会跟第二跳）
	mustCreateChannel(t, db, 2, "anthropic-backup", "claude-3-5-sonnet", "default", common.ChannelStatusEnabled, 3)
	mustCreateChannel(t, db, 3, "the-third-hop", "claude-3-5-sonnet", "default", common.ChannelStatusEnabled, 0)
	model.InitChannelCache()

	c := newFallbackTestContext(t)
	origin, err := model.CacheGetChannel(1)
	require.NoError(t, err)

	fb := redirectToFallbackChannel(c, origin, "claude-3-5-sonnet", "default")
	require.NotNil(t, fb, "a configured, enabled, model-compatible fallback must be used")
	require.Equal(t, 2, fb.channel.Id, "must hop to the channel's own fallback")
	require.Equal(t, "default", fb.group, "billing must stay on the original group")

	// ⛔ 只跳一跳：兜底渠道自己再配了兜底也不能跟过去。
	require.NotEqual(t, 3, fb.channel.Id, "the fallback channel's own fallback must not be followed")
}

func TestRateLimitFallbackRequiresAConfiguredFallback(t *testing.T) {
	db := setupChannelFallbackTestDB(t)
	mustCreateChannel(t, db, 11, "no-fallback", "claude-3-5-sonnet", "default", common.ChannelStatusEnabled, 0)
	model.InitChannelCache()

	c := newFallbackTestContext(t)
	origin, err := model.CacheGetChannel(11)
	require.NoError(t, err)

	require.Nil(t, redirectToFallbackChannel(c, origin, "claude-3-5-sonnet", "default"),
		"no configured fallback must resolve to nil so the caller can 429")
}

func TestRateLimitFallbackRejectsADisabledFallback(t *testing.T) {
	db := setupChannelFallbackTestDB(t)
	mustCreateChannel(t, db, 21, "main", "claude-3-5-sonnet", "default", common.ChannelStatusEnabled, 22)
	mustCreateChannel(t, db, 22, "disabled-backup", "claude-3-5-sonnet", "default", common.ChannelStatusManuallyDisabled, 0)
	model.InitChannelCache()

	c := newFallbackTestContext(t)
	origin, err := model.CacheGetChannel(21)
	require.NoError(t, err)

	require.Nil(t, redirectToFallbackChannel(c, origin, "claude-3-5-sonnet", "default"),
		"a disabled fallback must be treated as unusable")
}

// ⭐ 跨厂商必须挡住：Gemini 请求不能落进只认 OpenAI 模型的兜底渠道。
func TestRateLimitFallbackDoesNotCrossVendors(t *testing.T) {
	db := setupChannelFallbackTestDB(t)
	mustCreateChannel(t, db, 31, "gemini-main", "gemini-2.5-pro", "default", common.ChannelStatusEnabled, 32)
	mustCreateChannel(t, db, 32, "openai-only-backup", "gpt-4o", "default", common.ChannelStatusEnabled, 0)
	model.InitChannelCache()

	c := newFallbackTestContext(t)
	origin, err := model.CacheGetChannel(31)
	require.NoError(t, err)

	require.Nil(t, redirectToFallbackChannel(c, origin, "gemini-2.5-pro", "default"),
		"a fallback that does not serve the requested model must never be used")
}

func TestRateLimitFallbackRejectsASelfLoop(t *testing.T) {
	db := setupChannelFallbackTestDB(t)
	// 兜底指向自己：留在原地等于没兜底，必须判为不可用。
	mustCreateChannel(t, db, 41, "self-loop", "claude-3-5-sonnet", "default", common.ChannelStatusEnabled, 41)
	model.InitChannelCache()

	c := newFallbackTestContext(t)
	origin, err := model.CacheGetChannel(41)
	require.NoError(t, err)

	require.Nil(t, redirectToFallbackChannel(c, origin, "claude-3-5-sonnet", "default"),
		"a fallback pointing at itself must be treated as unusable")
}
