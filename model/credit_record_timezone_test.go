package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 授信按天 / 按月分桶必须按应用时区（与 vip_daily_consumptions.stat_date 同口径），
// 不能交给数据库时间函数：postgres 容器默认 UTC 时，北京时间 00:00-07:59 的授信
// 会被归到前一天，「按天统计」里就冒出查询范围外的日期行。
//
// 应用时区取 UTC+14（Pacific/Kiritimati），与数据库 / 操作系统时区都不同。
// 注意：SQLite 驱动的 'localtime' 修饰符跟随 Go 的 time.Local，所以单靠 SQLite
// 无法复现 postgres 会话时区（UTC）造成的偏差，因此额外的用例直接锁 creditPeriodKey
// 的分桶口径，确保分桶永远在应用侧完成。
func TestCreditBucketsUseApplicationTimezone(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&CreditRecord{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM credit_records") })

	appZone, err := time.LoadLocation("Pacific/Kiritimati")
	require.NoError(t, err)
	previousLocal := time.Local
	time.Local = appZone
	t.Cleanup(func() { time.Local = previousLocal })

	// 应用时区 2026-11-01 00:30 = UTC 2026-10-31 10:30：跨日且跨月
	earlyMorning := time.Date(2026, 11, 1, 0, 30, 0, 0, appZone).Unix()
	evening := time.Date(2026, 11, 1, 20, 0, 0, 0, appZone).Unix()
	require.NoError(t, DB.Create(&[]CreditRecord{
		{UserId: 7, Type: CreditRecordTypeCredit, Amount: 300, CreatedAt: earlyMorning},
		{UserId: 7, Type: CreditRecordTypeRepayment, Amount: 100, CreatedAt: evening},
		{UserId: 8, Type: CreditRecordTypeCredit, Amount: 50, CreatedAt: evening},
	}).Error)

	dayStart := time.Date(2026, 11, 1, 0, 0, 0, 0, appZone).Unix()
	dayEnd := time.Date(2026, 11, 1, 23, 59, 59, 0, appZone).Unix()

	daily, err := SumCreditByUserDay(dayStart, dayEnd, nil)
	require.NoError(t, err)
	assert.Equal(t, []CreditDailyAggregate{
		{Day: "2026-11-01", UserId: 7, TotalCredit: 300, TotalRepayment: 100},
		{Day: "2026-11-01", UserId: 8, TotalCredit: 50},
	}, daily)

	monthly, err := SumCreditByUserMonth(dayStart, dayEnd, []int{7})
	require.NoError(t, err)
	assert.Equal(t, []CreditMonthlyAggregate{
		{Month: "2026-11", UserId: 7, TotalCredit: 300, TotalRepayment: 100},
	}, monthly)

	trend, err := SumCreditTrendBuckets(CreditRecordTypeCredit, dayStart, dayEnd, false)
	require.NoError(t, err)
	assert.Equal(t, []CreditTrendBucket{{Bucket: "2026-11-01", Amount: 350}}, trend)

	assert.Equal(t, "2026-11-01", creditPeriodKey(earlyMorning, "2006-01-02"))
	assert.Equal(t, "2026-11", creditPeriodKey(earlyMorning, "2006-01"))

	noUsers, err := SumCreditByUserDay(dayStart, dayEnd, []int{})
	require.NoError(t, err)
	assert.Empty(t, noUsers)
}
