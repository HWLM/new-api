package controller

// 「调整额度-减少」带类型时的记账行为测试。
//
// 核心不变量：减少会写一条 recharge_input_amount 为负的 manage 日志，
// 于是 ManageQuotaAmountsSelect 的 SUM 直接等于净额（加的 - 减的）。
// 这是为了修「今天充 100、明天减 50，聚合表却仍统计 100」这个漏算 ——
// 所有读这些口径的报表都靠这个符号约定，一旦减少不记类型或记正数就会回退。

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedSubtractTestUser 建一个带初始额度的普通用户。
func seedSubtractTestUser(t *testing.T, db *gorm.DB, username string, quota int) *model.User {
	t.Helper()
	total := int64(quota)
	user := model.User{
		Username: username, Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
		Quota: quota, TotalQuota: &total,
	}
	require.NoError(t, db.Create(&user).Error)
	return &user
}

// manageAmountsByUser 用生产代码的聚合口径读某用户的四项金额净额。
func manageAmountsByUser(t *testing.T, userId int, startTs, endTs int64) (recharge, credit, gift, repayment float64) {
	t.Helper()
	type row struct {
		UserId         int
		TotalRecharge  float64
		TotalCredit    float64
		TotalGift      float64
		TotalRepayment float64
	}
	selectExpr, selectArgs := model.ManageQuotaAmountsSelect(false)
	var r row
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).
		Where("type = ?", model.LogTypeManage).
		Where("operation_type = ?", model.OperationTypeQuota).
		Where("quota_type IN ?", model.ManageQuotaAmountTypes()).
		Where("user_id = ?", userId).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Select(selectExpr, selectArgs...).
		Group("user_id").
		Scan(&r).Error)
	return r.TotalRecharge, r.TotalCredit, r.TotalGift, r.TotalRepayment
}

// 带类型的减少：logs 记下类型，金额为负，用户额度按 value 扣减。
func TestManageUserSubtractWithTypeRecordsNegativeAmount(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := seedSubtractTestUser(t, db, "subtract-typed-user", 1_000_000)

	// 页面填 50（展示币种），前端换算出 value=250000 quota
	recorder := performManageUserRequest(t, fmt.Sprintf(
		`{"id":%d,"action":"add_quota","mode":"subtract","value":250000,"quota_type":"充值","recharge_amount":50}`,
		user.Id))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	// 额度按 value 扣，不受 recharge_amount 影响
	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, 750_000, updated.Quota)

	var logs []model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", user.Id, model.LogTypeManage).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.NotNil(t, logs[0].QuotaType)
	assert.Equal(t, model.QuotaTypeRecharge, *logs[0].QuotaType)
	require.NotNil(t, logs[0].RechargeInputAmount)
	assert.InDelta(t, -50, *logs[0].RechargeInputAmount, 1e-9, "减少必须记负数，否则 SUM 得不到净额")
	assert.Equal(t, -250_000, logs[0].Quota)
}

// 你报的场景：今天充 100、明天减 50，金额口径的合计必须是 50。
func TestManageUserSubtractNetsAgainstEarlierAdd(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := seedSubtractTestUser(t, db, "subtract-net-user", 0)

	// 先充值 100（走 add 模式，ratio=1、价格=1 便于断言额度）
	originalPrice := operation_setting.Price
	operation_setting.Price = 1
	t.Cleanup(func() { operation_setting.Price = originalPrice })

	recorder := performManageUserRequest(t, fmt.Sprintf(
		`{"id":%d,"action":"add_quota","mode":"add","quota_type":"充值","recharge_amount":100,"ratio":1}`,
		user.Id))
	require.Contains(t, recorder.Body.String(), `"success":true`)

	// 再减少 50
	recorder = performManageUserRequest(t, fmt.Sprintf(
		`{"id":%d,"action":"add_quota","mode":"subtract","value":250000,"quota_type":"充值","recharge_amount":50}`,
		user.Id))
	require.Contains(t, recorder.Body.String(), `"success":true`)

	now := time.Now().Unix()
	recharge, _, _, _ := manageAmountsByUser(t, user.Id, now-3600, now+3600)
	assert.InDelta(t, 50, recharge, 1e-9, "充值净额应为 100 - 50")
}

