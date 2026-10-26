package controller

// 充值-授信统计顶部卡片的取数口径测试。
//
// 覆盖三组容易出错的约定：
//  1. 充值 / 赠送的「今天走 logs、历史走汇总表」切分 —— 切错会双算（同一笔钱既在汇总表
//     又在 logs 被统计到）或漏算（今天登记的充值当天看不到）。
//  2. 授信 / 回款只认 credit_records，不读 logs 与 vip_daily_consumptions。
//  3. 剩余应收 / 未回款授信金额是全时间口径，不随时间筛选变化，且都不为负。

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
		&model.User{}, &model.Log{}, &model.VipDailyConsumption{}, &model.CreditRecord{},
	))

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// seedCreditManageLog 写一条管理员额度日志（充值 / 赠送共用 recharge_input_amount 列）。
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

// seedCreditRecord 写一条授信 / 回款流水。GORM 的 autoCreateTime 不会覆盖非零 CreatedAt，
// 所以这里能精确控制流水落在哪一天。
func seedCreditRecord(t *testing.T, db *gorm.DB, userID, recordType int, amount float64, createdAt int64) {
	t.Helper()
	require.NoError(t, db.Create(&model.CreditRecord{
		UserId:    userID,
		Type:      recordType,
		Amount:    amount,
		CreatedAt: createdAt,
	}).Error)
}

// seedConsumeLog 写一条消耗日志（净消耗口径的正向部分）。
func seedConsumeLog(t *testing.T, db *gorm.DB, userID int, quota int, createdAt int64) {
	t.Helper()
	require.NoError(t, db.Create(&model.Log{
		UserId:    userID,
		Type:      model.LogTypeConsume,
		Quota:     quota,
		CreatedAt: createdAt,
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

// 「今天」档位：汇总表里没有今天的行，充值 / 赠送必须全部来自 logs，
// 授信 / 回款必须来自 credit_records。
func TestCreditSummaryTodayReadsLogsAndRecords(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	today := time.Now().Format("2006-01-02")
	now := time.Now().Unix()

	seedCreditManageLog(t, db, 1, model.QuotaTypeRecharge, 100, now)
	seedCreditManageLog(t, db, 1, model.QuotaTypeGift, 20, now)
	seedCreditRecord(t, db, 2, model.CreditRecordTypeCredit, 500, now)
	seedCreditRecord(t, db, 2, model.CreditRecordTypeRepayment, 200, now)

	resp := callCreditSummary(t, today, today)

	assert.InDelta(t, 100, resp.RechargeAmount, 1e-9)
	assert.InDelta(t, 20, resp.GiftAmount, 1e-9)
	assert.InDelta(t, 500, resp.CreditAmount, 1e-9)
	assert.InDelta(t, 200, resp.RepaymentAmount, 1e-9)
	// 未回款 = 授信 - 回款
	assert.InDelta(t, 300, resp.OutstandingCredit, 1e-9)
}

// 授信走 credit_records 而不是 logs：授信操作会同时往 logs 写一条审计记录（双写），
// 如果卡片还从 logs 读就会把同一笔授信算两遍。
func TestCreditSummaryIgnoresCreditLogs(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	today := time.Now().Format("2006-01-02")
	now := time.Now().Unix()

	// 一笔 500 的授信：credit_records 一行 + logs 一行审计（线上就是这样双写的）
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 500, now)
	seedCreditManageLog(t, db, 1, model.QuotaTypeCredit, 500, now)

	resp := callCreditSummary(t, today, today)
	assert.InDelta(t, 500, resp.CreditAmount, 1e-9)
}

// 跨天区间：充值的昨天那段来自汇总表、今天那段来自 logs，两段相加且互不重复。
// 这里特意让昨天的 logs 也留有记录（真实环境 cron 落盘后 logs 并不会删除），
// 验证历史段不会被 logs 二次计入。
func TestCreditSummaryMergesHistoryAndToday(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	todayStr := time.Now().Format("2006-01-02")
	yesterdayStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	yesterdayTs := time.Now().AddDate(0, 0, -1).Unix()
	now := time.Now().Unix()

	// 昨天的充值：已被 cron 落盘到汇总表，同时 logs 里原始记录仍在
	require.NoError(t, db.Create(&model.VipDailyConsumption{
		UserId: 1, StatDate: yesterdayStr, RechargeAmount: 1000,
	}).Error)
	seedCreditManageLog(t, db, 1, model.QuotaTypeRecharge, 1000, yesterdayTs)

	// 昨天的授信 / 回款：credit_records 是实时表，不存在「落盘」概念
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 300, yesterdayTs)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeRepayment, 100, yesterdayTs)

	// 今天：充值只在 logs，授信在 credit_records
	seedCreditManageLog(t, db, 1, model.QuotaTypeRecharge, 50, now)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 200, now)

	resp := callCreditSummary(t, yesterdayStr, todayStr)

	// 充值：昨天 1000（汇总表）+ 今天 50（logs），昨天的 logs 不得被重复计入
	assert.InDelta(t, 1050, resp.RechargeAmount, 1e-9)
	assert.InDelta(t, 500, resp.CreditAmount, 1e-9)
	assert.InDelta(t, 100, resp.RepaymentAmount, 1e-9)
	// 未回款是全时间口径：(300+200) - 100 = 400
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

// 剩余应收与未回款授信都按业务约定不为负：回款多于授信 / 多于消耗时显示 0 而不是负数。
func TestCreditSummaryNeverNegative(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	today := time.Now().Format("2006-01-02")
	now := time.Now().Unix()

	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 100, now)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeRepayment, 250, now)

	resp := callCreditSummary(t, today, today)
	assert.InDelta(t, 0, resp.OutstandingCredit, 1e-9)
	// 该用户没有任何消耗，总消耗 0 - 回款 250 为负，应收应落到 0
	assert.InDelta(t, 0, resp.Receivable, 1e-9)
}

