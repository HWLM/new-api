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
package model

import (
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
)

// CreditRecordType 区分一条记录是授信还是回款。
const (
	CreditRecordTypeCredit    = 1 // 授信：管理员「调整额度-授信」录入
	CreditRecordTypeRepayment = 2 // 回款：客户还款登记，不改变用户额度
)

// CreditRecord 授信 / 回款流水表。
//
// 为什么单独建表而不是复用 logs：
// 「充值-授信统计」页的授信明细区（汇总表、月度明细表、未回款授信金额）都是
// 授信与回款两个口径的聚合，logs 是全量请求日志级别的大表、且有留存清理，
// vip_daily_consumptions 按「用户 × 天」展开后同样会越来越大。
// 这张表只在管理员录入授信 / 登记回款时各写一行，数据量 = 业务操作次数，
// 聚合查询可以直接走 user_id / type 索引，不受上面两张表体量影响。
//
// 与 logs 的关系：授信仍然会照常写一条 quota_type=授信 的 logs（额度变动需要审计），
// 这张表是面向报表的冗余记录；回款只写这张表（不改额度，logs 不再写）。
type CreditRecord struct {
	Id     int     `json:"id" gorm:"primaryKey"`
	UserId int     `json:"user_id" gorm:"index;index:idx_credit_records_user_type,priority:1"`
	Type   int     `json:"type" gorm:"index:idx_credit_records_user_type,priority:2"` // 1=授信 2=回款
	// Amount 操作金额（人民币 ¥）。正数为发生额（新增授信 / 登记回款），
	// 负数为冲回：管理员「调整额度-减少」且类型=授信时写入负数，
	// 于是所有 SUM(amount) 聚合天然得到净额，不需要各自再减一次。
	Amount float64 `json:"amount"`
	Remark string  `json:"remark" gorm:"type:text"`
	// OperatorId 执行操作的管理员 id；0 表示来源不可考（历史回填数据）。
	OperatorId int   `json:"operator_id" gorm:"default:0"`
	CreatedAt  int64 `json:"created_at" gorm:"autoCreateTime;index"`
}

// CreditAggregate 一个用户的授信 / 回款聚合结果。
type CreditAggregate struct {
	UserId         int     `json:"user_id"`
	TotalCredit    float64 `json:"total_credit"`
	TotalRepayment float64 `json:"total_repayment"`
}

// creditSumSelect 构造按 type 拆分的金额聚合 SELECT 片段，以及占位符参数。
// 授信与回款共用 amount 列、靠 type 区分，一次扫描即可产出两个口径。
// CASE WHEN 是 SQL 标准语法，SQLite/MySQL/PostgreSQL 均支持。
func creditSumSelect(withUserId bool) (string, []interface{}) {
	expr := "COALESCE(SUM(CASE WHEN type = ? THEN amount ELSE 0 END), 0) AS total_credit, " +
		"COALESCE(SUM(CASE WHEN type = ? THEN amount ELSE 0 END), 0) AS total_repayment"
	if withUserId {
		expr = "user_id, " + expr
	}
	return expr, []interface{}{CreditRecordTypeCredit, CreditRecordTypeRepayment}
}

// CreateCreditRecord 写入一条授信 / 回款流水。
// amount 允许为负（授信冲回），但不允许为 0 —— 0 不改变任何口径，只会污染流水。
func CreateCreditRecord(record *CreditRecord) error {
	if record.Type != CreditRecordTypeCredit && record.Type != CreditRecordTypeRepayment {
		return fmt.Errorf("invalid credit record type: %d", record.Type)
	}
	if record.Amount == 0 {
		return fmt.Errorf("credit record amount must not be zero")
	}
	return DB.Create(record).Error
}

// CreateCreditRecordInTx 在已有事务中写入流水，供「授信同时变更额度」这类需要原子性的场景使用。
func CreateCreditRecordInTx(tx *gorm.DB, record *CreditRecord) error {
	if record.Type != CreditRecordTypeCredit && record.Type != CreditRecordTypeRepayment {
		return fmt.Errorf("invalid credit record type: %d", record.Type)
	}
	if record.Amount == 0 {
		return fmt.Errorf("credit record amount must not be zero")
	}
	return tx.Create(record).Error
}