// 类型=授信的减少：credit_records 多一条负数流水，授信统计页的各口径跟着减。
func TestManageUserSubtractCreditWritesNegativeCreditRecord(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CreditRecord{}))
	user := seedSubtractTestUser(t, db, "subtract-credit-user", 5_000_000)

	// 先授信 1000
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId: user.Id, Type: model.CreditRecordTypeCredit, Amount: 1000,
		CreatedAt: time.Now().Unix(),
	}).Error)

	// 再减少 300（类型=授信）
	recorder := performManageUserRequest(t, fmt.Sprintf(
		`{"id":%d,"action":"add_quota","mode":"subtract","value":1500000,"quota_type":"授信","recharge_amount":300}`,
		user.Id))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	var records []model.CreditRecord
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("id").Find(&records).Error)
	require.Len(t, records, 2)
	assert.InDelta(t, 1000, records[0].Amount, 1e-9)
	assert.InDelta(t, -300, records[1].Amount, 1e-9)
	assert.Equal(t, model.CreditRecordTypeCredit, records[1].Type)

	// 授信统计页的总授信 = 净额 700
	agg, err := model.SumCreditByUsers([]int{user.Id})
	require.NoError(t, err)
	assert.InDelta(t, 700, agg[user.Id].TotalCredit, 1e-9)
}

// 授信全额冲回后净额为 0，该用户不再算「授信客户」。
func TestCreditedUserIdsExcludesFullyReversed(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CreditRecord{}))
	now := time.Now().Unix()

	// 用户 1：授信 500 后全额冲回 → 净额 0
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId: 1, Type: model.CreditRecordTypeCredit, Amount: 500, CreatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId: 1, Type: model.CreditRecordTypeCredit, Amount: -500, CreatedAt: now,
	}).Error)
	// 用户 2：授信 500 只冲回 200 → 净额 300，仍是授信客户
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId: 2, Type: model.CreditRecordTypeCredit, Amount: 500, CreatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId: 2, Type: model.CreditRecordTypeCredit, Amount: -200, CreatedAt: now,
	}).Error)

	ids, err := model.GetCreditedUserIds()
	require.NoError(t, err)
	assert.NotContains(t, ids, 1, "授信已全额冲回的用户不该再带授信客户标签")
	assert.Contains(t, ids, 2)
}

// 不带类型的减少：保留原有行为 —— 按 value 扣、不登记来源、金额列为 NULL。
func TestManageUserSubtractWithoutTypeKeepsLegacyBehavior(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := seedSubtractTestUser(t, db, "subtract-legacy-user", 1_000_000)

	recorder := performManageUserRequest(t, fmt.Sprintf(
		`{"id":%d,"action":"add_quota","mode":"subtract","value":400000}`, user.Id))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	var updated model.User
	require.NoError(t, db.First(&updated, user.Id).Error)
	assert.Equal(t, 600_000, updated.Quota)

	var logs []model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", user.Id, model.LogTypeManage).Find(&logs).Error)
	require.Len(t, logs, 1)
	if logs[0].QuotaType != nil {
		assert.Equal(t, "", *logs[0].QuotaType)
	}
	assert.Nil(t, logs[0].RechargeInputAmount)
}

// 回款不是合法的减少类型（它只记账、不改额度）；金额非正也要拒绝。
func TestManageUserSubtractRejectsInvalidInput(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := seedSubtractTestUser(t, db, "subtract-invalid-user", 1_000_000)

	cases := []struct {
		name string
		body string
	}{
		{
			name: "repayment is not a subtract type",
			body: `{"id":%d,"action":"add_quota","mode":"subtract","value":100000,"quota_type":"回款","recharge_amount":20}`,
		},
		{
			name: "unknown type",
			body: `{"id":%d,"action":"add_quota","mode":"subtract","value":100000,"quota_type":"不存在","recharge_amount":20}`,
		},
		{
			name: "zero amount with type",
			body: `{"id":%d,"action":"add_quota","mode":"subtract","value":100000,"quota_type":"充值","recharge_amount":0}`,
		},
		{
			name: "zero value with type",
			body: `{"id":%d,"action":"add_quota","mode":"subtract","value":0,"quota_type":"充值","recharge_amount":20}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := performManageUserRequest(t, fmt.Sprintf(tc.body, user.Id))
			assert.Contains(t, recorder.Body.String(), `"success":false`)
			// 额度不得变化
			var updated model.User
			require.NoError(t, db.First(&updated, user.Id).Error)
			assert.Equal(t, 1_000_000, updated.Quota)
		})
	}
}
