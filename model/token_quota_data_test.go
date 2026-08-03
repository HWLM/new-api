package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestLogTokenQuotaDataUpsert 验证同步 upsert 的三条不变式：
//  1. 分桶键取服务器本地时区当日 0 点（跨 UTC 边界的 8:00 前后落到同一桶）
//  2. 命中已有桶时 count/quota/token_used 增量累加
//  3. 空 token_name/group 不覆盖已有值；tokenId <= 0 直接跳过
func TestLogTokenQuotaDataUpsert(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TokenQuotaData{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&TokenQuotaData{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&TokenQuotaData{}).Error)
	})

	originalLocation := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	time.Local = location
	t.Cleanup(func() {
		time.Local = originalLocation
	})

	beforeEightAM := time.Date(2026, time.July, 30, 7, 59, 0, 0, location).Unix()
	afterEightAM := time.Date(2026, time.July, 30, 8, 0, 0, 0, location).Unix()
	expectedDayStart := time.Date(2026, time.July, 30, 0, 0, 0, 0, location).Unix()

	LogTokenQuotaData(1, 2, "kratos", "default", 100, beforeEightAM, 10)
	LogTokenQuotaData(1, 2, "kratos", "default", 200, afterEightAM, 20)

	var rows []TokenQuotaData
	require.NoError(t, DB.Where("user_id = ? AND token_id = ?", 1, 2).Find(&rows).Error)
	require.Len(t, rows, 1, "两条同日调用应合并为同一桶")
	first := rows[0]
	assert.Equal(t, expectedDayStart, first.CreatedAt)
	assert.Equal(t, 2, first.Count)
	assert.Equal(t, 300, first.Quota)
	assert.Equal(t, 30, first.TokenUsed)
	assert.Equal(t, "kratos", first.TokenName)
	assert.Equal(t, "default", first.GroupName)

	// 下一天开新桶
	nextDay := time.Date(2026, time.July, 31, 10, 0, 0, 0, location).Unix()
	LogTokenQuotaData(1, 2, "kratos", "default", 500, nextDay, 50)

	require.NoError(t, DB.Where("user_id = ? AND token_id = ?", 1, 2).Order("created_at ASC").Find(&rows).Error)
	require.Len(t, rows, 2, "跨自然日应新开一桶")
	assert.Equal(t, expectedDayStart, rows[0].CreatedAt)
	assert.Equal(t, time.Date(2026, time.July, 31, 0, 0, 0, 0, location).Unix(), rows[1].CreatedAt)
	assert.Equal(t, 1, rows[1].Count)
	assert.Equal(t, 500, rows[1].Quota)

	// 空 token_name/group 不覆盖已有值，但计数仍累加
	LogTokenQuotaData(1, 2, "", "", 50, afterEightAM, 5)
	var refreshed TokenQuotaData
	require.NoError(t, DB.Where("user_id = ? AND token_id = ? AND created_at = ?", 1, 2, expectedDayStart).First(&refreshed).Error)
	assert.Equal(t, "kratos", refreshed.TokenName, "空 token_name 不应覆盖已有值")
	assert.Equal(t, "default", refreshed.GroupName, "空 group 不应覆盖已有值")
	assert.Equal(t, 3, refreshed.Count)
	assert.Equal(t, 350, refreshed.Quota)
	assert.Equal(t, 35, refreshed.TokenUsed)

	// tokenId <= 0 直接跳过，不新增行
	var before int64
	require.NoError(t, DB.Model(&TokenQuotaData{}).Count(&before).Error)
	LogTokenQuotaData(1, 0, "kratos", "default", 100, afterEightAM, 10)
	LogTokenQuotaData(1, -1, "kratos", "default", 100, afterEightAM, 10)
	var after int64
	require.NoError(t, DB.Model(&TokenQuotaData{}).Count(&after).Error)
	assert.Equal(t, before, after, "tokenId<=0 不应写入")
}

// TestLogTokenQuotaDataUniqueConstraint 验证 (user_id, token_id, created_at)
// 存在 unique 索引：绕开 upsert 的直插重复行会被数据库拒绝，
// 从根本上阻止同一分桶产生多行、避免查询多计。
func TestLogTokenQuotaDataUniqueConstraint(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TokenQuotaData{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&TokenQuotaData{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&TokenQuotaData{}).Error)
	})

	base := &TokenQuotaData{UserID: 42, TokenID: 7, CreatedAt: 1785427200, Count: 1, Quota: 10, TokenUsed: 1}
	require.NoError(t, DB.Create(base).Error)

	dup := &TokenQuotaData{UserID: 42, TokenID: 7, CreatedAt: 1785427200, Count: 1, Quota: 20, TokenUsed: 2}
	err := DB.Create(dup).Error
	require.Error(t, err, "重复的 (user_id, token_id, created_at) 应触发 unique 约束")
}

func TestTokenQuotaDataRecordedWhenDataExportDisabled(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TokenQuotaData{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&TokenQuotaData{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&TokenQuotaData{}).Error)
	})

	originalDataExportEnabled := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() {
		common.DataExportEnabled = originalDataExportEnabled
	})

	ctx, _ := gin.CreateTestContext(nil)
	ctx.Set("username", "token-stats-test")
	RecordConsumeLog(ctx, 101, RecordConsumeLogParams{
		TokenId:          201,
		TokenName:        "sync-consume",
		Group:            "default",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
	})
	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId:  102,
		TokenId: 202,
		Group:   "default",
		Quota:   200,
		LogType: LogTypeConsume,
	})

	var rows []TokenQuotaData
	require.NoError(t, DB.Order("token_id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, 201, rows[0].TokenID)
	assert.Equal(t, 100, rows[0].Quota)
	assert.Equal(t, 30, rows[0].TokenUsed)
	assert.Equal(t, 202, rows[1].TokenID)
	assert.Equal(t, 200, rows[1].Quota)
	assert.Zero(t, rows[1].TokenUsed)
}