// SumCreditTotals 返回全平台全时间的授信总额与回款总额。
// 这是「不参与时间筛选」的全量口径，供未回款授信金额与剩余应收两张卡片取数。
//
// 直接对 credit_records 做一次两列聚合即可 —— 这张表只在管理员录入授信 / 登记回款时
// 各写一行，数据量是业务操作次数级别，不会像 vip_daily_consumptions（用户 × 天）
// 或 logs（全量请求）那样随时间膨胀。
func SumCreditTotals() (CreditAggregate, error) {
	selectExpr, args := creditSumSelect(false)
	var row CreditAggregate
	err := DB.Model(&CreditRecord{}).
		Select(selectExpr, args...).
		Scan(&row).Error
	return row, err
}

// SumCreditInRange 聚合 [startTs, endTs] 内的授信与回款总额（全平台）。
// 供顶部卡片与趋势图按时间筛选取数使用。
func SumCreditInRange(startTs, endTs int64) (CreditAggregate, error) {
	selectExpr, args := creditSumSelect(false)
	var row CreditAggregate
	err := DB.Model(&CreditRecord{}).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Select(selectExpr, args...).
		Scan(&row).Error
	return row, err
}

// creditRecordRow 是授信 / 回款流水的原始行，供按时间分桶的聚合在 Go 侧使用。
type creditRecordRow struct {
	UserId    int
	Type      int
	Amount    float64
	CreatedAt int64
}

// loadCreditRecordRows 取出 [startTs, endTs] 内的原始流水行。
// userIds 语义同 SumCreditByUsers：nil 表示不限用户，长度 0 表示明确无匹配用户。
//
// 按天 / 按月分桶刻意放在 Go 侧（而不是交给 SQL 的 to_char/FROM_UNIXTIME）：
// 这些 SQL 时间函数按数据库会话时区换算，而 vip_daily_consumptions.stat_date
// 是按应用进程时区（TZ）算的。数据库时区与应用时区不一致时（例如 postgres 容器
// 默认 UTC、应用为 Asia/Shanghai），北京时间 00:00-07:59 的流水会被归到前一天 /
// 上个月，与消耗口径错开一天。这张表只有管理员操作级别的小数据量，内存分组无成本。
func loadCreditRecordRows(startTs, endTs int64, userIds []int) ([]creditRecordRow, error) {
	if userIds != nil && len(userIds) == 0 {
		return nil, nil
	}
	tx := DB.Model(&CreditRecord{}).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs)
	if userIds != nil {
		tx = tx.Where("user_id IN ?", userIds)
	}
	var rows []creditRecordRow
	err := tx.Select("user_id, type, amount, created_at").Scan(&rows).Error
	return rows, err
}

// creditPeriodKey 把 unix 秒转成应用本地时区下的 YYYY-MM-DD（layout 传 "2006-01-02"）
// 或 YYYY-MM（layout 传 "2006-01"）。
func creditPeriodKey(createdAt int64, layout string) string {
	return time.Unix(createdAt, 0).In(time.Local).Format(layout)
}

// CreditTrendBucket 趋势图的一个时间桶。
type CreditTrendBucket struct {
	Bucket string  `json:"bucket"` // 按天为 YYYY-MM-DD，按月为 YYYY-MM
	Amount float64 `json:"amount"`
}

// SumCreditTrendBuckets 按天或按月聚合单一口径（授信或回款）的金额，供趋势图使用。
// byMonth = true 时桶为 YYYY-MM，否则为 YYYY-MM-DD；结果按桶升序。
func SumCreditTrendBuckets(recordType int, startTs, endTs int64, byMonth bool) ([]CreditTrendBucket, error) {
	rows, err := loadCreditRecordRows(startTs, endTs, nil)
	if err != nil {
		return nil, err
	}
	layout := "2006-01-02"
	if byMonth {
		layout = "2006-01"
	}
	totals := make(map[string]float64)
	for _, r := range rows {
		if r.Type != recordType {
			continue
		}
		totals[creditPeriodKey(r.CreatedAt, layout)] += r.Amount
	}
	buckets := make([]CreditTrendBucket, 0, len(totals))
	for bucket, amount := range totals {
		buckets = append(buckets, CreditTrendBucket{Bucket: bucket, Amount: amount})
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Bucket < buckets[j].Bucket })
	return buckets, nil
}

