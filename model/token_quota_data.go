package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TokenQuotaData 按 (user_id, token_id, day) 聚合的密钥维度消耗数据。
// CreatedAt 精确到服务器本地时区的日 0 点；用于「密钥统计」tab 的汇总卡片、
// Top 排行、每日明细。(user_id, token_id, created_at) 是唯一键，每条消费同步
// upsert 一行，无内存缓存 —— 进程重启/崩溃不会丢已计数据。
type TokenQuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index;uniqueIndex:uidx_tqd_user_token_day,priority:1"`
	TokenID   int    `json:"token_id" gorm:"index:idx_tqd_token_day,priority:1;uniqueIndex:uidx_tqd_user_token_day,priority:2"`
	TokenName string `json:"token_name" gorm:"size:64;default:''"`
	GroupName string `json:"group_name" gorm:"size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_tqd_token_day,priority:2;index:idx_tqd_user_day,priority:2;uniqueIndex:uidx_tqd_user_token_day,priority:3"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
}

func (TokenQuotaData) TableName() string {
	return "token_quota_data"
}

// LogTokenQuotaData 同步 upsert 一行到 token_quota_data。分桶键取 createdAt
// 所在的服务器本地时区日 0 点；命中已有行时 count/quota/token_used 增量累加，
// token_name/group_name 仅在非空时覆盖（与历史行为一致，避免被空值清空）。
func LogTokenQuotaData(userId int, tokenId int, tokenName string, group string, quota int, createdAt int64, tokenUsed int) {
	if tokenId <= 0 {
		return
	}
	t := time.Unix(createdAt, 0).In(time.Local)
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Unix()

	row := &TokenQuotaData{
		UserID:    userId,
		TokenID:   tokenId,
		TokenName: tokenName,
		GroupName: group,
		CreatedAt: dayStart,
		Count:     1,
		Quota:     quota,
		TokenUsed: tokenUsed,
	}

	assignments := map[string]interface{}{
		"count":      gorm.Expr("count + ?", 1),
		"quota":      gorm.Expr("quota + ?", quota),
		"token_used": gorm.Expr("token_used + ?", tokenUsed),
	}
	if tokenName != "" {
		assignments["token_name"] = tokenName
	}
	if group != "" {
		assignments["group_name"] = group
	}

	err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "token_id"}, {Name: "created_at"}},
		DoUpdates: clause.Assignments(assignments),
	}).Create(row).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("LogTokenQuotaData upsert error: %s", err))
	}
}

// ----------------------------------------------------------------------------
// Query helpers used by controller/token_stats.go
// ----------------------------------------------------------------------------

// TokenSummaryWindow 单个时间窗口的聚合结果
type TokenSummaryWindow struct {
	Quota int64 `json:"quota"`
}

// GetUserTokenQuotaSum 返回指定时间窗口内该用户所有密钥的消耗 quota 总和
func GetUserTokenQuotaSum(userId int, startTs int64, endTs int64) (int64, error) {
	var sum int64
	err := DB.Table("token_quota_data").
		Where("user_id = ? AND created_at >= ? AND created_at < ?", userId, startTs, endTs).
		Select("COALESCE(SUM(quota), 0)").
		Row().Scan(&sum)
	return sum, err
}

// TokenTopItem 今日 Top 排行单条
type TokenTopItem struct {
	TokenID   int    `json:"token_id"`
	TokenName string `json:"token_name"`
	Quota     int64  `json:"quota"`
}

func GetUserTokenTop(userId int, startTs int64, endTs int64, limit int) ([]TokenTopItem, error) {
	if limit <= 0 {
		limit = 10
	}
	var items []TokenTopItem
	err := DB.Table("token_quota_data").
		Select("token_id, MAX(token_name) AS token_name, SUM(quota) AS quota").
		Where("user_id = ? AND created_at >= ? AND created_at < ?", userId, startTs, endTs).
		Group("token_id").
		Order("quota DESC").
		Limit(limit).
		Scan(&items).Error
	return items, err
}

// TokenDailyDetailFilters 每日明细筛选条件
type TokenDailyDetailFilters struct {
	StartDate int64  // 起始 0 点（秒级时间戳）
	EndDate   int64  // 结束 0 点 +1 天
	GroupName string // 精确匹配
	Status    *int   // token.status：开启=1，禁用=2/3 等；nil 表示不筛
	TokenName string // 模糊匹配
}

// TokenDailyDetailItem 每日明细单条
type TokenDailyDetailItem struct {
	Date            int64  `json:"date"` // 当天 0 点 ts
	TokenID         int    `json:"token_id"`
	TokenName       string `json:"token_name"`
	TokenKey        string `json:"token_key"` // 已 mask 的密钥
	GroupName       string `json:"group_name"`
	DailyQuota      int64  `json:"daily_quota"`      // 当天消耗
	CumulativeQuota int64  `json:"cumulative_quota"` // 该密钥累计消耗（来自 tokens.used_quota）
}

// GetUserTokenDailyDetail 返回分页明细。
// 注：cumulative_quota 来自 tokens.used_quota（实时累计），不是 token_quota_data 自身按日累加，
// 因为本表只从启用本特性后开始记录；累计值仍以 tokens.used_quota 为准更直观。
func GetUserTokenDailyDetail(userId int, f TokenDailyDetailFilters, page int, pageSize int, sortBy string, sortOrder string) ([]TokenDailyDetailItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	// 校验排序字段
	// cumulative_quota 是 SELECT 阶段的别名（来自子查询），三库都允许按别名 ORDER BY
	allowedSort := map[string]string{
		"date":             "tqd.created_at",
		"daily_quota":      "tqd.quota",
		"cumulative_quota": "cumulative_quota",
	}
	col, ok := allowedSort[sortBy]
	if !ok {
		col = "tqd.created_at"
	}
	dir := "DESC"
	if sortOrder == "asc" {
		dir = "ASC"
	}

	q := DB.Table("token_quota_data AS tqd").
		Joins("LEFT JOIN tokens AS t ON t.id = tqd.token_id").
		Where("tqd.user_id = ?", userId).
		Where("tqd.quota > 0") // 仅展示当天有消耗

	if f.StartDate > 0 {
		q = q.Where("tqd.created_at >= ?", f.StartDate)
	}
	if f.EndDate > 0 {
		q = q.Where("tqd.created_at < ?", f.EndDate)
	}
	if f.GroupName != "" {
		q = q.Where("tqd.group_name = ?", f.GroupName)
	}
	if f.Status != nil {
		q = q.Where("t.status = ?", *f.Status)
	}
	if f.TokenName != "" {
		q = q.Where("tqd.token_name LIKE ?", "%"+f.TokenName+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type rawRow struct {
		Date            int64
		TokenID         int
		TokenName       string
		TokenKeyRaw     string
		GroupName       string
		DailyQuota      int64
		CumulativeQuota int64
	}
	var raws []rawRow
	// cumulative_quota = 该密钥从开始到该日（含）的累计消耗
	// 用子查询，不受外层 group/status 等筛选条件影响，反映真实累计
	cumulativeExpr := "(SELECT COALESCE(SUM(t2.quota), 0) FROM token_quota_data t2 " +
		"WHERE t2.token_id = tqd.token_id AND t2.created_at <= tqd.created_at)"
	err := q.Select("tqd.created_at AS date, tqd.token_id AS token_id, "+
		"COALESCE(NULLIF(tqd.token_name, ''), t.name) AS token_name, "+
		"COALESCE(t."+commonKeyCol+", '') AS token_key_raw, "+
		"tqd.group_name AS group_name, "+
		"tqd.quota AS daily_quota, "+
		cumulativeExpr+" AS cumulative_quota").
		Order(col + " " + dir).
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Scan(&raws).Error
	if err != nil {
		return nil, total, err
	}
	items := make([]TokenDailyDetailItem, 0, len(raws))
	for _, r := range raws {
		items = append(items, TokenDailyDetailItem{
			Date:            r.Date,
			TokenID:         r.TokenID,
			TokenName:       r.TokenName,
			TokenKey:        MaskTokenKey(r.TokenKeyRaw),
			GroupName:       r.GroupName,
			DailyQuota:      r.DailyQuota,
			CumulativeQuota: r.CumulativeQuota,
		})
	}
	return items, total, nil
}

// UserTokenAggregate tokens 表实时聚合：启用数 + 剩余总额
type UserTokenAggregate struct {
	EnabledCount int   `json:"enabled_count"`
	RemainTotal  int64 `json:"remain_total"`
}

func GetUserTokenAggregate(userId int) (UserTokenAggregate, error) {
	var agg UserTokenAggregate
	row := DB.Table("tokens").
		Where("user_id = ? AND deleted_at IS NULL", userId).
		Select("COUNT(CASE WHEN status = 1 THEN 1 END) AS enabled_count, COALESCE(SUM(CASE WHEN unlimited_quota = " + commonFalseVal + " AND status = 1 THEN remain_quota ELSE 0 END), 0) AS remain_total").
		Row()
	err := row.Scan(&agg.EnabledCount, &agg.RemainTotal)
	return agg, err
}
