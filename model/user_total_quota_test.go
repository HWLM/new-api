package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillUsersTotalQuotaInitializesLegacyValueOnce(t *testing.T) {
	truncateTables(t)

	users := []*User{
		{Id: 101, Username: "seedance-user", AffCode: "seedance-101", Quota: 7000, UsedQuota: 5000},
		{Id: 102, Username: "unused-user", AffCode: "unused-102", Quota: 4000},
	}
	require.NoError(t, DB.Create(users).Error)
	require.NoError(t, LOG_DB.Create([]Log{
		{UserId: 101, Type: LogTypeConsume, Quota: 5000},
		{UserId: 101, Type: LogTypeRefund, Quota: 2000},
	}).Error)

	require.NoError(t, FillUsersTotalQuota(users))
	require.NotNil(t, users[0].TotalQuota)
	require.NotNil(t, users[1].TotalQuota)
	assert.EqualValues(t, 10000, *users[0].TotalQuota)
	assert.EqualValues(t, 4000, *users[1].TotalQuota)

	require.NoError(t, LOG_DB.Create(&Log{UserId: 101, Type: LogTypeConsume, Quota: 1000}).Error)
	require.NoError(t, FillUsersTotalQuota(users))
	assert.EqualValues(t, 10000, *users[0].TotalQuota)

	var persistedTotal *int64
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 101).Select("total_quota").Scan(&persistedTotal).Error)
	require.NotNil(t, persistedTotal)
	assert.EqualValues(t, 10000, *persistedTotal)
}

func TestQuotaConsumptionAndRefundDoNotChangePersistedTotal(t *testing.T) {
	truncateTables(t)

	totalQuota := int64(10000)
	user := &User{Id: 103, Username: "stable-total-user", AffCode: "stable-103", Quota: 8000, TotalQuota: &totalQuota}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, DecreaseUserQuota(user.Id, 1000, true))
	require.NoError(t, IncreaseUserQuota(user.Id, 500, true))

	var reloaded User
	require.NoError(t, DB.Select("quota", "total_quota").First(&reloaded, user.Id).Error)
	assert.Equal(t, 7500, reloaded.Quota)
	require.NotNil(t, reloaded.TotalQuota)
	assert.EqualValues(t, 10000, *reloaded.TotalQuota)
}

func TestConfiguredQuotaChangesUpdatePersistedTotal(t *testing.T) {
	truncateTables(t)

	totalQuota := int64(10000)
	user := &User{Id: 104, Username: "configured-total-user", AffCode: "configured-104", Quota: 8000, TotalQuota: &totalQuota}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, IncreaseUserQuotaAndTotal(user.Id, 2000))
	require.NoError(t, DecreaseUserQuotaAndTotal(user.Id, 500))
	require.NoError(t, OverrideUserQuotaAndTotal(user.Id, 9500, 7000))

	var reloaded User
	require.NoError(t, DB.Select("quota", "total_quota").First(&reloaded, user.Id).Error)
	assert.Equal(t, 7000, reloaded.Quota)
	require.NotNil(t, reloaded.TotalQuota)
	assert.EqualValues(t, 9000, *reloaded.TotalQuota)
}
