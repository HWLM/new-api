package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// TokenExhaustingSnapshot 即将耗尽密钥快照
// 由 service/token_exhausting_snapshot_task.go 每 5 分钟刷新一次
// 触发条件：tokens.status=1 AND tokens.unlimited_quota=false AND remain/(used+remain) < 0.05
type TokenExhaustingSnapshot struct {
	Id          int     `json:"id"`
	UserID      int     `json:"user_id" gorm:"index"`
	SnapshotAt  int64   `json:"snapshot_at" gorm:"bigint;index"`
	TokenID     int     `json:"token_id"`
	TokenName   string  `json:"token_name" gorm:"size:64"`
	TokenKey    string  `json:"token_key" gorm:"size:64;default:''"` // 已 mask
	GroupName   string  `json:"group_name" gorm:"size:64;default:''"`
	UsedQuota   int     `json:"used_quota" gorm:"default:0"`
	RemainQuota int     `json:"remain_quota" gorm:"default:0"`
	RemainRatio float64 `json:"remain_ratio" gorm:"default:0"`
}

func (TokenExhaustingSnapshot) TableName() string {
	return "token_exhausting_snapshot"
}

// RefreshExhaustingSnapshot 重算并替换全表数据。简单方案：清空再插入。
// 5 分钟一次的调度由调用方保证串行（atomic.Bool 锁）。
func RefreshExhaustingSnapshot(now int64) error {
	type tokenRow struct {
		Id          int
		UserId      int
		Name        string
		TokenKeyRaw string
		GroupName   string
		UsedQuota   int
		RemainQuota int
	}
	var rows []tokenRow
	err := DB.Table("tokens").
		Select("id, user_id, name, "+commonKeyCol+" AS token_key_raw, "+commonGroupCol+" AS group_name, used_quota, remain_quota").
		Where("status = 1 AND unlimited_quota = "+commonFalseVal+" AND deleted_at IS NULL").
		Where("remain_quota > 0").
		Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("scan tokens: %w", err)
	}

	snapshots := make([]TokenExhaustingSnapshot, 0, len(rows))
	for _, r := range rows {
		total := r.UsedQuota + r.RemainQuota
		if total <= 0 {
			continue
		}
		ratio := float64(r.RemainQuota) / float64(total)
		if ratio >= 0.05 {
			continue
		}
		snapshots = append(snapshots, TokenExhaustingSnapshot{
			UserID:      r.UserId,
			SnapshotAt:  now,
			TokenID:     r.Id,
			TokenName:   r.Name,
			TokenKey:    MaskTokenKey(r.TokenKeyRaw),
			GroupName:   r.GroupName,
			UsedQuota:   r.UsedQuota,
			RemainQuota: r.RemainQuota,
			RemainRatio: ratio,
		})
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM token_exhausting_snapshot").Error; err != nil {
			return fmt.Errorf("truncate snapshot: %w", err)
		}
		if len(snapshots) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(&snapshots, 200).Error; err != nil {
			return fmt.Errorf("insert snapshot: %w", err)
		}
		common.SysLog(fmt.Sprintf("token exhausting snapshot refreshed: %d rows", len(snapshots)))
		return nil
	})
}

// ListExhaustingByUser 按用户分页拉耗尽密钥列表
func ListExhaustingByUser(userId int, page int, pageSize int) ([]TokenExhaustingSnapshot, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 10
	}
	var total int64
	if err := DB.Model(&TokenExhaustingSnapshot{}).
		Where("user_id = ?", userId).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []TokenExhaustingSnapshot
	err := DB.Model(&TokenExhaustingSnapshot{}).
		Where("user_id = ?", userId).
		Order("remain_ratio ASC, used_quota DESC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&items).Error
	return items, total, err
}