// SumCreditByUsers 按用户聚合授信 / 回款总额（全时间）。
// userIds 为 nil 表示不限用户；长度为 0 表示明确无匹配用户，返回空 map。
func SumCreditByUsers(userIds []int) (map[int]CreditAggregate, error) {
	result := make(map[int]CreditAggregate)
	if userIds != nil && len(userIds) == 0 {
		return result, nil
	}
	selectExpr, args := creditSumSelect(true)
	tx := DB.Model(&CreditRecord{})
	if userIds != nil {
		tx = tx.Where("user_id IN ?", userIds)
	}
	var rows []CreditAggregate
	if err := tx.Select(selectExpr, args...).Group("user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.UserId] = r
	}
	return result, nil
}

// SumCreditByUsersInRange 按用户聚合 [startTs, endTs] 内的授信 / 回款总额。
// userIds 语义同 SumCreditByUsers：nil 表示不限用户，长度 0 表示明确无匹配用户。
func SumCreditByUsersInRange(startTs, endTs int64, userIds []int) (map[int]CreditAggregate, error) {
	result := make(map[int]CreditAggregate)
	if userIds != nil && len(userIds) == 0 {
		return result, nil
	}
	selectExpr, args := creditSumSelect(true)
	tx := DB.Model(&CreditRecord{}).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs)
	if userIds != nil {
		tx = tx.Where("user_id IN ?", userIds)
	}
	var rows []CreditAggregate
	if err := tx.Select(selectExpr, args...).Group("user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.UserId] = r
	}
	return result, nil
}

// CreditDailyAggregate 某用户在某一天的授信 / 回款聚合。
type CreditDailyAggregate struct {
	Day            string  `json:"day"` // YYYY-MM-DD
	UserId         int     `json:"user_id"`
	TotalCredit    float64 `json:"total_credit"`
	TotalRepayment float64 `json:"total_repayment"`
}

// SumCreditByUserDay 按 (用户, 天) 聚合 [startTs, endTs] 内的授信 / 回款，
// 供「按天统计」明细按日期维度取授信金额。userIds 语义同 SumCreditByUsers。
func SumCreditByUserDay(startTs, endTs int64, userIds []int) ([]CreditDailyAggregate, error) {
	rows, err := loadCreditRecordRows(startTs, endTs, userIds)
	if err != nil {
		return nil, err
	}
	type key struct {
		day    string
		userId int
	}
	totals := make(map[key]CreditDailyAggregate)
	for _, r := range rows {
		k := key{day: creditPeriodKey(r.CreatedAt, "2006-01-02"), userId: r.UserId}
		agg := totals[k]
		agg.Day = k.day
		agg.UserId = r.UserId
		switch r.Type {
		case CreditRecordTypeCredit:
			agg.TotalCredit += r.Amount
		case CreditRecordTypeRepayment:
			agg.TotalRepayment += r.Amount
		}
		totals[k] = agg
	}
	result := make([]CreditDailyAggregate, 0, len(totals))
	for _, agg := range totals {
		result = append(result, agg)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Day != result[j].Day {
			return result[i].Day < result[j].Day
		}
		return result[i].UserId < result[j].UserId
	})
	return result, nil
}

// CreditMonthlyAggregate 某用户在某个月份的授信 / 回款聚合。
type CreditMonthlyAggregate struct {
	Month          string  `json:"month"` // YYYY-MM
	UserId         int     `json:"user_id"`
	TotalCredit    float64 `json:"total_credit"`
	TotalRepayment float64 `json:"total_repayment"`
}

