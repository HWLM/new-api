package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLogTokenQuotaDataBucketsByLocalCalendarDay(t *testing.T) {
	originalLocation := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	time.Local = location
	t.Cleanup(func() {
		time.Local = originalLocation
	})

	CacheTokenQuotaDataLock.Lock()
	CacheTokenQuotaData = make(map[string]*TokenQuotaData)
	CacheTokenQuotaDataLock.Unlock()
	t.Cleanup(func() {
		CacheTokenQuotaDataLock.Lock()
		CacheTokenQuotaData = make(map[string]*TokenQuotaData)
		CacheTokenQuotaDataLock.Unlock()
	})

	beforeEightAM := time.Date(2026, time.July, 30, 7, 59, 0, 0, location).Unix()
	afterEightAM := time.Date(2026, time.July, 30, 8, 0, 0, 0, location).Unix()
	LogTokenQuotaData(1, 2, "kratos", "default", 100, beforeEightAM, 10)
	LogTokenQuotaData(1, 2, "kratos", "default", 200, afterEightAM, 20)

	expectedDayStart := time.Date(2026, time.July, 30, 0, 0, 0, 0, location).Unix()
	CacheTokenQuotaDataLock.Lock()
	defer CacheTokenQuotaDataLock.Unlock()
	require.Len(t, CacheTokenQuotaData, 1)
	entry := CacheTokenQuotaData[fmt.Sprintf("1-2-%d", expectedDayStart)]
	require.NotNil(t, entry)
	require.Equal(t, expectedDayStart, entry.CreatedAt)
	require.Equal(t, 2, entry.Count)
	require.Equal(t, 300, entry.Quota)
	require.Equal(t, 30, entry.TokenUsed)
}
