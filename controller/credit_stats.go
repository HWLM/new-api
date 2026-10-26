/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package controller

// 「数据看板 -> 充值-授信统计」专用接口。
// 全部要求管理员权限（在路由层 middleware.AdminAuth() 拦截）。
//
// 口径说明：
//   - 充值 / 赠送 / 授信 / 回款四个金额口径都来自 logs.recharge_input_amount（人民币 ¥），
//     靠 logs.quota_type 区分；cron 每天把它们汇总进 vip_daily_consumptions 的四个金额列。
//   - 未回款授信金额 = 授信 - 回款，按业务约定不为负数，最少为 0。
//   - 回款日志（quota_type=回款）只登记金额，不改变用户额度（quota 恒为 0）。

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// creditTrendDimensionColumns 把前端的趋势维度映射到 vip_daily_consumptions 的金额列。
// 维度名不可直接拼进 SQL，必须经由这张白名单，避免注入。
// 只覆盖充值与赠送：授信 / 回款走 credit_records，不读这张聚合表。
var creditTrendDimensionColumns = map[string]string{
	"recharge": "recharge_amount",
	"gift":     "gift_amount",
}

// creditStatsSummaryResp 顶部 7 张卡片
type creditStatsSummaryResp struct {
	RechargeAmount    float64 `json:"recharge_amount"`    // 充值金额（¥）
	GiftAmount        float64 `json:"gift_amount"`        // 赠送金额（¥）
	CreditAmount      float64 `json:"credit_amount"`      // 授信金额（¥）
	RepaymentAmount   float64 `json:"repayment_amount"`   // 回款金额（¥）
	CreditConsumption float64 `json:"credit_consumption"` // 消耗授信金额 = 授信客户在查询区间内的消耗（quota/QuotaPerUnit）
	Receivable        float64 `json:"receivable"`         // 剩余应收 = 授信客户总消耗（quota/QuotaPerUnit） - 总回款，不为负
	OutstandingCredit float64 `json:"outstanding_credit"` // 未回款授信金额（¥）= 授信 - 回款，不为负
}

// GetCreditStatsSummary 顶部 7 张卡片：按查询时间区间汇总平台总的充值、赠送、授信、回款、授信消耗金额。
// GET /api/credit_stats/summary?start_date=YYYY-MM-DD&end_date=YYYY-MM-DD
func GetCreditStatsSummary(c *gin.Context) {
	startDate, endDate, ok := parseCreditDateRange(c)
	if !ok {
		return
	}

	// 充值 / 赠送：历史走汇总表，今天（若在区间内）回 logs 实时聚合，两段相加。
	histStart, histEnd, includeToday := splitCreditRange(startDate, endDate)
	total, err := sumCreditAmountsFromDaily(histStart, histEnd, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if includeToday {
		todayStart, todayEnd := todayUnixRange()
		todayAmounts, err := sumCreditAmountsFromLogs(todayStart, todayEnd, nil)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		total.add(todayAmounts)
	}

	// 授信 / 回款：只认 credit_records。回款从不写 logs，授信虽然双写 logs，
	// 但两个口径必须同源，否则「授信 - 回款」会对不上账。
	rangeStartTs, rangeEndTs := creditDateRangeUnix(startDate, endDate)
	creditAgg, err := model.SumCreditInRange(rangeStartTs, rangeEndTs)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 消耗授信金额：授信客户在查询区间内的消耗，随时间筛选变化。
	creditUserIds, err := model.GetCreditRecordUserIds()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	consumptionMap, err := sumCreditConsumptionByUsers(startDate, endDate, creditUserIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var creditConsumption float64
	for _, amount := range consumptionMap {
		creditConsumption += amount
	}

	// 剩余应收 / 未回款授信金额都是「全时间总额」口径，不随上面的时间筛选变化，单独算。
	receivable, outstanding, err := sumCreditReceivable(creditUserIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": creditStatsSummaryResp{
			RechargeAmount:    total.Recharge,
			GiftAmount:        total.Gift,
			CreditAmount:      creditAgg.TotalCredit,
			RepaymentAmount:   creditAgg.TotalRepayment,
			CreditConsumption: creditConsumption,
			Receivable:        receivable,
			OutstandingCredit: outstanding,
		},
	})
}

// GetCreditStatsTrend 趋势柱状图：按天或按月返回单一维度的金额序列。
// GET /api/credit_stats/trend?start_date=&end_date=&granularity=day|month&dimension=recharge|gift|credit|repayment
func GetCreditStatsTrend(c *gin.Context) {
	startDate, endDate, ok := parseCreditDateRange(c)
	if !ok {
		return
	}

	column, exists := creditTrendDimensionColumns[c.Query("dimension")]
	if !exists {
		column = creditTrendDimensionColumns["recharge"]
	}

	granularity := c.Query("granularity")
	if granularity != "month" {
		granularity = "day"
	}

	// 授信 / 回款只存在于 credit_records，直接按时间桶聚合即可，
	// 不需要「汇总表 + 今天 logs」那套拼接（新表本身就是实时的）。
	dimension := c.Query("dimension")
	if dimension == "credit" || dimension == "repayment" {
		recordType := model.CreditRecordTypeCredit
		if dimension == "repayment" {
			recordType = model.CreditRecordTypeRepayment
		}
		startTs, endTs := creditDateRangeUnix(startDate, endDate)
		bucketRows, err := model.SumCreditTrendBuckets(
			recordType, startTs, endTs, granularity == "month")
		if err != nil {
			common.ApiError(c, err)
			return
		}
		buckets := make([]string, 0, len(bucketRows))
		values := make([]float64, 0, len(bucketRows))
		for _, r := range bucketRows {
			buckets = append(buckets, r.Bucket)
			values = append(values, r.Amount)
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"granularity": granularity,
				"buckets":     buckets,
				"values":      values,
			},
		})
		return
	}

	// 按月时用 SUBSTR(stat_date, 1, 7) 取 YYYY-MM —— stat_date 是定长 varchar(10) 字符串，
	// SUBSTR 在 SQLite / MySQL / PostgreSQL 上语义一致，不需要按方言分支。
	bucketExpr := "stat_date"
	if granularity == "month" {
		bucketExpr = "SUBSTR(stat_date, 1, 7)"
	}

	// 历史桶走汇总表，今天那个桶（若在区间内）回 logs 实时补。
	histStart, histEnd, includeToday := splitCreditRange(startDate, endDate)

	type trendRow struct {
		Bucket string
		Value  float64
	}
	var rows []trendRow
	if histStart != "" {
		if err := model.DB.Model(&model.VipDailyConsumption{}).
			Where("stat_date >= ? AND stat_date <= ?", histStart, histEnd).
			Select(fmt.Sprintf("%s AS bucket, COALESCE(SUM(%s), 0) AS value", bucketExpr, column)).
			Group(bucketExpr).
			Order("bucket").
			Scan(&rows).Error; err != nil {
			common.ApiError(c, err)
			return
		}
	}

	buckets := make([]string, 0, len(rows)+1)
	values := make([]float64, 0, len(rows)+1)
	for _, r := range rows {
		buckets = append(buckets, r.Bucket)
		values = append(values, r.Value)
	}

	if includeToday {
		todayStart, todayEnd := todayUnixRange()
		todayAmounts, err := sumCreditAmountsFromLogs(todayStart, todayEnd, nil)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		todayValue := creditAmountByDimension(todayAmounts, column)
		todayBucket := time.Now().Format("2006-01-02")
		if granularity == "month" {
			// 按月粒度时今天并进当月那个桶：汇总表只统计到昨天，当月桶已存在就累加。
			todayBucket = todayBucket[:7]
		}
		if n := len(buckets); n > 0 && buckets[n-1] == todayBucket {
			values[n-1] += todayValue
		} else {
			buckets = append(buckets, todayBucket)
			values = append(values, todayValue)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"granularity": granularity,
			"buckets":     buckets,
			"values":      values,
		},
	})
}

