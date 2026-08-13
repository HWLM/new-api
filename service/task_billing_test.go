package service

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&model.Task{},
		&model.User{},
		&model.Token{},
		&model.Log{},
		&model.Channel{},
		&model.TopUp{},
		&model.UserSubscription{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: quota, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedBilledUser(t *testing.T, id int, quota int, usedQuota int) {
	t.Helper()
	user := &model.User{
		Id:        id,
		Username:  "test_user",
		Quota:     quota,
		UsedQuota: usedQuota,
		Status:    common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedSubscription(t *testing.T, id int, userId int, amountTotal int64, amountUsed int64) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:          id,
		UserId:      userId,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

func TestTaskExprMatchedParamsPreservesReferencedConditionValues(t *testing.T) {
	vars := &model.TaskExprVars{
		Seconds:    8,
		Resolution: "1080p",
		HasVideo:   false,
		HasImage:   true,
		N:          1,
	}

	params := taskExprMatchedParams(
		vars,
		`v2:resolution == "1080p" && !has_video ? tier("matched", 0.1 * seconds) : tier("fallback", 0)`,
	)

	assert.Equal(t, map[string]interface{}{
		"seconds":    8.0,
		"resolution": "1080p",
		"has_video":  false,
	}, params)
	assert.NotContains(t, params, "has_image")
	assert.NotContains(t, params, "n")
}

// ---------------------------------------------------------------------------
// Read-back helpers
// ---------------------------------------------------------------------------

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getUserUsedQuota(t *testing.T, id int) int {
	t.Helper()
	quota, err := model.GetUserUsedQuota(id)
	require.NoError(t, err)
	return quota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return token.UsedQuota
}

func getSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func getTaskQuota(t *testing.T, id int64) int {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&task).Error)
	return task.Quota
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

func TestBillingSessionWalletSnapshotCoversPreConsumeAndSettlement(t *testing.T) {
	truncate(t)
	const userID = 1001
	seedUser(t, userID, 10000)

	ctx, _ := gin.CreateTestContext(nil)
	relayInfo := &relaycommon.RelayInfo{
		UserId:       userID,
		IsPlayground: true,
	}
	session, apiErr := NewBillingSession(ctx, relayInfo, 3000)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.NoError(t, session.Settle(2000))

	require.NotNil(t, relayInfo.UserQuotaBefore)
	require.NotNil(t, relayInfo.UserQuotaAfter)
	assert.Equal(t, int64(10000), *relayInfo.UserQuotaBefore)
	assert.Equal(t, int64(8000), *relayInfo.UserQuotaAfter)
	assert.Equal(t, 8000, getUserQuota(t, userID))
}

// ===========================================================================
// RefundTaskQuota tests
// ===========================================================================

func TestRefundTaskQuota_Wallet(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1, 1, 1
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedBilledUser(t, userID, initQuota, preConsumed)
	seedToken(t, tokenID, userID, "sk-test-key", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, task.Insert())

	assert.True(t, RefundTaskQuota(ctx, task, "task failed: upstream error"))

	// User quota should increase by preConsumed
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Zero(t, getUserUsedQuota(t, userID))
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID)+getUserUsedQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, -preConsumed, getTokenUsedQuota(t, tokenID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)
	require.NotNil(t, log.BeforeQuota)
	require.NotNil(t, log.AfterQuota)
	assert.Equal(t, int64(initQuota), *log.BeforeQuota)
	assert.Equal(t, int64(initQuota+preConsumed), *log.AfterQuota)
	assert.Zero(t, task.Quota)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_Subscription(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 2, 2, 2, 1
	const preConsumed = 2000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedBilledUser(t, userID, 0, preConsumed)
	seedToken(t, tokenID, userID, "sk-sub-key", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	require.NoError(t, task.Insert())

	assert.True(t, RefundTaskQuota(ctx, task, "subscription task failed"))

	// Subscription used should decrease by preConsumed
	assert.Equal(t, subUsed-int64(preConsumed), getSubscriptionUsed(t, subID))
	assert.Zero(t, getUserUsedQuota(t, userID))

	// Token should also be refunded
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_ZeroQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 3
	seedUser(t, userID, 5000)

	task := makeTask(userID, 0, 0, 0, BillingSourceWallet, 0)
	require.NoError(t, task.Insert())

	assert.True(t, RefundTaskQuota(ctx, task, "zero quota task"))

	// No change to user quota
	assert.Equal(t, 5000, getUserQuota(t, userID))

	// No log created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_NoToken(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 4, 4
	const initQuota, preConsumed = 10000, 1500

	seedBilledUser(t, userID, initQuota, preConsumed)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0) // TokenId=0
	require.NoError(t, task.Insert())

	assert.True(t, RefundTaskQuota(ctx, task, "no token task failed"))

	// User quota refunded
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Zero(t, getUserUsedQuota(t, userID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_FundingFailureKeepsPendingMarker(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, preConsumed = 5, 1200
	seedUser(t, userID, 5000)
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceSubscription, 9999)
	task.Status = model.TaskStatusFailure
	require.NoError(t, model.DB.Create(task).Error)

	assert.False(t, RefundTaskQuota(ctx, task, "subscription missing"))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, preConsumed, getTaskQuota(t, task.ID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_ConcurrentCallsRefundOnce(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 5, 5, 5
	const initQuota, preConsumed = 10000, 2500
	const tokenRemain = 6000

	seedBilledUser(t, userID, initQuota, preConsumed)
	seedToken(t, tokenID, userID, "sk-refund-once", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, task.Insert())

	var first, second model.Task
	require.NoError(t, model.DB.First(&first, task.ID).Error)
	require.NoError(t, model.DB.First(&second, task.ID).Error)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		RefundTaskQuota(ctx, &first, "concurrent upstream failure")
	}()
	go func() {
		defer wg.Done()
		RefundTaskQuota(ctx, &second, "concurrent upstream failure")
	}()
	wg.Wait()

	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Zero(t, getUserUsedQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, -preConsumed, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, int64(1), countLogs(t))

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.Zero(t, stored.Quota)
}

func TestRefundTaskQuota_FundingFailureRestoresClaim(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 6, 6
	const preConsumed = 1800

	seedUser(t, userID, 0)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceSubscription, 999)
	require.NoError(t, task.Insert())

	RefundTaskQuota(ctx, task, "subscription lookup failed")

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.Equal(t, preConsumed, stored.Quota)
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

// ===========================================================================
// RecalculateTaskQuota tests
// ===========================================================================

func TestRecalculate_PositiveDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 10, 10, 10
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // under-charged by 1000
	const tokenRemain = 5000

	seedBilledUser(t, userID, initQuota, preConsumed)
	seedToken(t, tokenID, userID, "sk-recalc-pos", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuotaWithUsage(ctx, task, actualQuota, "adaptor adjustment", TaskBillingUsage{
		TotalTokens:      1200,
		CompletionTokens: 200,
	})

	// User quota should decrease by the delta (1000 additional charge)
	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))
	assert.Equal(t, actualQuota, getUserUsedQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.EqualValues(t, 1200, other["task_total_tokens"])
	assert.EqualValues(t, 200, other["task_completion_tokens"])
	assert.EqualValues(t, preConsumed, other["pre_consumed_quota"])
	assert.EqualValues(t, actualQuota, other["actual_quota"])
}

func TestRecalculate_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 11, 11, 11
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged by 2000
	const tokenRemain = 5000

	seedBilledUser(t, userID, initQuota, preConsumed)
	seedToken(t, tokenID, userID, "sk-recalc-neg", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, actualQuota, getUserUsedQuota(t, userID))
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID)+getUserUsedQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
	require.NotNil(t, log.BeforeQuota)
	require.NotNil(t, log.AfterQuota)
	assert.Equal(t, int64(initQuota), *log.BeforeQuota)
	assert.Equal(t, int64(initQuota+preConsumed-actualQuota), *log.AfterQuota)
}

func TestRecalculateTaskQuotaByTokensUsesFrozenGroupRatio(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalGroupGroupRatio := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalGroupGroupRatio))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":2}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"billing-group":0.5}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))

	const userID, tokenID, channelID = 12, 12, 12
	const initialQuota, preConsumedQuota, tokenRemainQuota = 10000, 1000, 10000
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, "sk-frozen-group-ratio", tokenRemainQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumedQuota, tokenID, BillingSourceWallet, 0)
	task.Group = "billing-group"
	task.PrivateData.BillingContext.GroupRatio = 0.75

	RecalculateTaskQuotaByTokens(ctx, task, 1000)

	const actualQuota = 1500 // 1000 tokens * model ratio 2 * frozen group ratio 0.75
	assert.Equal(t, actualQuota, task.Quota)
	assert.Equal(t, initialQuota-(actualQuota-preConsumedQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemainQuota-(actualQuota-preConsumedQuota), getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Contains(t, log.Content, "groupRatio=0.75")
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, 0.75, other["group_ratio"])
}