// 剩余应收 = 授信客户的累计消耗（历史汇总表 + 今天 logs，按 quota/QuotaPerUnit 折算）
// - 总回款，且与未回款授信一样不随时间筛选变化。
func TestCreditSummaryReceivable(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	todayStr := time.Now().Format("2006-01-02")
	yesterdayStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	weekAgoStr := time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	now := time.Now().Unix()

	// 固定换算参数，避免测试依赖站点配置
	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })
	common.QuotaPerUnit = 500_000.0

	// 用户 1 是授信客户，用户 2 不是 —— 用户 2 的消耗不能进应收
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 1000, now)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeRepayment, 400, now)

	// 用户 1 的历史消耗（汇总表，截至昨天）：2 天共 1_000_000 quota = 2
	require.NoError(t, db.Create(&model.VipDailyConsumption{
		UserId: 1, StatDate: weekAgoStr, Quota: 500_000,
	}).Error)
	require.NoError(t, db.Create(&model.VipDailyConsumption{
		UserId: 1, StatDate: yesterdayStr, Quota: 500_000,
	}).Error)
	// 用户 1 今天的消耗只在 logs：250_000 quota = 0.5
	seedConsumeLog(t, db, 1, 250_000, now)

	// 噪声：非授信客户的消耗（汇总表 + 今天 logs）都不应计入
	require.NoError(t, db.Create(&model.VipDailyConsumption{
		UserId: 2, StatDate: yesterdayStr, Quota: 9_000_000,
	}).Error)
	seedConsumeLog(t, db, 2, 9_000_000, now)

	// 2.5 - 400 为负 → 先验证不为负的兜底，再换一组能算出正值的回款额
	resp := callCreditSummary(t, todayStr, todayStr)
	assert.InDelta(t, 0, resp.Receivable, 1e-9)

	// 把回款改成 1：2.5 - 1 = 1.5
	require.NoError(t, db.Model(&model.CreditRecord{}).
		Where("type = ?", model.CreditRecordTypeRepayment).
		Update("amount", 1).Error)

	// 三种时间档位，应收都应相同（全时间口径）
	for _, r := range [][2]string{
		{todayStr, todayStr},
		{weekAgoStr, todayStr},
		{weekAgoStr, yesterdayStr},
	} {
		resp := callCreditSummary(t, r[0], r[1])
		assert.InDelta(t, 1.5, resp.Receivable, 1e-9, "range %s~%s", r[0], r[1])
	}
}

// 未回款授信是全时间口径：不随 start_date/end_date 变化 —— 换任何时间档位都是同一个值。
func TestCreditSummaryOutstandingIgnoresDateFilter(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	todayStr := time.Now().Format("2006-01-02")
	yesterdayStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	weekAgoStr := time.Now().AddDate(0, 0, -6).Format("2006-01-02")

	// 一周前：授信 600、回款 100
	weekAgoTs := time.Now().AddDate(0, 0, -6).Unix()
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 600, weekAgoTs)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeRepayment, 100, weekAgoTs)
	// 昨天：授信 400、回款 300
	yesterdayTs := time.Now().AddDate(0, 0, -1).Unix()
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 400, yesterdayTs)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeRepayment, 300, yesterdayTs)
	// 今天：授信 300、回款 50
	now := time.Now().Unix()
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 300, now)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeRepayment, 50, now)

	// (600+400+300) - (100+300+50) = 850
	const wantOutstanding = 850.0

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

