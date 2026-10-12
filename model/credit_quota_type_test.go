package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「调整额度 add 模式」只接受充值 / 赠送 / 授信；回款只记账不加额度，
// 必须走专门的回款接口，否则管理员能通过回款凭空给用户加余额。
func TestManageQuotaAddTypesExcludesRepayment(t *testing.T) {
	addTypes := ManageQuotaAddTypes()

	assert.ElementsMatch(t,
		[]string{QuotaTypeRecharge, QuotaTypeGift, QuotaTypeCredit},
		addTypes)
	assert.NotContains(t, addTypes, QuotaTypeRepayment)

	// 金额聚合口径比 add 口径多一个回款，四个口径共用 recharge_input_amount 列。
	assert.ElementsMatch(t,
		[]string{QuotaTypeRecharge, QuotaTypeCredit, QuotaTypeGift, QuotaTypeRepayment},
		ManageQuotaAmountTypes())
}

// ManageQuotaAmountsSelect 必须把同一列 recharge_input_amount 按 quota_type
// 拆成充值 / 授信 / 赠送 / 回款四个互不串台的合计，充值-授信统计页的
// 「未回款授信 = 授信 - 回款」完全依赖这个拆分。
func TestManageQuotaAmountsSelectSplitsByQuotaType(t *testing.T) {
	truncateTables(t)

	const userID = 9101
	amounts := []struct {
		quotaType string
		amount    float64
		createdAt int64
	}{
		{QuotaTypeRecharge, 100, 1000},
		{QuotaTypeRecharge, 50, 3000}, // 最后一次充值
		{QuotaTypeCredit, 200, 2000},
		{QuotaTypeGift, 30, 2500},
		{QuotaTypeRepayment, 80, 4000}, // 晚于最后一次充值，不得污染 last_recharge_at
	}
	opType := OperationTypeQuota
	for _, a := range amounts {
		quotaType := a.quotaType
		amount := a.amount
		require.NoError(t, LOG_DB.Create(&Log{
			UserId:              userID,
			Type:                LogTypeManage,
			OperationType:       &opType,
			QuotaType:           &quotaType,
			RechargeInputAmount: &amount,
			CreatedAt:           a.createdAt,
		}).Error)
	}

	type row struct {
		UserId         int
		TotalRecharge  float64
		TotalCredit    float64
		TotalGift      float64
		TotalRepayment float64
		LastRechargeAt int64
	}
	var got row
	selectExpr, selectArgs := ManageQuotaAmountsSelect(true)
	require.NoError(t, LOG_DB.Model(&Log{}).
		Where("type = ?", LogTypeManage).
		Where("operation_type = ?", OperationTypeQuota).
		Where("quota_type IN ?", ManageQuotaAmountTypes()).
		Where("user_id = ?", userID).
		Select(selectExpr, selectArgs...).
		Group("user_id").
		Scan(&got).Error)

	assert.Equal(t, userID, got.UserId)
	assert.InDelta(t, 150, got.TotalRecharge, 1e-9)
	assert.InDelta(t, 200, got.TotalCredit, 1e-9)
	assert.InDelta(t, 30, got.TotalGift, 1e-9)
	assert.InDelta(t, 80, got.TotalRepayment, 1e-9)
	// last_recharge_at 只看充值：回款虽然更晚，也不能改变这一列的既有含义。
	assert.Equal(t, int64(3000), got.LastRechargeAt)
}
