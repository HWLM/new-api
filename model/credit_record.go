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

	"github.com/QuantumNous/new-api/common"

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
	Amount float64 `json:"amount"`                                                    // 操作金额（人民币 ¥），恒为正数
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
func CreateCreditRecord(record *CreditRecord) error {
	if record.Type != CreditRecordTypeCredit && record.Type != CreditRecordTypeRepayment {
		return fmt.Errorf("invalid credit record type: %d", record.Type)
	}
	if record.Amount <= 0 {
		return fmt.Errorf("credit record amount must be positive, got %v", record.Amount)
	}
	return DB.Create(record).Error
}

// CreateCreditRecordInTx 在已有事务中写入流水，供「授信同时变更额度」这类需要原子性的场景使用。
func CreateCreditRecordInTx(tx *gorm.DB, record *CreditRecord) error {
	if record.Type != CreditRecordTypeCredit && record.Type != CreditRecordTypeRepayment {
		return fmt.Errorf("invalid credit record type: %d", record.Type)
	}
	if record.Amount <= 0 {
		return fmt.Errorf("credit record amount must be positive, got %v", record.Amount)
	}
	return tx.Create(record).Error
}

// SumCreditOutstanding 返回全平台未回款授信金额 = 总授信 - 总回款。
// 按业务约定不为负，回款多于授信时返回 0。
// 该口径是全时间总额，不参与任何时间筛选。
func SumCreditOutstanding() (float64, error) {
	selectExpr, args := creditSumSelect(false)
	var row CreditAggregate
	if err := DB.Model(&CreditRecord{}).
		Select(selectExpr, args...).
		Scan(&row).Error; err != nil {
		return 0, err
	}
	if outstanding := row.TotalCredit - row.TotalRepayment; outstanding > 0 {
		return outstanding, nil
	}
	return 0, nil
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

// CreditTrendBucket 趋势图的一个时间桶。
type CreditTrendBucket struct {
	Bucket string  `json:"bucket"` // 按天为 YYYY-MM-DD，按月为 YYYY-MM
	Amount float64 `json:"amount"`
}

// creditDayExpr 返回把 created_at（unix 秒）转成 'YYYY-MM-DD' 的 SQL 表达式，方言同 creditMonthExpr。
func creditDayExpr() string {
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		return "to_char(to_timestamp(created_at), 'YYYY-MM-DD')"
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		return "DATE_FORMAT(FROM_UNIXTIME(created_at), '%Y-%m-%d')"
	default:
		return "strftime('%Y-%m-%d', created_at, 'unixepoch', 'localtime')"
	}
}

// SumCreditTrendBuckets 按天或按月聚合单一口径（授信或回款）的金额，供趋势图使用。
// byMonth = true 时桶为 YYYY-MM，否则为 YYYY-MM-DD；结果按桶升序。
func SumCreditTrendBuckets(recordType int, startTs, endTs int64, byMonth bool) ([]CreditTrendBucket, error) {
	bucketExpr := creditDayExpr()
	if byMonth {
		bucketExpr = creditMonthExpr()
	}
	var rows []CreditTrendBucket
	err := DB.Model(&CreditRecord{}).
		Where("type = ?", recordType).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs).
		Select(bucketExpr+" AS bucket, COALESCE(SUM(amount), 0) AS amount").
		Group(bucketExpr).
		Order("bucket").
		Scan(&rows).Error
	return rows, err
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

// CreditMonthlyAggregate 某用户在某个月份的授信 / 回款聚合。
type CreditMonthlyAggregate struct {
	Month          string  `json:"month"` // YYYY-MM
	UserId         int     `json:"user_id"`
	TotalCredit    float64 `json:"total_credit"`
	TotalRepayment float64 `json:"total_repayment"`
}

// creditMonthExpr 返回把 created_at（unix 秒）转成 'YYYY-MM' 的 SQL 表达式。
// 三种数据库的时间函数不通用，必须按方言分支：
//   - SQLite：strftime + unixepoch 修饰符
//   - MySQL：FROM_UNIXTIME + DATE_FORMAT
//   - PostgreSQL：to_timestamp + to_char
//
// 统一按数据库服务器的本地时区换算，与 vip_daily_consumptions 的 stat_date 口径一致。
func creditMonthExpr() string {
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		return "to_char(to_timestamp(created_at), 'YYYY-MM')"
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		return "DATE_FORMAT(FROM_UNIXTIME(created_at), '%Y-%m')"
	default:
		return "strftime('%Y-%m', created_at, 'unixepoch', 'localtime')"
	}
}

// SumCreditByUserMonth 按 (用户, 月份) 聚合授信 / 回款，用于月度明细表。
// startTs / endTs 为闭区间的 unix 秒；userIds 语义同 SumCreditByUsers。
func SumCreditByUserMonth(startTs, endTs int64, userIds []int) ([]CreditMonthlyAggregate, error) {
	if userIds != nil && len(userIds) == 0 {
		return nil, nil
	}
	monthExpr := creditMonthExpr()
	selectExpr, args := creditSumSelect(true)
	tx := DB.Model(&CreditRecord{}).
		Where("created_at >= ? AND created_at <= ?", startTs, endTs)
	if userIds != nil {
		tx = tx.Where("user_id IN ?", userIds)
	}
	var rows []CreditMonthlyAggregate
	if err := tx.
		Select(monthExpr+" AS month, "+selectExpr, args...).
		Group(monthExpr + ", user_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
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

// GetCreditedUserIds 返回所有有过「授信」记录的 user_id（不含只有回款的用户）。
// 供新用户统计的「授信客户」标签与筛选判定使用。
func GetCreditedUserIds() ([]int, error) {
	var ids []int
	err := DB.Model(&CreditRecord{}).
		Where("type = ?", CreditRecordTypeCredit).
		Distinct().
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
