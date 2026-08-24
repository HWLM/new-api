package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

type BusinessCooperation struct {
	ID            int64  `json:"id" gorm:"primaryKey"`
	UserID        int    `json:"user_id" gorm:"index"`
	UpstreamID    int64  `json:"upstream_id" gorm:"uniqueIndex"`
	Status        string `json:"status" gorm:"type:varchar(32);index"`
	RejectReason  string `json:"reject_reason,omitempty" gorm:"type:varchar(1024)"`
	Revision      int    `json:"revision"`
	CreatedTime   int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime   int64  `json:"updated_time" gorm:"bigint"`
	SubmittedTime int64  `json:"submitted_time" gorm:"bigint"`
	ReviewedTime  int64  `json:"reviewed_time,omitempty" gorm:"bigint"`
}

func (b *BusinessCooperation) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if b.CreatedTime == 0 {
		b.CreatedTime = now
	}
	if b.UpdatedTime == 0 {
		b.UpdatedTime = now
	}
	if b.SubmittedTime == 0 {
		b.SubmittedTime = now
	}
	if b.Status == "" {
		b.Status = constant.BusinessCooperationPendingReview
	}
	if b.Revision == 0 {
		b.Revision = 1
	}
	return nil
}

func (b *BusinessCooperation) BeforeUpdate(_ *gorm.DB) error {
	b.UpdatedTime = common.GetTimestamp()
	return nil
}

func GetBusinessCooperation(id int64) (*BusinessCooperation, error) {
	var item BusinessCooperation
	if err := DB.First(&item, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func ListBusinessCooperations(userID int, offset, limit int) ([]*BusinessCooperation, int64, error) {
	query := DB.Model(&BusinessCooperation{}).Where("user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*BusinessCooperation
	if err := query.Order("id desc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