func TestRecalculateTaskQuotaByTokensLegacyTaskUsesCurrentGroupRatio(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	originalModelRatio := ratio_setting.ModelRatio2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalGroupGroupRatio := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatio))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalGroupGroupRatio))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":2}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"billing-group":0.5}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))

	const userID, tokenID, channelID = 13, 13, 13
	const initialQuota, preConsumedQuota, tokenRemainQuota = 10000, 1500, 10000
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, "sk-legacy-group-ratio", tokenRemainQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumedQuota, tokenID, BillingSourceWallet, 0)
	task.Group = "billing-group"
	task.PrivateData.BillingContext = nil

	RecalculateTaskQuotaByTokens(ctx, task, 1000)

	const actualQuota = 1000 // 1000 tokens * model ratio 2 * current group ratio 0.5
	assert.Equal(t, actualQuota, task.Quota)
	assert.Equal(t, initialQuota+(preConsumedQuota-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemainQuota+(preConsumedQuota-actualQuota), getTokenRemainQuota(t, tokenID))
}

func TestRecalculate_ZeroDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 12
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, preConsumed, "exact match")

	// No change to user quota
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No log created (delta is zero)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_ActualQuotaZero(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 13
	const initQuota = 10000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, 5000, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, 0, "zero actual")

	// No change (early return)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_Subscription_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 14, 14, 14, 2
	const preConsumed = 5000
	const actualQuota = 2000 // over-charged by 3000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedBilledUser(t, userID, 0, preConsumed)
	seedToken(t, tokenID, userID, "sk-sub-recalc", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	RecalculateTaskQuota(ctx, task, actualQuota, "subscription over-charge")

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))
	assert.Equal(t, actualQuota, getUserUsedQuota(t, userID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// CAS + Billing integration tests
// Simulates the flow in updateVideoSingleTask (service/task_polling.go)
// ===========================================================================

// simulatePollBilling reproduces the CAS + billing logic from updateVideoSingleTask.
// It takes a persisted task (already in DB), applies the new status, and performs
// the conditional update + billing exactly as the polling loop does.
func simulatePollBilling(ctx context.Context, task *model.Task, newStatus model.TaskStatus, actualQuota int) {
	snap := task.Snapshot()

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = newStatus
	switch string(newStatus) {
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		task.FinishTime = 9999
		shouldSettle = true
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = 9999
		task.FailReason = "upstream error"
		if quota != 0 {
			shouldRefund = true
		}
	default:
		task.Progress = "50%"
	}

	isDone := task.Status == model.TaskStatus(model.TaskStatusSuccess) || task.Status == model.TaskStatus(model.TaskStatusFailure)
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	if shouldSettle && actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "test settle")
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}
}

