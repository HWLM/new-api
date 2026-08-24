package model

import (
	"errors"

	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

func LockUpstreamCandidate(tx *gorm.DB, id int64) (*UpstreamCandidate, error) {
	var item UpstreamCandidate
	if err := lockForUpdate(tx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func LockBusinessCooperation(tx *gorm.DB, id int64, userID int) (*BusinessCooperation, error) {
	var item BusinessCooperation
	if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", id, userID).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func LockBusinessCooperationByID(tx *gorm.DB, id int64) (*BusinessCooperation, error) {
	var item BusinessCooperation
	if err := lockForUpdate(tx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func LockUpstreamBenchmarkRun(tx *gorm.DB, id int64) (*UpstreamBenchmarkRun, error) {
	var item UpstreamBenchmarkRun
	if err := lockForUpdate(tx).Where("id = ?", id).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func LockUpstreamBenchmarkSystemTask(tx *gorm.DB, taskID string) (*SystemTask, error) {
	var item SystemTask
	if err := lockForUpdate(tx).
		Where("type = ? AND task_id = ?", constant.SystemTaskTypeUpstreamBenchmark, taskID).
		First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func LockUpstreamBenchmarkLeaseByTaskID(tx *gorm.DB, taskID string) (*SystemTaskLock, error) {
	var item SystemTaskLock
	err := lockForUpdate(tx).
		Where("type = ? AND task_id = ?", constant.SystemTaskTypeUpstreamBenchmark, taskID).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func LockUpstreamBenchmarkLease(tx *gorm.DB, taskID string, runnerID string, now int64) (*SystemTaskLock, error) {
	var item SystemTaskLock
	if err := lockForUpdate(tx).
		Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until >= ?", constant.SystemTaskTypeUpstreamBenchmark, taskID, runnerID, now).
		First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func LockExpiredUpstreamBenchmarkLeases(tx *gorm.DB, now int64) ([]*SystemTaskLock, error) {
	var items []*SystemTaskLock
	err := lockForUpdate(tx).
		Where("type = ? AND locked_until < ?", constant.SystemTaskTypeUpstreamBenchmark, now).
		Order("task_id asc").
		Find(&items).Error
	return items, err
}