// amountDetailRow 卡片明细弹窗一行
type amountDetailRow struct {
	UserId      int     `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Amount      float64 `json:"amount"`
}

// GetCreditAmountDetails 五张卡片（充值/赠送/授信/回款/消耗授信）的「查看明细」弹窗。
// GET /api/credit_stats/amount_details?dimension=recharge|gift|credit|repayment|consumption&start_date=&end_date=&page=&page_size=
// 返回给定时间范围内该维度金额 > 0 的用户，按金额从大到小排序。
func GetCreditAmountDetails(c *gin.Context) {
	dimension := c.Query("dimension")
	if dimension != "recharge" && dimension != "gift" && dimension != "credit" &&
		dimension != "repayment" && dimension != "consumption" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	startDate, endDate, ok := parseCreditDateRange(c)
	if !ok {
		return
	}
	page, pageSize := parseCreditPagination(c)

	// 按维度分流：授信/回款走新表，消耗走汇总表 quota + logs，充值/赠送走汇总表金额列 + logs
	var amountMap map[int]float64
	switch dimension {
	case "credit", "repayment":
		startTs, endTs := creditDateRangeUnix(startDate, endDate)
		recordType := model.CreditRecordTypeCredit
		if dimension == "repayment" {
			recordType = model.CreditRecordTypeRepayment
		}
		rows, err := model.SumCreditAmountByUserInRange(recordType, startTs, endTs)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		amountMap = make(map[int]float64, len(rows))
		for _, r := range rows {
			amountMap[r.UserId] = r.Amount
		}
	case "consumption":
		creditUserIds, err := model.GetCreditRecordUserIds()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		byUser, err := sumCreditConsumptionByUsers(startDate, endDate, creditUserIds)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		amountMap = make(map[int]float64, len(byUser))
		for uid, amount := range byUser {
			if amount > 0 {
				amountMap[uid] = amount
			}
		}
	default:
		// recharge / gift
		col := dimension + "_amount"
		byUser, err := sumRechargeOrGiftByUserInRange(startDate, endDate, col)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		amountMap = byUser
	}

	if len(amountMap) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"rows":      []amountDetailRow{},
				"total":     int64(0),
				"page":      page,
				"page_size": pageSize,
			},
		})
		return
	}

	// 按金额从大到小排序（内存排序，因为数据已经是聚合后的用户数级别）
	type sortEntry struct {
		userId int
		amount float64
	}
	entries := make([]sortEntry, 0, len(amountMap))
	for uid, amt := range amountMap {
		entries = append(entries, sortEntry{uid, amt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].amount > entries[j].amount
	})

	// 分页
	total := int64(len(entries))
	start := (page - 1) * pageSize
	if start > len(entries) {
		start = len(entries)
	}
	end := start + pageSize
	if end > len(entries) {
		end = len(entries)
	}
	pageEntries := entries[start:end]

	// 查用户信息
	userIds := make([]int, len(pageEntries))
	for i, e := range pageEntries {
		userIds[i] = e.userId
	}
	var users []userRow
	if err := model.DB.Model(&model.User{}).
		Where("id IN ?", userIds).
		Select("id, username, display_name").
		Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	userMap := make(map[int]userRow, len(users))
	for _, u := range users {
		userMap[u.Id] = u
	}

	// 拼装行（保持排序）
	rows := make([]amountDetailRow, 0, len(pageEntries))
	for _, e := range pageEntries {
		u := userMap[e.userId]
		rows = append(rows, amountDetailRow{
			UserId:      e.userId,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Amount:      e.amount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"rows":      rows,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// creditRepaymentRequest 回款弹窗提交体
type creditRepaymentRequest struct {
	UserId int     `json:"user_id"`
	Amount float64 `json:"amount"` // 回款金额（¥），必填且 > 0
	Remark string  `json:"remark"`
}

// maxCreditRepaymentAmount 单笔回款金额上限（¥）。
// 回款只记账不加额度，但金额仍会进入报表汇总，设上限避免误输入的天文数字污染统计。
const maxCreditRepaymentAmount = 1e9

// CreateCreditRepayment 登记一笔授信回款：往 logs 写一条 quota_type=回款 的记录。
// 该日志 quota=0，不改变用户余额，只参与「授信-回款」报表口径。
// POST /api/credit_stats/repayment
func CreateCreditRepayment(c *gin.Context) {
	var req creditRepaymentRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Amount <= 0 || req.Amount > maxCreditRepaymentAmount {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	user, err := model.GetUserById(req.UserId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	remark := strings.TrimSpace(req.Remark)
	if err := model.CreateCreditRecord(&model.CreditRecord{
		UserId:     user.Id,
		Type:       model.CreditRecordTypeRepayment,
		Amount:     req.Amount,
		Remark:     remark,
		OperatorId: c.GetInt("id"),
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, user.Id, "user.credit_repayment", map[string]interface{}{
		"amount": req.Amount,
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// creditBackfillRequest 历史数据补录提交体
type creditBackfillRequest struct {
	UserId     int     `json:"user_id"`
	Type       int     `json:"type"`        // 1=授信 2=回款
	Amount     float64 `json:"amount"`      // 金额（¥），必填且 > 0
	OccurredAt string  `json:"occurred_at"` // 发生日期 YYYY-MM-DD，必填
	Remark     string  `json:"remark"`
}

// CreateCreditBackfill 补录历史授信 / 回款记录。
//
// 用于功能上线前已在线下发生、额度也早已手工加过的那批数据：
// 只往 credit_records 写一行流水，**不触碰用户额度**，否则会重复加钱。
// 与 CreateCreditRepayment 的区别是允许指定发生日期 —— 历史数据如果都落在
// 补录当天，月度明细的逐月滚动（月初/月末未回款）就会全错。
// POST /api/credit_stats/backfill
func CreateCreditBackfill(c *gin.Context) {
	var req creditBackfillRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Type != model.CreditRecordTypeCredit && req.Type != model.CreditRecordTypeRepayment {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Amount <= 0 || req.Amount > maxCreditRepaymentAmount {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !isCreditDate(req.OccurredAt) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	occurred, err := time.ParseInLocation("2006-01-02", req.OccurredAt, time.Local)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// 不允许补录未来日期：那不是"历史数据"，多半是手误输错年份。
	if occurred.After(time.Now()) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	user, err := model.GetUserById(req.UserId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 落在当天 12:00 而不是 00:00：按天聚合时用本地时区换算，
	// 取正午可以避开夏令时/时区偏移把日期甩到前一天的边界问题。
	occurredAt := occurred.Add(12 * time.Hour).Unix()

	if err := model.CreateCreditRecord(&model.CreditRecord{
		UserId:     user.Id,
		Type:       req.Type,
		Amount:     req.Amount,
		Remark:     strings.TrimSpace(req.Remark),
		OperatorId: c.GetInt("id"),
		CreatedAt:  occurredAt,
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, user.Id, "user.credit_backfill", map[string]interface{}{
		"type":        req.Type,
		"amount":      req.Amount,
		"occurred_at": req.OccurredAt,
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// creditRecordRow 操作记录弹窗的一行
type creditRecordRow struct {
	CreatedAt int64   `json:"created_at"` // unix 秒，前端自行格式化
	QuotaType string  `json:"quota_type"` // 授信 / 回款
	Amount    float64 `json:"amount"`     // 金额（¥）
	Remark    string  `json:"remark"`
}

// GetCreditRepaymentRecords 操作记录弹窗：某个用户的授信 / 回款流水，按月份区间过滤。
// GET /api/credit_stats/records?user_id=&type=credit|repayment&start_month=YYYY-MM&end_month=YYYY-MM&page=&page_size=
func GetCreditRepaymentRecords(c *gin.Context) {
	userId := common.String2Int(c.Query("user_id"))
	if userId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 弹窗的两个标签分别对应授信记录与回款记录；缺省按授信展示。
	recordType := model.CreditRecordTypeCredit
	quotaType := model.QuotaTypeCredit
	if c.Query("type") == "repayment" {
		recordType = model.CreditRecordTypeRepayment
		quotaType = model.QuotaTypeRepayment
	}

	// 月份区间 → unix 秒；两侧都可缺省。
	startTs, _ := parseCreditMonthStart(c, "start_month")
	endTs, _ := parseCreditMonthEnd(c, "end_month")

	page, pageSize := parseCreditPagination(c)
	records, total, err := model.ListCreditRecords(
		userId, recordType, startTs, endTs, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	rows := make([]creditRecordRow, 0, len(records))
	for _, r := range records {
		rows = append(rows, creditRecordRow{
			CreatedAt: r.CreatedAt,
			QuotaType: quotaType,
			Amount:    r.Amount,
			Remark:    r.Remark,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"rows":      rows,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// parseCreditDateRange 读取并校验 start_date / end_date（YYYY-MM-DD）。
// 校验失败时已经写过响应，调用方直接 return。
func parseCreditDateRange(c *gin.Context) (string, string, bool) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if !isCreditDate(startDate) || !isCreditDate(endDate) || startDate > endDate {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return "", "", false
	}
	return startDate, endDate, true
}

// isCreditDate 校验 YYYY-MM-DD 形式的日期字符串。
func isCreditDate(value string) bool {
	if value == "" {
		return false
	}
	_, err := time.ParseInLocation("2006-01-02", value, time.Local)
	return err == nil
}

// parseCreditMonthStart 把 YYYY-MM 解析成该月第一天 00:00:00 的 unix 秒。
func parseCreditMonthStart(c *gin.Context, key string) (int64, bool) {
	value := c.Query(key)
	if value == "" {
		return 0, false
	}
	t, err := time.ParseInLocation("2006-01", value, time.Local)
	if err != nil {
		return 0, false
	}
	return t.Unix(), true
}

// parseCreditMonthEnd 把 YYYY-MM 解析成该月最后一秒的 unix 秒。
func parseCreditMonthEnd(c *gin.Context, key string) (int64, bool) {
	value := c.Query(key)
	if value == "" {
		return 0, false
	}
	t, err := time.ParseInLocation("2006-01", value, time.Local)
	if err != nil {
		return 0, false
	}
	return t.AddDate(0, 1, 0).Add(-time.Second).Unix(), true
}

// creditDateRangeUnix 把 [startDate, endDate]（YYYY-MM-DD）换算成闭区间的 unix 秒，
// 供需要按 created_at 过滤 credit_records 的查询使用。
func creditDateRangeUnix(startDate, endDate string) (int64, int64) {
	start, _ := time.ParseInLocation("2006-01-02", startDate, time.Local)
	end, _ := time.ParseInLocation("2006-01-02", endDate, time.Local)
	return start.Unix(), end.AddDate(0, 0, 1).Add(-time.Second).Unix()
}

// parseCreditPagination 读取 page / page_size，并夹到合理区间。
func parseCreditPagination(c *gin.Context) (int, int) {
	page := common.String2Int(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize := common.String2Int(c.Query("page_size"))
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// =========================================================
// 今日实时补数
//
// vip_daily_consumptions 由 00:10 的 cron 写入「昨天」，今天那一行要等次日凌晨才存在。
// 所以凡是查询区间覆盖今天的，今天这一段必须回到 logs 实时聚合，再和汇总表的历史
// 部分相加，否则「今天」档位全是 0、刚登记的回款也要隔夜才出现在卡片上。
//
// 切分规则（splitCreditRange）：
//   区间完全在今天之前          → 只查汇总表
//   区间包含今天（end >= today）→ 汇总表查 [start, 昨天]，logs 查今天 [00:00, now]
//   区间起点就是今天            → 只查 logs
// 未来日期不补：cron 没跑过、logs 也不会有，查出来恒为 0。
// =========================================================

// creditAmounts 四个金额口径（¥），汇总表与 logs 两条取数路径共用。
type creditAmounts struct {
	Recharge  float64
	Gift      float64
	Credit    float64
	Repayment float64
}

func (a *creditAmounts) add(other creditAmounts) {
	a.Recharge += other.Recharge
	a.Gift += other.Gift
	a.Credit += other.Credit
	a.Repayment += other.Repayment
}

// sumCreditReceivable 返回两个「全时间总额」口径的卡片值：剩余应收、未回款授信金额。
//
//	剩余应收         = 所有授信用户的累计消耗 - 总回款，不为负
//	未回款授信金额   = 总授信 - 总回款，不为负
//
// 两者都不参与时间筛选，共用同一次 credit_records 的两列聚合。
// creditUserIds 是有过任何授信 / 回款记录的用户（GetCreditRecordUserIds），与授信表格同一口径。
func sumCreditReceivable(creditUserIds []int) (receivable, outstanding float64, err error) {
	totals, err := model.SumCreditTotals()
	if err != nil {
		return 0, 0, err
	}
	if outstanding = totals.TotalCredit - totals.TotalRepayment; outstanding < 0 {
		outstanding = 0
	}

	consumptionMap, err := sumCreditConsumptionByUsers("", time.Now().Format("2006-01-02"), creditUserIds)
	if err != nil {
		return 0, 0, err
	}
	var consumption float64
	for _, amount := range consumptionMap {
		consumption += amount
	}
	if receivable = consumption - totals.TotalRepayment; receivable < 0 {
		receivable = 0
	}
	return receivable, outstanding, nil
}

// sumCreditConsumptionByUsers 按用户聚合 [startDate, endDate] 内的净消耗，折算为 quota / QuotaPerUnit
// （与「新用户统计」总消耗额同口径，不乘支付价格）。startDate 为空表示不设起点（全时间）。
//
// 历史段走 vip_daily_consumptions（WHERE user_id IN (授信用户)，走 user_id 索引，
// 扫到的行数 = 授信用户数 × 天数，与表总量无关），今天那段回 logs 实时补。
func sumCreditConsumptionByUsers(startDate, endDate string, userIds []int) (map[int]float64, error) {
	quotaMap := make(map[int]int64)
	result := make(map[int]float64)
	if len(userIds) == 0 {
		return result, nil
	}
	today := time.Now().Format("2006-01-02")

	// 汇总表只写到昨天，endDate 落在今天或未来时截断到昨天。
	histEnd := endDate
	if histEnd >= today {
		histEnd = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	}
	if startDate <= histEnd {
		tx := model.DB.Model(&model.VipDailyConsumption{}).
			Where("user_id IN ?", userIds).
			Where("stat_date <= ?", histEnd)
		if startDate != "" {
			tx = tx.Where("stat_date >= ?", startDate)
		}
		type row struct {
			UserId int
			Total  int64
		}
		var rows []row
		if err := tx.Select("user_id, COALESCE(SUM(quota), 0) AS total").
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			quotaMap[r.UserId] += r.Total
		}
	}

	if startDate <= today && endDate >= today {
		todayStart, todayEnd := todayUnixRange()
		todayMap, err := sumTodayConsumptionByUsers(todayStart, todayEnd, userIds)
		if err != nil {
			return nil, err
		}
		for uid, quota := range todayMap {
			quotaMap[uid] += quota
		}
	}

	for uid, quota := range quotaMap {
		result[uid] = quotaToUSD(quota)
	}
	return result, nil
}

// sumTodayConsumptionByUsers 从 logs 实时按用户聚合净消耗 quota（扣除异步任务差额退款），
// 口径与 cron 落进 vip_daily_consumptions.quota 的一致。
func sumTodayConsumptionByUsers(startTs, endTs int64, userIds []int) (map[int]int64, error) {
	type row struct {
		UserId int
		Total  int64
	}
	var rows []row
	if err := model.LOG_DB.Model(&model.Log{}).
		Where("type IN ?", model.NetQuotaSumTypes()).
		Where("user_id IN ?", userIds).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Select("user_id, " + model.NetQuotaSumExpr() + " AS total").
		Group("user_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[int]int64, len(rows))
	for _, r := range rows {
		result[r.UserId] = r.Total
	}
	return result, nil
}

// creditAmountByDimension 按趋势图选中的汇总表列名取对应口径的金额，
// 让 logs 实时补的那一段和汇总表查的那一段走同一个维度。
func creditAmountByDimension(a creditAmounts, column string) float64 {
	// 只会收到充值 / 赠送：授信与回款在上层就已分流到 credit_records。
	if column == "gift_amount" {
		return a.Gift
	}
	return a.Recharge
}

// splitCreditRange 把 [startDate, endDate] 切成「汇总表区间」和「是否需要补今天」。
// histEnd 为空字符串表示不需要查汇总表（区间起点就是今天或更晚）。
func splitCreditRange(startDate, endDate string) (histStart, histEnd string, includeToday bool) {
	today := time.Now().Format("2006-01-02")
	if endDate < today {
		return startDate, endDate, false
	}
	includeToday = true
	if startDate >= today {
		return "", "", includeToday
	}
	// 汇总表只到昨天为止；endDate 落在未来时同样截断到昨天。
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	return startDate, yesterday, includeToday
}

// todayUnixRange 返回今天 [00:00:00, 当前时刻] 的 unix 秒区间。
func todayUnixRange() (int64, int64) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return start.Unix(), now.Unix()
}

// sumCreditAmountsFromDaily 从 vip_daily_consumptions 汇总四个金额口径。
// userIds 为 nil 表示全平台；长度 0 表示明确无匹配用户，直接返回零值。
func sumCreditAmountsFromDaily(startDate, endDate string, userIds []int) (creditAmounts, error) {
	var out creditAmounts
	if startDate == "" || userIds != nil && len(userIds) == 0 {
		return out, nil
	}
	type amountRow struct {
		TotalRecharge  float64
		TotalGift      float64
		TotalCredit    float64
		TotalRepayment float64
	}
	tx := model.DB.Model(&model.VipDailyConsumption{}).
		Where("stat_date >= ? AND stat_date <= ?", startDate, endDate)
	if userIds != nil {
		tx = tx.Where("user_id IN ?", userIds)
	}
	var row amountRow
	if err := tx.Select("COALESCE(SUM(recharge_amount), 0) AS total_recharge, " +
		"COALESCE(SUM(gift_amount), 0) AS total_gift, " +
		"COALESCE(SUM(credit_amount), 0) AS total_credit, " +
		"COALESCE(SUM(repayment_amount), 0) AS total_repayment").
		Scan(&row).Error; err != nil {
		return out, err
	}
	out = creditAmounts{
		Recharge:  row.TotalRecharge,
		Gift:      row.TotalGift,
		Credit:    row.TotalCredit,
		Repayment: row.TotalRepayment,
	}
	return out, nil
}

// sumCreditAmountsFromLogs 直接从 logs 实时聚合四个金额口径，用于今天这一段。
// 口径与 cron 落盘完全一致（同一个 CASE WHEN 拆分），保证两段相加不会串台或重复。
func sumCreditAmountsFromLogs(startTs, endTs int64, userIds []int) (creditAmounts, error) {
	var out creditAmounts
	if userIds != nil && len(userIds) == 0 {
		return out, nil
	}
	type amountRow struct {
		TotalRecharge  float64
		TotalCredit    float64
		TotalGift      float64
		TotalRepayment float64
	}
	selectExpr, selectArgs := model.ManageQuotaAmountsSelect(false)
	// ManageQuotaAmountsSelect 带 user_id 前缀供 GROUP BY 用，这里只要总量，去掉它。
	selectExpr = strings.TrimPrefix(selectExpr, "user_id, ")

	tx := model.LOG_DB.Model(&model.Log{}).
		Where("type = ?", model.LogTypeManage).
		Where("operation_type = ?", model.OperationTypeQuota).
		Where("quota_type IN ?", model.ManageQuotaAmountTypes()).
		Where("user_id > 0").
		Where("created_at >= ? AND created_at <= ?", startTs, endTs)
	if userIds != nil {
		tx = tx.Where("user_id IN ?", userIds)
	}
	var row amountRow
	if err := tx.Select(selectExpr, selectArgs...).Scan(&row).Error; err != nil {
		return out, err
	}
	out = creditAmounts{
		Recharge:  row.TotalRecharge,
		Gift:      row.TotalGift,
		Credit:    row.TotalCredit,
		Repayment: row.TotalRepayment,
	}
	return out, nil
}

// sumRechargeOrGiftByUserInRange 按用户聚合充值或赠送在 [startDate, endDate] 内的金额，
// 供卡片「查看明细」弹窗使用。历史来自 vip_daily_consumptions，今天补 logs。
// dimension 只能是 "recharge_amount" 或 "gift_amount"。
func sumRechargeOrGiftByUserInRange(startDate, endDate, dimension string) (map[int]float64, error) {
	result := make(map[int]float64)
	if startDate == "" || endDate == "" {
		return result, nil
	}

	// 历史段：从汇总表按用户聚合
	histStart, histEnd, includeToday := splitCreditRange(startDate, endDate)
	type histRow struct {
		UserId int
		Amount float64
	}
	var histRows []histRow
	if histStart != "" {
		if err := model.DB.Model(&model.VipDailyConsumption{}).
			Where("stat_date >= ? AND stat_date <= ?", histStart, histEnd).
			Select("user_id, COALESCE(SUM(" + dimension + "), 0) AS amount").
			Group("user_id").
			Having("COALESCE(SUM(" + dimension + "), 0) > 0").
			Scan(&histRows).Error; err != nil {
			return nil, err
		}
		for _, r := range histRows {
			result[r.UserId] = r.Amount
		}
	}

	// 今天：logs 按用户聚合
	if includeToday {
		todayStart, todayEnd := todayUnixRange()
		var quotaType string
		switch dimension {
		case "recharge_amount":
			quotaType = model.QuotaTypeRecharge
		case "gift_amount":
			quotaType = model.QuotaTypeGift
		default:
			return nil, fmt.Errorf("invalid dimension for recharge/gift: %s", dimension)
		}

		type todayRow struct {
			UserId int
			Amount float64
		}
		var todayRows []todayRow
		if err := model.LOG_DB.Model(&model.Log{}).
			Where("type = ?", model.LogTypeManage).
			Where("operation_type = ?", model.OperationTypeQuota).
			Where("quota_type = ?", quotaType).
			Where("user_id > 0").
			Where("created_at >= ? AND created_at <= ?", todayStart, todayEnd).
			Select("user_id, COALESCE(SUM(recharge_input_amount), 0) AS amount").
			Group("user_id").
			Having("COALESCE(SUM(recharge_input_amount), 0) > 0").
			Scan(&todayRows).Error; err != nil {
			return nil, err
		}
		for _, r := range todayRows {
			result[r.UserId] += r.Amount
		}
	}

	return result, nil
}

// =========================================================
// 汇总表（用户维度）
// =========================================================

// creditSummaryRow 汇总表一行：按用户汇总全时间范围的授信与回款
type creditSummaryRow struct {
	UserId             int     `json:"user_id"`
	Username           string  `json:"username"`
	DisplayName        string  `json:"display_name"`
	BusinessChannel    string  `json:"business_channel"`     // 归属渠道（inviter.business_channel）
	InviterDisplayName string  `json:"inviter_display_name"` // 归属销售（inviter.display_name）
	TotalCredit        float64 `json:"total_credit"`         // 总授信金额（¥）
	TotalRepayment     float64 `json:"total_repayment"`      // 总回款金额（¥）
	Outstanding        float64 `json:"outstanding"`          // 未回款授信金额（¥）
	TotalConsumption   float64 `json:"total_consumption"`    // 总授信消耗金额（全时间，quota/QuotaPerUnit）
	Receivable         float64 `json:"receivable"`           // 剩余应收金额 = 总授信消耗 - 总回款，不为负
}

// creditSummarySortKeys 汇总表可排序的列（前端 sort_by 取值）→ 取值函数。
// 白名单之外的 sort_by 一律回落到默认的 id DESC。
var creditSummarySortKeys = map[string]func(r creditSummaryRow) float64{
	"total_credit":    func(r creditSummaryRow) float64 { return r.TotalCredit },
	"total_repayment": func(r creditSummaryRow) float64 { return r.TotalRepayment },
	"outstanding":     func(r creditSummaryRow) float64 { return r.Outstanding },
	"receivable":      func(r creditSummaryRow) float64 { return r.Receivable },
}

// GetCreditDetailsSummary 汇总表：按用户维度展示授信与回款汇总（全时间口径）。
// GET /api/credit_stats/details_summary?username=&channel=&sales=&sort_by=&sort_dir=asc|desc&page=&page_size=
//
// 排序列（剩余应收等）是聚合后才算得出的值，没法下推到 SQL，所以先对整个筛选范围
// 组装出全部行，内存排序后再分页。候选用户 = 有过授信 / 回款流水的用户，量级只有几十。
func GetCreditDetailsSummary(c *gin.Context) {
	username := strings.TrimSpace(c.Query("username"))
	channels := splitCSV(c.Query("channel"))
	salesList := splitCSV(c.Query("sales"))
	sortValue, sortable := creditSummarySortKeys[c.Query("sort_by")]
	sortAsc := c.Query("sort_dir") == "asc"
	page, pageSize := parseCreditPagination(c)

	// 1. 候选行 = 有过授信/回款流水的用户，再按用户名与归属筛选。
	recordUserIds, err := model.GetCreditRecordUserIds()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(recordUserIds) == 0 {
		writeCreditSummaryEmpty(c, page, pageSize)
		return
	}

	userTx, ok := applyCreditUserFilter(c, username, channels, salesList)
	if !ok {
		return
	}
	if userTx == nil {
		writeCreditSummaryEmpty(c, page, pageSize)
		return
	}

	var users []userRow
	if err := userTx.Where("id IN ?", recordUserIds).
		Select("id, username, display_name, inviter_id").
		Order("id DESC").
		Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if len(users) == 0 {
		writeCreditSummaryEmpty(c, page, pageSize)
		return
	}
	matchedIds := make([]int, len(users))
	for i, u := range users {
		matchedIds[i] = u.Id
	}

	// 2. 授信 / 回款来自 credit_records，消耗为全时间（汇总表 + 今天 logs）
	amountMap, err := model.SumCreditByUsers(matchedIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	consumptionMap, err := sumCreditConsumptionByUsers("", time.Now().Format("2006-01-02"), matchedIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 3. 组装全部行；汇总条统计整个筛选范围（不是当前页）
	inviterDisplayMap, inviterChannelMap := lookupInviters(users)
	allRows := make([]creditSummaryRow, 0, len(users))
	var summaryCredit, summaryRepayment, summaryOutstanding, summaryConsumption, summaryReceivable float64
	for _, u := range users {
		amt := amountMap[u.Id]
		outstanding := amt.TotalCredit - amt.TotalRepayment
		if outstanding < 0 {
			outstanding = 0
		}
		consumption := consumptionMap[u.Id]
		receivable := consumption - amt.TotalRepayment
		if receivable < 0 {
			receivable = 0
		}
		summaryCredit += amt.TotalCredit
		summaryRepayment += amt.TotalRepayment
		summaryOutstanding += outstanding
		summaryConsumption += consumption
		summaryReceivable += receivable
		allRows = append(allRows, creditSummaryRow{
			UserId:             u.Id,
			Username:           u.Username,
			DisplayName:        u.DisplayName,
			BusinessChannel:    inviterChannelMap[u.InviterId],
			InviterDisplayName: inviterDisplayMap[u.InviterId],
			TotalCredit:        amt.TotalCredit,
			TotalRepayment:     amt.TotalRepayment,
			Outstanding:        outstanding,
			TotalConsumption:   consumption,
			Receivable:         receivable,
		})
	}

	// 4. 排序：值相同时保持 id DESC 的原始顺序，翻页时行不会跳动
	if sortable {
		sort.SliceStable(allRows, func(i, j int) bool {
			if sortAsc {
				return sortValue(allRows[i]) < sortValue(allRows[j])
			}
			return sortValue(allRows[i]) > sortValue(allRows[j])
		})
	}

	total := int64(len(allRows))
	start := (page - 1) * pageSize
	if start > len(allRows) {
		start = len(allRows)
	}
	end := start + pageSize
	if end > len(allRows) {
		end = len(allRows)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"rows":                allRows[start:end],
			"total":               total,
			"page":                page,
			"page_size":           pageSize,
			"summary_credit":      summaryCredit,
			"summary_repayment":   summaryRepayment,
			"summary_outstanding": summaryOutstanding,
			"summary_consumption": summaryConsumption,
			"summary_receivable":  summaryReceivable,
		},
	})
}

// writeCreditSummaryEmpty 返回汇总表的空结果。
func writeCreditSummaryEmpty(c *gin.Context, page, pageSize int) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"rows":                []creditSummaryRow{},
			"total":               int64(0),
			"page":                page,
			"page_size":           pageSize,
			"summary_credit":      0.0,
			"summary_repayment":   0.0,
			"summary_outstanding": 0.0,
			"summary_consumption": 0.0,
			"summary_receivable":  0.0,
		},
	})
}

// applyCreditUserFilter 构造带「用户名 / 归属渠道 / 归属销售」筛选的 users 查询。
// 归属语义与新用户统计页一致：筛的是「邀请人属某渠道 / 是某销售」，不是用户本人。
// 返回 (nil, true) 表示筛选条件明确命中不了任何用户；(nil, false) 表示已写过错误响应。
func applyCreditUserFilter(c *gin.Context, username string, channels, salesList []string) (*gorm.DB, bool) {
	tx := model.DB.Model(&model.User{})
	if username != "" {
		tx = tx.Where("username LIKE ? OR display_name LIKE ?",
			"%"+username+"%", "%"+username+"%")
	}
	if len(channels) == 0 && len(salesList) == 0 {
		return tx, true
	}

	inviterTx := model.DB.Model(&model.User{}).Where("business_channel <> ''")
	if len(channels) > 0 {
		inviterTx = inviterTx.Where("business_channel IN ?", channels)
	}
	if len(salesList) > 0 {
		inviterTx = inviterTx.Where("display_name IN ?", salesList)
	}
	var inviterIds []int
	if err := inviterTx.Pluck("id", &inviterIds).Error; err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if len(inviterIds) == 0 {
		return nil, true
	}
	return tx.Where("inviter_id IN ?", inviterIds), true
}

// =========================================================
// 明细表（用户 + 月份维度）
// =========================================================

// creditMonthlyRow 明细表一行：用户 × 月份
type creditMonthlyRow struct {
	Month                string  `json:"month"` // YYYY-MM
	UserId               int     `json:"user_id"`
	Username             string  `json:"username"`
	DisplayName          string  `json:"display_name"`
	BusinessChannel      string  `json:"business_channel"`
	InviterDisplayName   string  `json:"inviter_display_name"`
	BeginningOutstanding float64 `json:"beginning_outstanding"` // 月初未回款授信金额
	MonthCredit          float64 `json:"month_credit"`          // 本月授信金额
	MonthRepayment       float64 `json:"month_repayment"`       // 本月回款金额
	EndingOutstanding    float64 `json:"ending_outstanding"`    // 月末未回款授信金额
	MonthConsumption     float64 `json:"month_consumption"`     // 本月授信消耗金额（quota/QuotaPerUnit）
}

// GetCreditDetailsMonthly 明细表：按用户 + 月份维度展示授信与回款明细。
// GET /api/credit_stats/details_monthly?start_month=YYYY-MM&end_month=YYYY-MM&username=&channel=&sales=&page=&page_size=
func GetCreditDetailsMonthly(c *gin.Context) {
	startMonth := strings.TrimSpace(c.Query("start_month"))
	endMonth := strings.TrimSpace(c.Query("end_month"))
	username := strings.TrimSpace(c.Query("username"))
	channels := splitCSV(c.Query("channel"))
	salesList := splitCSV(c.Query("sales"))
	page, pageSize := parseCreditPagination(c)

	if !isCreditMonth(startMonth) || !isCreditMonth(endMonth) || startMonth > endMonth {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 1. 候选行 = 有过授信/回款流水的用户，再按用户名与归属筛选。
	recordUserIds, err := model.GetCreditRecordUserIds()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(recordUserIds) == 0 {
		writeCreditMonthlyEmpty(c, page, pageSize)
		return
	}

	userTx, ok := applyCreditUserFilter(c, username, channels, salesList)
	if !ok {
		return
	}
	if userTx == nil {
		writeCreditMonthlyEmpty(c, page, pageSize)
		return
	}

	var users []userRow
	if err := userTx.Where("id IN ?", recordUserIds).
		Select("id, username, display_name, inviter_id").
		Order("id DESC").
		Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if len(users) == 0 {
		writeCreditMonthlyEmpty(c, page, pageSize)
		return
	}
	userIds := make([]int, len(users))
	for i, u := range users {
		userIds[i] = u.Id
	}

	// 2. 区间内按 (用户, 月份) 聚合
	rangeStartTs, rangeEndTs := creditMonthRangeUnix(startMonth, endMonth)
	monthlyRows, err := model.SumCreditByUserMonth(rangeStartTs, rangeEndTs, userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	amountMap := make(map[creditMonthKey]model.CreditMonthlyAggregate, len(monthlyRows))
	for _, r := range monthlyRows {
		amountMap[creditMonthKey{r.Month, r.UserId}] = r
	}

	// 3. 区间之前的累计，作为第一个月的「月初未回款」基线
	historyMap, err := model.SumCreditBeforeByUsers(rangeStartTs, userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 3b. 区间内按 (用户, 月份) 聚合的消耗
	consumptionMap, err := sumCreditConsumptionByUserMonth(startMonth, endMonth, userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 4. 展开 users × months 逐月滚动：上月月末即本月月初
	inviterDisplayMap, inviterChannelMap := lookupInviters(users)
	months := expandMonths(startMonth, endMonth)
	allRows := make([]creditMonthlyRow, 0, len(users)*len(months))
	for _, u := range users {
		hist := historyMap[u.Id]
		runningCredit := hist.TotalCredit
		runningRepayment := hist.TotalRepayment

		for _, m := range months {
			amt := amountMap[creditMonthKey{m, u.Id}]
			beginningOut := runningCredit - runningRepayment
			if beginningOut < 0 {
				beginningOut = 0
			}
			runningCredit += amt.TotalCredit
			runningRepayment += amt.TotalRepayment
			endingOut := runningCredit - runningRepayment
			if endingOut < 0 {
				endingOut = 0
			}

			// 本月无授信、无回款、无消耗，且月初月末都没有欠款时，这一行没有信息量，略过
			consumption := consumptionMap[creditMonthKey{m, u.Id}]
			if amt.TotalCredit == 0 && amt.TotalRepayment == 0 && consumption == 0 &&
				beginningOut == 0 && endingOut == 0 {
				continue
			}

			allRows = append(allRows, creditMonthlyRow{
				Month:                m,
				UserId:               u.Id,
				Username:             u.Username,
				DisplayName:          u.DisplayName,
				BusinessChannel:      inviterChannelMap[u.InviterId],
				InviterDisplayName:   inviterDisplayMap[u.InviterId],
				BeginningOutstanding: beginningOut,
				MonthCredit:          amt.TotalCredit,
				MonthRepayment:       amt.TotalRepayment,
				EndingOutstanding:    endingOut,
				MonthConsumption:     consumption,
			})
		}
	}

	// 5. 汇总条统计整个筛选范围（所有行），分页只影响返回的那一页。
	var summaryCredit, summaryRepayment, summaryConsumption float64
	for _, r := range allRows {
		summaryCredit += r.MonthCredit
		summaryRepayment += r.MonthRepayment
		summaryConsumption += r.MonthConsumption
	}

	total := int64(len(allRows))
	start := (page - 1) * pageSize
	if start > len(allRows) {
		start = len(allRows)
	}
	end := start + pageSize
	if end > len(allRows) {
		end = len(allRows)
	}
	pageRows := allRows[start:end]

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"rows":                pageRows,
			"total":               total,
			"page":                page,
			"page_size":           pageSize,
			"summary_credit":      summaryCredit,
			"summary_repayment":   summaryRepayment,
			"summary_consumption": summaryConsumption,
		},
	})
}

// writeCreditMonthlyEmpty 返回月度明细表的空结果。
func writeCreditMonthlyEmpty(c *gin.Context, page, pageSize int) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"rows":                []creditMonthlyRow{},
			"total":               int64(0),
			"page":                page,
			"page_size":           pageSize,
			"summary_credit":      0.0,
			"summary_repayment":   0.0,
			"summary_consumption": 0.0,
		},
	})
}

// creditMonthKey 月度明细表里 (月份, 用户) 维度的聚合键。
type creditMonthKey struct {
	month  string // YYYY-MM
	userId int
}

// sumCreditConsumptionByUserMonth 按 (月份, 用户) 聚合 [startMonth, endMonth] 内的净消耗，
// 折算为 quota / QuotaPerUnit。历史走 vip_daily_consumptions，今天（若落在区间内）回 logs 补进当月。
// SUBSTR(stat_date, 1, 7) 在 SQLite / MySQL / PostgreSQL 上语义一致（stat_date 是定长 YYYY-MM-DD 字符串）。
func sumCreditConsumptionByUserMonth(startMonth, endMonth string, userIds []int) (map[creditMonthKey]float64, error) {
	quotaMap := make(map[creditMonthKey]int64)
	result := make(map[creditMonthKey]float64)
	if len(userIds) == 0 {
		return result, nil
	}

	today := time.Now().Format("2006-01-02")
	histStart := startMonth + "-01"
	histEnd := endMonth + "-31" // stat_date 是字符串，-31 作为月末上界对任何月份都成立
	if histEnd >= today {
		histEnd = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	}
	if histStart <= histEnd {
		type row struct {
			Month  string
			UserId int
			Total  int64
		}
		var rows []row
		if err := model.DB.Model(&model.VipDailyConsumption{}).
			Where("user_id IN ?", userIds).
			Where("stat_date >= ? AND stat_date <= ?", histStart, histEnd).
			Select("SUBSTR(stat_date, 1, 7) AS month, user_id, COALESCE(SUM(quota), 0) AS total").
			Group("SUBSTR(stat_date, 1, 7), user_id").
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			quotaMap[creditMonthKey{r.Month, r.UserId}] += r.Total
		}
	}

	if currentMonth := today[:7]; currentMonth >= startMonth && currentMonth <= endMonth {
		todayStart, todayEnd := todayUnixRange()
		todayMap, err := sumTodayConsumptionByUsers(todayStart, todayEnd, userIds)
		if err != nil {
			return nil, err
		}
		for uid, quota := range todayMap {
			quotaMap[creditMonthKey{currentMonth, uid}] += quota
		}
	}

	for k, quota := range quotaMap {
		result[k] = quotaToUSD(quota)
	}
	return result, nil
}

// creditMonthRangeUnix 把 [startMonth, endMonth]（YYYY-MM）换算成闭区间的 unix 秒。
func creditMonthRangeUnix(startMonth, endMonth string) (int64, int64) {
	start, _ := time.ParseInLocation("2006-01", startMonth, time.Local)
	end, _ := time.ParseInLocation("2006-01", endMonth, time.Local)
	return start.Unix(), end.AddDate(0, 1, 0).Add(-time.Second).Unix()
}

// =========================================================
// 辅助函数
// =========================================================

// lookupInviters 批量反查 inviter 的 display_name 和 business_channel。
func lookupInviters(users []userRow) (map[int]string, map[int]string) {
	inviterDisplayMap := make(map[int]string)
	inviterChannelMap := make(map[int]string)

	inviterIdSet := make(map[int]struct{})
	for _, u := range users {
		if u.InviterId > 0 {
			inviterIdSet[u.InviterId] = struct{}{}
		}
	}
	if len(inviterIdSet) == 0 {
		return inviterDisplayMap, inviterChannelMap
	}

	ids := make([]int, 0, len(inviterIdSet))
	for id := range inviterIdSet {
		ids = append(ids, id)
	}

	type invRow struct {
		Id              int
		DisplayName     string
		BusinessChannel string
	}
	var invs []invRow
	if err := model.DB.Model(&model.User{}).
		Select("id, display_name, business_channel").
		Where("id IN ?", ids).
		Scan(&invs).Error; err == nil {
		for _, r := range invs {
			inviterChannelMap[r.Id] = r.BusinessChannel
			if r.BusinessChannel != "" {
				inviterDisplayMap[r.Id] = r.DisplayName
			}
		}
	}
	return inviterDisplayMap, inviterChannelMap
}

// isCreditMonth 校验 YYYY-MM 格式。
func isCreditMonth(value string) bool {
	if value == "" {
		return false
	}
	_, err := time.ParseInLocation("2006-01", value, time.Local)
	return err == nil
}

// expandMonths 返回 [startMonth, endMonth] 之间的所有月份（YYYY-MM）。
func expandMonths(startMonth, endMonth string) []string {
	start, _ := time.ParseInLocation("2006-01", startMonth, time.Local)
	end, _ := time.ParseInLocation("2006-01", endMonth, time.Local)
	var months []string
	for t := start; !t.After(end); t = t.AddDate(0, 1, 0) {
		months = append(months, t.Format("2006-01"))
	}
	return months
}

// userRow 声明挪到包级别供 lookupInviters 复用
type userRow struct {
	Id          int
	Username    string
	DisplayName string
	InviterId   int
}