func TestCASGuardedRefund_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-win", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS wins: task in DB should now be FAILURE
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Zero(t, reloaded.Quota)

	// Refund should have happened
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestCASGuardedRefund_Lose(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-lose", tokenRemain)
	seedChannel(t, channelID)

	// Create task with IN_PROGRESS in DB
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate another process already transitioning to FAILURE
	model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailure)

	// Our process still has the old in-memory state (IN_PROGRESS) and tries to transition
	// task.Status is still IN_PROGRESS in the snapshot
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS lost: user quota should NOT change (no double refund)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))

	// No billing log should be created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestCASGuardedSettle_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged, should get partial refund
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-settle-win", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusSuccess), actualQuota)

	// CAS wins: task should be SUCCESS
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)

	// Settlement should refund the over-charge (5000 - 3000 = 2000 back to user)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)
}

func TestNonTerminalUpdate_NoBilling(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 23, 23
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	task.Progress = "20%"
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate a non-terminal poll update (still IN_PROGRESS, progress changed)
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusInProgress), 0)

	// User quota should NOT change
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No billing log
	assert.Equal(t, int64(0), countLogs(t))

	// Task progress should be updated in DB
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "50%", reloaded.Progress)
}

// ===========================================================================
// Mock adaptor for settleTaskBillingOnComplete tests
// ===========================================================================

type mockAdaptor struct {
	adjustReturn int
}

