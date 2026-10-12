package controller

// 充值-授信统计的「今天走 logs、历史走汇总表」切分逻辑测试。
//
// 这段切分是整个页面数据正确性的关键：切错会双算（同一笔钱既在汇总表又在 logs 被统计到）
// 或漏算（今天登记的授信当天看不到）。

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

func setupCreditStatsTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Log{}, &model.VipDailyConsumption{}, &model.DailySummary{},
	))

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// seedCreditManageLog 写一条管理员额度日志（充值/赠送/授信/回款共用 recharge_input_amount 列）。
func seedCreditManageLog(t *testing.T, db *gorm.DB, userID int, quotaType string, amount float64, createdAt int64) {
	t.Helper()
	opType := model.OperationTypeQuota
	qt := quotaType
	amt := amount
	require.NoError(t, db.Create(&model.Log{
		UserId:              userID,
		Type:                model.LogTypeManage,
		OperationType:       &opType,
		QuotaType:           &qt,
		RechargeInputAmount: &amt,
		CreatedAt:           createdAt,
	}).Error)
}

func TestSplitCreditRangeBoundaries(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	weekAgo := time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	cases := []struct {
		name       string
		start, end string
		wantStart  string
		wantEnd    string
		wantToday  bool
	}{
		{
			// 区间完全是历史：全部走汇总表，不碰 logs
			name: "past only", start: weekAgo, end: yesterday,
			wantStart: weekAgo, wantEnd: yesterday, wantToday: false,
		},
		{
			// 只选今天：汇总表没有今天这行，只能查 logs
			name: "today only", start: today, end: today,
			wantStart: "", wantEnd: "", wantToday: true,
		},
		{
			// 近 7 天：汇总表查到昨天为止，今天那段补 logs
			name: "spans today", start: weekAgo, end: today,
			wantStart: weekAgo, wantEnd: yesterday, wantToday: true,
		},
		{
			// 终点在未来：汇总表同样只能到昨天，不会多查出什么
			name: "ends in future", start: weekAgo, end: tomorrow,
			wantStart: weekAgo, wantEnd: yesterday, wantToday: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd, gotToday := splitCreditRange(tc.start, tc.end)
			assert.Equal(t, tc.wantStart, gotStart)
			assert.Equal(t, tc.wantEnd, gotEnd)
			assert.Equal(t, tc.wantToday, gotToday)
		})
	}
}

// 「今天」档位：汇总表里没有今天的行，金额必须全部来自 logs。
func TestCreditSummaryTodayReadsLogs(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	today := time.Now().Format("2006-01-02")
	now := time.Now().Unix()

	// 今天的四笔操作只存在于 logs
	seedCreditManageLog(t, db, 1, model.QuotaTypeRecharge, 100, now)
	seedCreditManageLog(t, db, 1, model.QuotaTypeGift, 20, now)
	seedCreditManageLog(t, db, 2, model.QuotaTypeCredit, 500, now)
	seedCreditManageLog(t, db, 2, model.QuotaTypeRepayment, 200, now)

	resp := callCreditSummary(t, today, today)

	assert.InDelta(t, 100, resp.RechargeAmount, 1e-9)
	assert.InDelta(t, 20, resp.GiftAmount, 1e-9)
	assert.InDelta(t, 500, resp.CreditAmount, 1e-9)
	assert.InDelta(t, 200, resp.RepaymentAmount, 1e-9)
	// 未回款 = 授信 - 回款
	assert.InDelta(t, 300, resp.OutstandingCredit, 1e-9)
}

// 跨天区间：昨天来自汇总表、今天来自 logs，两段相加且互不重复。
// 这里特意让昨天的 logs 也留有记录（真实环境 cron 落盘后 logs 并不会删除），
// 验证历史段不会被 logs 二次计入。
func TestCreditSummaryMergesHistoryAndToday(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	todayStr := time.Now().Format("2006-01-02")
	yesterdayStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	yesterdayTs := time.Now().AddDate(0, 0, -1).Unix()
	now := time.Now().Unix()

	// 昨天：已被 cron 落盘到汇总表，同时 logs 里原始记录仍在
	require.NoError(t, db.Create(&model.VipDailyConsumption{
		UserId: 1, StatDate: yesterdayStr,
		RechargeAmount: 1000, CreditAmount: 300, RepaymentAmount: 100,
	}).Error)
	seedCreditManageLog(t, db, 1, model.QuotaTypeRecharge, 1000, yesterdayTs)
	seedCreditManageLog(t, db, 1, model.QuotaTypeCredit, 300, yesterdayTs)
	seedCreditManageLog(t, db, 1, model.QuotaTypeRepayment, 100, yesterdayTs)
	// 同一天的全局汇总行，供未回款口径读累计列
	require.NoError(t, db.Create(&model.DailySummary{
		StatDate: yesterdayStr, CumCreditAmount: 300, CumRepaymentAmount: 100,
	}).Error)

	// 今天：只在 logs 里
	seedCreditManageLog(t, db, 1, model.QuotaTypeRecharge, 50, now)
	seedCreditManageLog(t, db, 1, model.QuotaTypeCredit, 200, now)

	resp := callCreditSummary(t, yesterdayStr, todayStr)

	// 前四项受时间筛选：昨天 1000 + 今天 50，昨天的 logs 不得被重复计入
	assert.InDelta(t, 1050, resp.RechargeAmount, 1e-9)
	assert.InDelta(t, 500, resp.CreditAmount, 1e-9)
	assert.InDelta(t, 100, resp.RepaymentAmount, 1e-9)
	// 未回款走累计列 + 今天：(300+200) - (100+0) = 400
	assert.InDelta(t, 400, resp.OutstandingCredit, 1e-9)
}