// credit_records 为空（功能刚上线、还没录入过）时四个金额与两个全时间口径都应为 0，
// 而不是报错或读出 logs 里的历史授信。
func TestCreditSummaryWithoutCreditRecords(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	today := time.Now().Format("2006-01-02")

	// logs 里有授信审计记录，但 credit_records 空 —— 卡片只认新表
	seedCreditManageLog(t, db, 1, model.QuotaTypeCredit, 700, time.Now().Unix())

	resp := callCreditSummary(t, today, today)
	assert.InDelta(t, 0, resp.CreditAmount, 1e-9)
	assert.InDelta(t, 0, resp.RepaymentAmount, 1e-9)
	assert.InDelta(t, 0, resp.OutstandingCredit, 1e-9)
	assert.InDelta(t, 0, resp.Receivable, 1e-9)
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

// 授信用户口径 = 有过任何授信或回款记录的用户：授信后全额冲回（净额 0）、
// 只有回款记录的用户，消耗都要计入剩余应收与消耗授信金额。
func TestCreditSummaryConsumptionCountsAllRecordUsers(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	todayStr := time.Now().Format("2006-01-02")
	yesterdayStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	weekAgoStr := time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	now := time.Now().Unix()

	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })
	common.QuotaPerUnit = 500_000.0

	// 用户 1：授信 100 后全额冲回，净额 0
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 100, now)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, -100, now)
	// 用户 2：只有回款记录
	seedCreditRecord(t, db, 2, model.CreditRecordTypeRepayment, 1, now)

	// 一周前各消耗 1（汇总表），今天用户 1 再消耗 0.5（logs）
	require.NoError(t, db.Create(&model.VipDailyConsumption{UserId: 1, StatDate: weekAgoStr, Quota: 500_000}).Error)
	require.NoError(t, db.Create(&model.VipDailyConsumption{UserId: 2, StatDate: weekAgoStr, Quota: 500_000}).Error)
	seedConsumeLog(t, db, 1, 250_000, now)
	// 噪声：没有任何授信 / 回款记录的用户
	require.NoError(t, db.Create(&model.VipDailyConsumption{UserId: 3, StatDate: weekAgoStr, Quota: 9_000_000}).Error)
	seedConsumeLog(t, db, 3, 9_000_000, now)

	// 剩余应收全时间：(1 + 0.5 + 1) - 回款 1 = 1.5，与时间筛选无关
	// 消耗授信金额随时间筛选：只选今天 = 0.5；近 7 天 = 2.5；截至昨天 = 2
	for _, tc := range []struct {
		start, end      string
		wantConsumption float64
	}{
		{todayStr, todayStr, 0.5},
		{weekAgoStr, todayStr, 2.5},
		{weekAgoStr, yesterdayStr, 2},
	} {
		resp := callCreditSummary(t, tc.start, tc.end)
		assert.InDelta(t, tc.wantConsumption, resp.CreditConsumption, 1e-9, "range %s~%s", tc.start, tc.end)
		assert.InDelta(t, 1.5, resp.Receivable, 1e-9, "range %s~%s", tc.start, tc.end)
	}
}

// 汇总表：新增的总授信消耗 / 剩余应收列按用户计算（剩余应收逐用户不为负），
// 汇总条合计整个筛选范围，排序作用于全部行后再分页。
func TestCreditDetailsSummaryConsumptionAndSort(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	weekAgoStr := time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	now := time.Now().Unix()

	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })
	common.QuotaPerUnit = 500_000.0

	for _, u := range []model.User{{Id: 1, Username: "u1", AffCode: "a1"}, {Id: 2, Username: "u2", AffCode: "a2"}, {Id: 3, Username: "u3", AffCode: "a3"}} {
		require.NoError(t, db.Create(&u).Error)
	}
	// 用户 1：消耗 3，回款 1 → 剩余应收 2
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 10, now)
	seedCreditRecord(t, db, 1, model.CreditRecordTypeRepayment, 1, now)
	require.NoError(t, db.Create(&model.VipDailyConsumption{UserId: 1, StatDate: weekAgoStr, Quota: 1_000_000}).Error)
	seedConsumeLog(t, db, 1, 500_000, now)
	// 用户 2：消耗 1，回款 5 → 剩余应收 0（不为负）
	seedCreditRecord(t, db, 2, model.CreditRecordTypeCredit, 20, now)
	seedCreditRecord(t, db, 2, model.CreditRecordTypeRepayment, 5, now)
	require.NoError(t, db.Create(&model.VipDailyConsumption{UserId: 2, StatDate: weekAgoStr, Quota: 500_000}).Error)
	// 用户 3：消耗 6，无回款 → 剩余应收 6
	seedCreditRecord(t, db, 3, model.CreditRecordTypeCredit, 5, now)
	seedConsumeLog(t, db, 3, 3_000_000, now)

	body := callCreditDetailsSummary(t, "sort_by=receivable&sort_dir=desc&page=1&page_size=2")
	require.Len(t, body.Rows, 2)
	assert.Equal(t, int64(3), body.Total)
	assert.Equal(t, []int{3, 1}, []int{body.Rows[0].UserId, body.Rows[1].UserId})
	assert.InDelta(t, 6, body.Rows[0].TotalConsumption, 1e-9)
	assert.InDelta(t, 3, body.Rows[1].TotalConsumption, 1e-9)
	assert.InDelta(t, 2, body.Rows[1].Receivable, 1e-9)
	// 合计覆盖全部 3 行而不是当前页：消耗 3+1+6，应收 2+0+6
	assert.InDelta(t, 10, body.SummaryConsumption, 1e-9)
	assert.InDelta(t, 8, body.SummaryReceivable, 1e-9)

	// 升序第二页拿到的是排在最后的用户 3
	body = callCreditDetailsSummary(t, "sort_by=receivable&sort_dir=asc&page=2&page_size=2")
	require.Len(t, body.Rows, 1)
	assert.Equal(t, 3, body.Rows[0].UserId)

	// 按总授信升序：5(u3) < 10(u1) < 20(u2)
	body = callCreditDetailsSummary(t, "sort_by=total_credit&sort_dir=asc&page=1&page_size=10")
	require.Len(t, body.Rows, 3)
	assert.Equal(t, []int{3, 1, 2}, []int{body.Rows[0].UserId, body.Rows[1].UserId, body.Rows[2].UserId})

	// 非白名单排序列回落到默认 id DESC
	body = callCreditDetailsSummary(t, "sort_by=username&page=1&page_size=10")
	require.Len(t, body.Rows, 3)
	assert.Equal(t, []int{3, 2, 1}, []int{body.Rows[0].UserId, body.Rows[1].UserId, body.Rows[2].UserId})
}

