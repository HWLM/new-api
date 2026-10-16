package controller

// 「新用户统计 -> 明细数据」与「充值-授信统计」两个页面的授信口径一致性测试。
//
// 三个 tab（汇总 / 按天 / 当日）的授信金额都只认 credit_records，与授信统计页同源。
// 这里锁住的是最容易回退的那条：历史补录的授信只写 credit_records（不改额度、不写 logs，
// 也不会进 vip_daily_consumptions），一旦哪个 tab 退回读聚合表 / logs 就会漏掉它。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDetailsCreditTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	// 这些接口的 raw SQL 引用了保留字列 users.`group`，列引用由 initCol 按方言初始化。
	// 测试直连 sqlite 不走 InitDB，必须显式初始化一次。
	model.InitColumnRefs()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Log{}, &model.VipDailyConsumption{}, &model.CreditRecord{},
	))

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// 补录的历史授信必须出现在三个 tab 里，金额与「充值-授信统计」汇总表一致。
//
// 场景：一笔 3 天前补录的 800 授信（只在 credit_records）+ 今天一笔 200 正常授信
// （credit_records 与 logs 双写）。旧实现读 vip_daily_consumptions.credit_amount +
// 今天 logs，会得到 200（漏掉补录）或 400（把今天那笔的 logs 与新表各算一遍）。
func TestDetailsCreditMatchesCreditRecords(t *testing.T) {
	db := setupDetailsCreditTestDB(t)
	now := time.Now()
	todayStr := now.Format("2006-01-02")
	backfillDay := now.AddDate(0, 0, -3)
	backfillStr := backfillDay.Format("2006-01-02")

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "cust1", DisplayName: "客户一",
	}).Error)

	// 3 天前补录的授信：只写 credit_records，聚合表与 logs 都没有。
	// 存在 12:00 避免时区把日期推到前后一天（补录接口就是这么写的）。
	backfillTs := time.Date(backfillDay.Year(), backfillDay.Month(), backfillDay.Day(),
		12, 0, 0, 0, now.Location()).Unix()
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId: 1, Type: model.CreditRecordTypeCredit, Amount: 800, CreatedAt: backfillTs,
	}).Error)

	// 今天的正常授信：credit_records + logs 双写（线上 add_quota 就是双写）
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId: 1, Type: model.CreditRecordTypeCredit, Amount: 200, CreatedAt: now.Unix(),
	}).Error)
	opType := model.OperationTypeQuota
	creditType := model.QuotaTypeCredit
	creditAmt := 200.0
	require.NoError(t, db.Create(&model.Log{
		UserId: 1, Type: model.LogTypeManage,
		OperationType: &opType, QuotaType: &creditType,
		RechargeInputAmount: &creditAmt, CreatedAt: now.Unix(),
	}).Error)

	// 噪声：聚合表里残留的 credit_amount（cron 仍在写这一列，但已无人读）。
	// 任何 tab 读到它就会多算 999。
	require.NoError(t, db.Create(&model.VipDailyConsumption{
		UserId: 1, StatDate: backfillStr, CreditAmount: 999,
	}).Error)

	const wantTotal = 1000.0

	t.Run("summary tab", func(t *testing.T) {
		rows := callDetailsSummary(t)
		require.Len(t, rows, 1)
		assert.InDelta(t, wantTotal, rows[0].TotalCreditCny, 1e-9)
	})

	t.Run("daily tab splits by actual day", func(t *testing.T) {
		rows := callDetailsDaily(t, backfillStr, todayStr)
		byDate := map[string]float64{}
		for _, r := range rows {
			if r.DailyCreditCny != nil {
				byDate[r.Date] += *r.DailyCreditCny
			}
		}
		// 补录那笔落在补录日期，不是补录操作发生的今天
		assert.InDelta(t, 800, byDate[backfillStr], 1e-9)
		assert.InDelta(t, 200, byDate[todayStr], 1e-9)

		var sum float64
		for _, v := range byDate {
			sum += v
		}
		assert.InDelta(t, wantTotal, sum, 1e-9, "按天合计应等于汇总 tab 的总授信")
	})

	t.Run("single day tab", func(t *testing.T) {
		// 历史日：补录那天
		rows := callDetailsSingleDay(t, backfillStr)
		require.Len(t, rows, 1)
		require.NotNil(t, rows[0].DailyCreditCny)
		assert.InDelta(t, 800, *rows[0].DailyCreditCny, 1e-9)

		// 今天
		rows = callDetailsSingleDay(t, todayStr)
		require.Len(t, rows, 1)
		require.NotNil(t, rows[0].DailyCreditCny)
		assert.InDelta(t, 200, *rows[0].DailyCreditCny, 1e-9)
	})

	t.Run("agrees with credit stats summary table", func(t *testing.T) {
		// 「充值-授信统计」汇总表的总授信金额列，必须和明细数据的总授信一致
		agg, err := model.SumCreditByUsers([]int{1})
		require.NoError(t, err)
		assert.InDelta(t, wantTotal, agg[1].TotalCredit, 1e-9)
	})
}

// 只有回款、没有授信的用户，授信列为 0 而不是把回款算进去。
func TestDetailsCreditExcludesRepayment(t *testing.T) {
	db := setupDetailsCreditTestDB(t)
	now := time.Now()

	require.NoError(t, db.Create(&model.User{Id: 1, Username: "cust1"}).Error)
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId: 1, Type: model.CreditRecordTypeRepayment, Amount: 500, CreatedAt: now.Unix(),
	}).Error)

	rows := callDetailsSummary(t)
	require.Len(t, rows, 1)
	assert.InDelta(t, 0, rows[0].TotalCreditCny, 1e-9)
}

func callDetailsSummary(t *testing.T) []detailsRow {
	t.Helper()
	router := gin.New()
	router.GET("/api/user_stats/details", GetUserStatsDetails)

	req := httptest.NewRequest(http.MethodGet, "/api/user_stats/details", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Success bool        `json:"success"`
		Data    detailsResp `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success, w.Body.String())
	return body.Data.Rows
}

func callDetailsDaily(t *testing.T, startDate, endDate string) []detailsDailyRow {
	t.Helper()
	router := gin.New()
	router.GET("/api/user_stats/details_daily", GetUserStatsDetailsDaily)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf(
		"/api/user_stats/details_daily?start_date=%s&end_date=%s&page_size=200",
		startDate, endDate), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Success bool             `json:"success"`
		Data    detailsDailyResp `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success, w.Body.String())
	return body.Data.Rows
}

func callDetailsSingleDay(t *testing.T, date string) []detailsDailyRow {
	t.Helper()
	router := gin.New()
	router.GET("/api/user_stats/details_singleday", GetUserStatsDetailsSingleDay)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/user_stats/details_singleday?date=%s&page_size=200", date), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Success bool             `json:"success"`
		Data    detailsDailyResp `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success, w.Body.String())
	return body.Data.Rows
}
