package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

type UpstreamBenchmarkRun struct {
	ID               int64   `json:"id" gorm:"primaryKey"`
	UpstreamID       int64   `json:"upstream_id" gorm:"index"`
	SystemTaskID     string  `json:"system_task_id" gorm:"type:varchar(64);index"`
	Concurrency      int     `json:"concurrency"`
	Model            string  `json:"model" gorm:"type:varchar(128)"`
	Status           string  `json:"status" gorm:"type:varchar(32);index"`
	Metrics          string  `json:"metrics" gorm:"type:text"`
	Scores           string  `json:"scores" gorm:"type:text"`
	StatusCode       int     `json:"status_code"`
	LatencyMS        int64   `json:"latency_ms" gorm:"bigint"`
	UnmetCount       int     `json:"unmet_count"`
	AutoSyncStatus   string  `json:"auto_sync_status" gorm:"type:varchar(32)"`
	AutoSyncError    string  `json:"auto_sync_error,omitempty" gorm:"type:text"`
	AutoSyncedTime   int64   `json:"auto_synced_time,omitempty" gorm:"bigint"`
	BaselineSnapshot string  `json:"baseline_snapshot" gorm:"type:text"`
	OverallScore     float64 `json:"overall_score"`
	Error            string  `json:"error,omitempty" gorm:"type:text"`
	StartedTime      int64   `json:"started_time,omitempty" gorm:"bigint"`
	FinishedTime     int64   `json:"finished_time,omitempty" gorm:"bigint"`
	DurationMS       int64   `json:"duration_ms,omitempty" gorm:"bigint"`
	ProfileName      string  `json:"profile_name" gorm:"type:varchar(64)"`
	ProfileVersion   int     `json:"profile_version"`
	ProfileSnapshot  string  `json:"profile_snapshot" gorm:"type:text"`
	CreatedTime      int64   `json:"created_time" gorm:"bigint;index"`
}

type UpstreamBenchmarkProfile struct {
	ID          int    `json:"id" gorm:"primaryKey"`
	Name        string `json:"name" gorm:"type:varchar(64);uniqueIndex"`
	Version     int    `json:"version"`
	Config      string `json:"config" gorm:"type:text"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

func (p *UpstreamBenchmarkProfile) BeforeCreate(_ *gorm.DB) error {
	if p.Name == "" {
		p.Name = constant.UpstreamBenchmarkProfileGeneral
	}
	if p.Version == 0 {
		p.Version = 1
	}
	if p.UpdatedTime == 0 {
		p.UpdatedTime = common.GetTimestamp()
	}
	return nil
}

func GetUpstreamBenchmarkProfile(tx *gorm.DB, name string) (*UpstreamBenchmarkProfile, error) {
	if name == "" {
		name = constant.UpstreamBenchmarkProfileGeneral
	}
	var profile UpstreamBenchmarkProfile
	err := tx.Where("name = ?", name).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (r *UpstreamBenchmarkRun) BeforeCreate(_ *gorm.DB) error {
	if r.CreatedTime == 0 {
		r.CreatedTime = common.GetTimestamp()
	}
	if r.Status == "" {
		r.Status = constant.UpstreamBenchmarkRunPending
	}
	return nil
}

func GetUpstreamBenchmarkRun(id int64) (*UpstreamBenchmarkRun, error) {
	var item UpstreamBenchmarkRun
	if err := DB.First(&item, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func ListUpstreamBenchmarkRuns(upstreamID int64, limit int) ([]*UpstreamBenchmarkRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var items []*UpstreamBenchmarkRun
	err := DB.Where("upstream_id = ?", upstreamID).Order("id desc").Limit(limit).Find(&items).Error
	return items, err
}

func GetActiveUpstreamBenchmarkSystemTask(tx *gorm.DB) (*SystemTask, error) {
	var task SystemTask
	err := tx.Where("type = ? AND status IN ?", constant.SystemTaskTypeUpstreamBenchmark, []string{
		string(SystemTaskStatusPending),
		string(SystemTaskStatusRunning),
	}).Order("id desc").First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func CreateUpstreamBenchmarkSystemTask(tx *gorm.DB, payload any, state any) (*SystemTask, error) {
	taskID, err := GenerateSystemTaskID()
	if err != nil {
		return nil, err
	}
	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	stateBytes, err := common.Marshal(state)
	if err != nil {
		return nil, err
	}
	taskType := constant.SystemTaskTypeUpstreamBenchmark
	task := &SystemTask{
		TaskID:    taskID,
		Type:      taskType,
		Status:    SystemTaskStatusPending,
		ActiveKey: &taskType,
		Payload:   string(payloadBytes),
		State:     string(stateBytes),
	}
	if err := tx.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}