// 明细表：本月授信消耗按月拆分（历史月走汇总表，本月含今天 logs）；
// 只有消耗、无授信回款且不欠款的月份也要出现。
func TestCreditDetailsMonthlyConsumption(t *testing.T) {
	db := setupCreditStatsTestDB(t)
	now := time.Now()
	thisMonth := now.Format("2006-01")
	lastMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0)
	lastMonth := lastMonthStart.Format("2006-01")

	originalQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })
	common.QuotaPerUnit = 500_000.0

	require.NoError(t, db.Create(&model.User{Id: 1, Username: "u1", AffCode: "a1"}).Error)
	// 上月：授信 10、回款 10（结清）；本月没有任何授信 / 回款流水
	seedCreditRecord(t, db, 1, model.CreditRecordTypeCredit, 10, lastMonthStart.Unix())
	seedCreditRecord(t, db, 1, model.CreditRecordTypeRepayment, 10, lastMonthStart.Unix())
	// 上月消耗 1（汇总表），本月：今天 logs 消耗 0.5
	require.NoError(t, db.Create(&model.VipDailyConsumption{
		UserId: 1, StatDate: lastMonthStart.Format("2006-01-02"), Quota: 500_000,
	}).Error)
	seedConsumeLog(t, db, 1, 250_000, now.Unix())

	body := callCreditDetailsMonthly(t, lastMonth, thisMonth)
	got := map[string]float64{}
	for _, r := range body.Rows {
		got[r.Month] = r.MonthConsumption
	}
	// 本月只有消耗，没有授信回款且月初月末不欠款，也必须保留这一行
	assert.Equal(t, map[string]float64{lastMonth: 1, thisMonth: 0.5}, got)
	assert.InDelta(t, 1.5, body.SummaryConsumption, 1e-9)
}

type creditDetailsSummaryBody struct {
	Rows               []creditSummaryRow `json:"rows"`
	Total              int64              `json:"total"`
	SummaryConsumption float64            `json:"summary_consumption"`
	SummaryReceivable  float64            `json:"summary_receivable"`
}

func callCreditDetailsSummary(t *testing.T, query string) creditDetailsSummaryBody {
	t.Helper()
	router := gin.New()
	router.GET("/api/credit_stats/details_summary", GetCreditDetailsSummary)

	req := httptest.NewRequest(http.MethodGet, "/api/credit_stats/details_summary?"+query, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Success bool                     `json:"success"`
		Data    creditDetailsSummaryBody `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success, w.Body.String())
	return body.Data
}

type creditDetailsMonthlyBody struct {
	Rows               []creditMonthlyRow `json:"rows"`
	SummaryConsumption float64            `json:"summary_consumption"`
}

func callCreditDetailsMonthly(t *testing.T, startMonth, endMonth string) creditDetailsMonthlyBody {
	t.Helper()
	router := gin.New()
	router.GET("/api/credit_stats/details_monthly", GetCreditDetailsMonthly)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf(
		"/api/credit_stats/details_monthly?start_month=%s&end_month=%s&page_size=100", startMonth, endMonth), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Success bool                     `json:"success"`
		Data    creditDetailsMonthlyBody `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success, w.Body.String())
	return body.Data
}