// SumCreditByUserMonth 按 (用户, 月份) 聚合授信 / 回款，用于月度明细表。
// startTs / endTs 为闭区间的 unix 秒；userIds 语义同 SumCreditByUsers。
// 分桶同样在 Go 侧按应用本地时区完成，理由见 loadCreditRecordRows。
func SumCreditByUserMonth(startTs, endTs int64, userIds []int) ([]CreditMonthlyAggregate, error) {
	rows, err := loadCreditRecordRows(startTs, endTs, userIds)
	if err != nil {
		return nil, err
	}
	type key struct {
		month  string
		userId int
	}
	totals := make(map[key]CreditMonthlyAggregate)
	for _, r := range rows {
		k := key{month: creditPeriodKey(r.CreatedAt, "2006-01"), userId: r.UserId}
		agg := totals[k]
		agg.Month = k.month
		agg.UserId = r.UserId
		switch r.Type {
		case CreditRecordTypeCredit:
			agg.TotalCredit += r.Amount
		case CreditRecordTypeRepayment:
			agg.TotalRepayment += r.Amount
		}
		totals[k] = agg
	}
	result := make([]CreditMonthlyAggregate, 0, len(totals))
	for _, agg := range totals {
		result = append(result, agg)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Month != result[j].Month {
			return result[i].Month < result[j].Month
		}
		return result[i].UserId < result[j].UserId
	})
	return result, nil
}

// SumCreditBeforeByUsers 聚合给定时间点之前的授信 / 回款总额，用于推算「月初未回款授信金额」。
func SumCreditBeforeByUsers(beforeTs int64, userIds []int) (map[int]CreditAggregate, error) {
	result := make(map[int]CreditAggregate)
	if userIds != nil && len(userIds) == 0 {
		return result, nil
	}
	selectExpr, args := creditSumSelect(true)
	tx := DB.Model(&CreditRecord{}).Where("created_at < ?", beforeTs)
	if userIds != nil {
		tx = tx.Where("user_id IN ?", userIds)
	}
	var rows []CreditAggregate
	if err := tx.Select(selectExpr, args...).Group("user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.UserId] = r
	}
	return result, nil
}

// GetCreditRecordUserIds 返回所有有过授信 / 回款记录的 user_id，供明细表确定候选行。
func GetCreditRecordUserIds() ([]int, error) {
	var ids []int
	err := DB.Model(&CreditRecord{}).
		Distinct().
		Pluck("user_id", &ids).Error
	return ids, err
}

// GetCreditedUserIds 返回所有授信净额 > 0 的 user_id（不含只有回款的用户）。
// 供新用户统计的「授信客户」标签与筛选判定使用。
//
// 用净额而不是「存在授信行」：授信后又全额冲回（减少额度-授信）的用户净额为 0，
// 业务上已经不是授信客户，不该继续带标签。
func GetCreditedUserIds() ([]int, error) {
	var ids []int
	err := DB.Model(&CreditRecord{}).
		Where("type = ?", CreditRecordTypeCredit).
		Group("user_id").
		Having("COALESCE(SUM(amount), 0) > 0").
		Pluck("user_id", &ids).Error
	return ids, err
}

// CreditUserAmount 某用户在给定时间窗内单一口径（授信或回款）的金额合计。
type CreditUserAmount struct {
	UserId int     `json:"user_id"`
	Amount float64 `json:"amount"`
}

// SumCreditAmountByUserInRange 按用户聚合某一口径在 [startTs, endTs] 内的金额合计，
// 供卡片「查看明细」弹窗使用。只返回金额 > 0 的用户（没有对应操作的商家不展示）。
func SumCreditAmountByUserInRange(recordType int, startTs, endTs int64) ([]CreditUserAmount, error) {
	var rows []CreditUserAmount
	err := DB.Model(&CreditRecord{}).
		Where("type = ?", recordType).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Select("user_id, COALESCE(SUM(amount), 0) AS amount").
		Group("user_id").
		Having("COALESCE(SUM(amount), 0) > 0").
		Scan(&rows).Error
	return rows, err
}

// ListCreditRecords 分页查询某用户的授信或回款流水（操作记录弹窗用）。
// startTs / endTs 传 0 表示该侧不限。
func ListCreditRecords(userId, recordType int, startTs, endTs int64, offset, limit int) ([]CreditRecord, int64, error) {
	tx := DB.Model(&CreditRecord{}).
		Where("user_id = ?", userId).
		Where("type = ?", recordType)
	if startTs > 0 {
		tx = tx.Where("created_at >= ?", startTs)
	}
	if endTs > 0 {
		tx = tx.Where("created_at <= ?", endTs)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []CreditRecord
	if err := tx.Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}