// 区间不含今天：今天的 logs 再多也不能漏进历史查询。
func TestCreditSummaryExcludesTodayWhenOutOfRange(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	yesterdayStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	twoDaysAgo := time.Now().AddDate(0, 0, -2).Format("2006-01-02")

	require.NoError(t, db.Create(&model.VipDailyConsumption{
		UserId: 1, StatDate: yesterdayStr, RechargeAmount: 700,
	}).Error)
	// 今天有一笔大额充值，但查询区间是 [前天, 昨天]，不该出现在结果里
	seedCreditManageLog(t, db, 1, model.QuotaTypeRecharge, 99999, time.Now().Unix())

	resp := callCreditSummary(t, twoDaysAgo, yesterdayStr)
	assert.InDelta(t, 700, resp.RechargeAmount, 1e-9)
}

// 未回款授信按业务约定不为负：回款多于授信时显示 0 而不是负数。
func TestCreditSummaryOutstandingNeverNegative(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	today := time.Now().Format("2006-01-02")

	seedCreditManageLog(t, db, 1, model.QuotaTypeCredit, 100, time.Now().Unix())
	seedCreditManageLog(t, db, 1, model.QuotaTypeRepayment, 250, time.Now().Unix())

	resp := callCreditSummary(t, today, today)
	assert.InDelta(t, 0, resp.OutstandingCredit, 1e-9)
}

// 未回款授信是全时间口径：读 daily_summaries 累计列（截至昨天）+ 今天 logs，
// 且不随 start_date/end_date 变化 —— 换任何时间档位都应该是同一个值。
func TestCreditSummaryOutstandingIgnoresDateFilter(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	todayStr := time.Now().Format("2006-01-02")
	yesterdayStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	weekAgoStr := time.Now().AddDate(0, 0, -6).Format("2006-01-02")

	// 历史累计：截至昨天 授信 1000、回款 400。
	// 特意再写一条更早的行，验证取的是 stat_date 最大那条而不是求和。
	require.NoError(t, db.Create(&model.DailySummary{
		StatDate: weekAgoStr, CumCreditAmount: 600, CumRepaymentAmount: 100,
	}).Error)
	require.NoError(t, db.Create(&model.DailySummary{
		StatDate: yesterdayStr, CumCreditAmount: 1000, CumRepaymentAmount: 400,
	}).Error)

	// 今天：logs 里再加 授信 300、回款 50
	seedCreditManageLog(t, db, 1, model.QuotaTypeCredit, 300, time.Now().Unix())
	seedCreditManageLog(t, db, 1, model.QuotaTypeRepayment, 50, time.Now().Unix())

	// (1000+300) - (400+50) = 850
	const wantOutstanding = 850.0

	// 三种时间档位，未回款都应相同
	for _, r := range [][2]string{
		{todayStr, todayStr},
		{weekAgoStr, todayStr},
		{weekAgoStr, yesterdayStr},
	} {
		resp := callCreditSummary(t, r[0], r[1])
		assert.InDelta(t, wantOutstanding, resp.OutstandingCredit, 1e-9,
			"range %s~%s", r[0], r[1])
	}
}

// daily_summaries 为空（首次部署 / cron 没跑过）时按 0 起算，只统计今天的 logs。
func TestCreditSummaryOutstandingWithoutSummaryRows(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	today := time.Now().Format("2006-01-02")

	seedCreditManageLog(t, db, 1, model.QuotaTypeCredit, 700, time.Now().Unix())
	seedCreditManageLog(t, db, 1, model.QuotaTypeRepayment, 200, time.Now().Unix())

	resp := callCreditSummary(t, today, today)
	assert.InDelta(t, 500, resp.OutstandingCredit, 1e-9)
}

// callCreditSummary 发一次 summary 请求并解出 data 段。
func callCreditSummary(t *testing.T, startDate, endDate string) creditStatsSummaryResp {
	t.Helper()
	router := gin.New()
	router.GET("/api/credit_stats/summary", GetCreditStatsSummary)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/credit_stats/summary?start_date=%s&end_date=%s", startDate, endDate), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Success bool                   `json:"success"`
		Data    creditStatsSummaryResp `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success, w.Body.String())
	return body.Data
}