func (m *mockAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (m *mockAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}
func (m *mockAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) { return nil, nil }
func (m *mockAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return m.adjustReturn
}

// ===========================================================================
// PerCallBilling tests — settleTaskBillingOnComplete
// ===========================================================================

func TestSettle_PerCallBilling_SkipsAdaptorAdjust(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-adaptor", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 2000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no adjustment despite adaptor returning 2000
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_PerCallBilling_SkipsTotalTokens(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 31, 31, 31
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-tokens", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no recalculation by tokens
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_NonPerCallBilling_AppliesAdaptorAdjustment(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 32, 32, 32
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-adj", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// PerCallBilling defaults to false

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Non-per-call: adaptor adjustment applies (refund 2000)
	assert.Equal(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// TaskPollingRatiosAdjuster tests — settleTaskBillingOnComplete 新分支
// ===========================================================================

// mockRatiosAdjuster 同时实现 TaskPollingAdaptor + TaskPollingRatiosAdjuster。
// 用来验证：结算前会调用 AdjustBillingRatiosOnComplete，用返回的 map 覆盖
// BillingContext.OtherRatios 并持久化到 DB。
type mockRatiosAdjuster struct {
	mockAdaptor
	newRatios map[string]float64
}

func (m *mockRatiosAdjuster) AdjustBillingRatiosOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) map[string]float64 {
	return m.newRatios
}

// TestSettle_RatiosAdjuster_OverridesAndPersists 覆盖典型正向路径：
// adjuster 返回新 ratios → BillingContext.OtherRatios 被替换 → 写回 DB。
// AdjustBillingOnComplete 返回 0 & TotalTokens=0 → settle 走完覆盖分支后
// 不再重算 quota（只验证 ratios 覆盖本身）。
func TestSettle_RatiosAdjuster_OverridesAndPersists(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 40, 40, 40
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-ratios-adj", 10000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 5000, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.OtherRatios = map[string]float64{
		"video_input":       1.0,
		"duration_estimate": 3.0,
	}
	// task 必须先 Insert 才能被 UpdatePrivateData 写回
	require.NoError(t, task.Insert())

	adaptor := &mockRatiosAdjuster{
		newRatios: map[string]float64{"video_input": 51.0 / 46.0},
	}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// 内存中的 OtherRatios 被替换
	assert.Equal(t, map[string]float64{"video_input": 51.0 / 46.0}, task.PrivateData.BillingContext.OtherRatios)

	// DB 中也已持久化
	var reloaded model.Task
	require.NoError(t, model.DB.Where("id = ?", task.ID).First(&reloaded).Error)
	require.NotNil(t, reloaded.PrivateData.BillingContext)
	assert.InDelta(t, 51.0/46.0, reloaded.PrivateData.BillingContext.OtherRatios["video_input"], 1e-6)
	assert.NotContains(t, reloaded.PrivateData.BillingContext.OtherRatios, "duration_estimate", "duration_estimate 应被替换掉")
}

// TestSettle_RatiosAdjuster_FiltersInvalidValues 验证覆盖时会过滤 ≤0/NaN/±Inf。
// 沿用 PriceData.AddOtherRatio 的过滤规则，避免负价或 NaN 传到结算。
func TestSettle_RatiosAdjuster_FiltersInvalidValues(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 41, 41, 41
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-ratios-filter", 10000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 5000, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.OtherRatios = map[string]float64{"video_input": 1.0}
	require.NoError(t, task.Insert())

	nan := math.NaN()
	posInf := math.Inf(1)
	negInf := math.Inf(-1)
	adaptor := &mockRatiosAdjuster{
		newRatios: map[string]float64{
			"video_input": 1.2,
			"nan_key":     nan,
			"pos_inf":     posInf,
			"neg_inf":     negInf,
			"zero":        0,
			"negative":    -1.5,
		},
	}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// 只剩 video_input=1.2，其它非法值全被过滤
	assert.Equal(t, map[string]float64{"video_input": 1.2}, task.PrivateData.BillingContext.OtherRatios)
}

// TestSettle_RatiosAdjuster_NilReturnKeepsOriginal 验证 adjuster 返回 nil 时
// 不覆盖 OtherRatios（沿用冻结值）。
func TestSettle_RatiosAdjuster_NilReturnKeepsOriginal(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 42, 42, 42
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-ratios-nil", 10000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 5000, tokenID, BillingSourceWallet, 0)
	original := map[string]float64{"video_input": 0.6087}
	task.PrivateData.BillingContext.OtherRatios = original
	require.NoError(t, task.Insert())

	adaptor := &mockRatiosAdjuster{newRatios: nil}
	settleTaskBillingOnComplete(ctx, adaptor, task, &relaycommon.TaskInfo{})

	assert.Equal(t, original, task.PrivateData.BillingContext.OtherRatios)
}

// TestSettle_NoAdjusterInterface_SkipsRatiosBranch 验证不实现 TaskPollingRatiosAdjuster
// 的 adapter 完全走原路径（老 adapter 兼容）。
func TestSettle_NoAdjusterInterface_SkipsRatiosBranch(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 43, 43, 43
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-noiface", 10000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 5000, tokenID, BillingSourceWallet, 0)
	original := map[string]float64{"video_input": 0.5}
	task.PrivateData.BillingContext.OtherRatios = original
	require.NoError(t, task.Insert())

	adaptor := &mockAdaptor{adjustReturn: 0}
	settleTaskBillingOnComplete(ctx, adaptor, task, &relaycommon.TaskInfo{})

	// OtherRatios 原样保留（没有覆盖分支）
	assert.Equal(t, original, task.PrivateData.BillingContext.OtherRatios)
}
