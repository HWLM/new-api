package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

type UpstreamCandidate struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	Source            string `json:"source" gorm:"type:varchar(16);index"`
	OwnerUserID       *int   `json:"owner_user_id,omitempty" gorm:"index"`
	ApplicationID     *int64 `json:"application_id,omitempty" gorm:"uniqueIndex"`
	Type              int    `json:"type" gorm:"index;uniqueIndex:idx_upstream_type_url"`
	Name              string `json:"name" gorm:"type:varchar(128)"`
	BaseURL           string `json:"base_url" gorm:"type:varchar(512)"`
	Models            string `json:"models" gorm:"type:text"`
	NormalizedBaseURL string `json:"normalized_base_url" gorm:"type:varchar(512);uniqueIndex:idx_upstream_type_url"`
	APIKey            string `json:"-" gorm:"type:text"`
	Contact           string `json:"contact" gorm:"type:varchar(255)"`
	Remark            string `json:"remark" gorm:"type:varchar(1024)"`
	BenchmarkStatus   string `json:"benchmark_status" gorm:"type:varchar(32);index"`
	LatestRunID       *int64 `json:"latest_run_id,omitempty"`
	ChannelID         *int   `json:"channel_id,omitempty" gorm:"index"`
	SyncedTime        int64  `json:"synced_time,omitempty"`
	CreatedTime       int64  `json:"created_time" gorm:"bigint;index"`
	UpdatedTime       int64  `json:"updated_time" gorm:"bigint;index"`
}

func (u *UpstreamCandidate) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if u.CreatedTime == 0 {
		u.CreatedTime = now
	}
	if u.UpdatedTime == 0 {
		u.UpdatedTime = now
	}
	if u.BenchmarkStatus == "" {
		u.BenchmarkStatus = constant.UpstreamBenchmarkPending
	}
	return nil
}

func (u *UpstreamCandidate) BeforeUpdate(_ *gorm.DB) error {
	u.UpdatedTime = common.GetTimestamp()
	return nil
}

func (u *UpstreamCandidate) HasAPIKey() bool { return strings.TrimSpace(u.APIKey) != "" }

func GetUpstreamCandidate(id int64) (*UpstreamCandidate, error) {
	var candidate UpstreamCandidate
	if err := DB.First(&candidate, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &candidate, nil
}

func ListUpstreamCandidates(offset, limit int, keyword string, source string, status string) ([]*UpstreamCandidate, int64, error) {
	query := DB.Model(&UpstreamCandidate{})
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query = query.Where("name LIKE ? OR base_url LIKE ? OR contact LIKE ?", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if status != "" {
		query = query.Where("benchmark_status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*UpstreamCandidate
	if err := query.Order("id desc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
