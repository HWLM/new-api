package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildUserQuotaHistoryItemsDerivesBeforeAndAfterBalances(t *testing.T) {
	items := buildUserQuotaHistoryItems([]*model.Log{
		{Id: 3, Type: model.LogTypeRefund, Quota: 200},
		{Id: 2, Type: model.LogTypeConsume, Quota: 500},
		{Id: 1, Type: model.LogTypeManage, Quota: 1000},
	}, 1700, 0, false)

	require.Len(t, items, 3)
	require.NotNil(t, items[0].BeforeQuota)
	require.NotNil(t, items[0].AfterQuota)
	require.NotNil(t, items[1].BeforeQuota)
	require.NotNil(t, items[1].AfterQuota)
	require.NotNil(t, items[2].BeforeQuota)
	require.NotNil(t, items[2].AfterQuota)
	assert.Equal(t, int64(1500), *items[0].BeforeQuota)
	assert.Equal(t, int64(1700), *items[0].AfterQuota)
	assert.Equal(t, int64(2000), *items[1].BeforeQuota)
	assert.Equal(t, int64(1500), *items[1].AfterQuota)
	assert.Equal(t, int64(1000), *items[2].BeforeQuota)
	assert.Equal(t, int64(2000), *items[2].AfterQuota)
}

func TestBuildUserQuotaHistoryItemsStopsAtUnknownHistoricalDelta(t *testing.T) {
	items := buildUserQuotaHistoryItems([]*model.Log{
		{Id: 3, Type: model.LogTypeConsume, Quota: 500},
		{Id: 2, Type: model.LogTypeTopup, Quota: 0},
		{Id: 1, Type: model.LogTypeConsume, Quota: 200},
	}, 1500, 0, false)

	require.Len(t, items, 3)
	require.NotNil(t, items[0].BeforeQuota)
	require.NotNil(t, items[0].AfterQuota)
	assert.Equal(t, int64(2000), *items[0].BeforeQuota)
	assert.Equal(t, int64(1500), *items[0].AfterQuota)
	assert.Nil(t, items[1].BeforeQuota)
	assert.Nil(t, items[1].AfterQuota)
	assert.Nil(t, items[2].BeforeQuota)
	assert.Nil(t, items[2].AfterQuota)
}

func TestBuildUserQuotaHistoryItemsAccountsForNewerPages(t *testing.T) {
	items := buildUserQuotaHistoryItems([]*model.Log{
		{Id: 1, Type: model.LogTypeConsume, Quota: 300},
	}, 1200, -500, false)

	require.Len(t, items, 1)
	require.NotNil(t, items[0].BeforeQuota)
	require.NotNil(t, items[0].AfterQuota)
	assert.Equal(t, int64(2000), *items[0].BeforeQuota)
	assert.Equal(t, int64(1700), *items[0].AfterQuota)
}

func TestBuildUserQuotaHistoryItemsUsesStoredSnapshotAndContinuesLegacyDerivation(t *testing.T) {
	beforeQuota := int64(2000)
	afterQuota := int64(1500)
	items := buildUserQuotaHistoryItems([]*model.Log{
		{Id: 2, Type: model.LogTypeConsume, Quota: 999, BeforeQuota: &beforeQuota, AfterQuota: &afterQuota},
		{Id: 1, Type: model.LogTypeConsume, Quota: 200},
	}, 10, 12345, true)

	require.Len(t, items, 2)
	require.NotNil(t, items[0].DeltaQuota)
	assert.Equal(t, int64(-500), *items[0].DeltaQuota)
	assert.Equal(t, beforeQuota, *items[0].BeforeQuota)
	assert.Equal(t, afterQuota, *items[0].AfterQuota)
	require.NotNil(t, items[1].BeforeQuota)
	require.NotNil(t, items[1].AfterQuota)
	assert.Equal(t, int64(2200), *items[1].BeforeQuota)
	assert.Equal(t, int64(2000), *items[1].AfterQuota)
}
