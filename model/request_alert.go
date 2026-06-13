package model

import (
	"context"
)

// 请求-响应统计告警规则
type RequestAlertRule struct {
	Id               int     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string  `gorm:"type:varchar(128)" json:"name"`
	Platforms        string  `gorm:"type:varchar(512);default:''" json:"platforms"` // JSON [channel_type,...] 空表示全部
	Metric           string  `gorm:"type:varchar(32)" json:"metric"`                // avg_duration_ms / slow_resp_rate / error_rate
	Operator         string  `gorm:"type:varchar(8);default:'gt'" json:"operator"`  // gt / eq
	Threshold        float64 `gorm:"default:0" json:"threshold"`
	SustainedMinutes int     `gorm:"default:5" json:"sustained_minutes"`
	CooldownMinutes  int     `gorm:"default:30" json:"cooldown_minutes"`
	TgBotToken       string  `gorm:"type:varchar(128);default:''" json:"tg_bot_token"`
	TgChatId         string  `gorm:"type:varchar(64);default:''" json:"tg_chat_id"`
	Enabled          bool    `gorm:"default:true" json:"enabled"`
	CreatedAt        int64   `gorm:"not null" json:"created_at"`
	UpdatedAt        int64   `gorm:"not null" json:"updated_at"`
}

func (RequestAlertRule) TableName() string { return "request_alert_rules" }

// 告警事件
type RequestAlertEvent struct {
	Id          int64   `gorm:"primaryKey;autoIncrement" json:"id"`
	RuleId      int     `gorm:"index" json:"rule_id"`
	FiredAt     int64   `gorm:"index;not null" json:"fired_at"`
	ResolvedAt  int64   `gorm:"default:0" json:"resolved_at"`
	MetricValue float64 `gorm:"default:0" json:"metric_value"`
	Status      string  `gorm:"type:varchar(16);default:'firing'" json:"status"` // firing / resolved
	Payload     string  `gorm:"type:text" json:"payload"`
}

func (RequestAlertEvent) TableName() string { return "request_alert_events" }

// ===== CRUD =====

func ListRequestAlertRules(ctx context.Context) ([]RequestAlertRule, error) {
	var rules []RequestAlertRule
	err := DB.WithContext(ctx).Order("id desc").Find(&rules).Error
	return rules, err
}

func GetRequestAlertRule(ctx context.Context, id int) (*RequestAlertRule, error) {
	var rule RequestAlertRule
	if err := DB.WithContext(ctx).First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func CreateRequestAlertRule(ctx context.Context, rule *RequestAlertRule) error {
	return DB.WithContext(ctx).Create(rule).Error
}

func UpdateRequestAlertRule(ctx context.Context, rule *RequestAlertRule) error {
	return DB.WithContext(ctx).Save(rule).Error
}

func DeleteRequestAlertRule(ctx context.Context, id int) error {
	return DB.WithContext(ctx).Delete(&RequestAlertRule{}, id).Error
}

func ListEnabledRequestAlertRules(ctx context.Context) ([]RequestAlertRule, error) {
	var rules []RequestAlertRule
	err := DB.WithContext(ctx).Where("enabled = ?", true).Find(&rules).Error
	return rules, err
}

// 告警事件
func ListRequestAlertEvents(ctx context.Context, fromUnix, toUnix int64, limit int) ([]RequestAlertEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	q := DB.WithContext(ctx).Order("fired_at desc").Limit(limit)
	if fromUnix > 0 {
		q = q.Where("fired_at >= ?", fromUnix)
	}
	if toUnix > 0 {
		q = q.Where("fired_at < ?", toUnix)
	}
	var events []RequestAlertEvent
	err := q.Find(&events).Error
	return events, err
}

func CreateRequestAlertEvent(ctx context.Context, ev *RequestAlertEvent) error {
	return DB.WithContext(ctx).Create(ev).Error
}

func FindLastFiringEvent(ctx context.Context, ruleId int) (*RequestAlertEvent, error) {
	var ev RequestAlertEvent
	err := DB.WithContext(ctx).
		Where("rule_id = ? AND status = ?", ruleId, "firing").
		Order("fired_at desc").First(&ev).Error
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

func ResolveAlertEvent(ctx context.Context, eventId int64, resolvedAt int64) error {
	return DB.WithContext(ctx).Model(&RequestAlertEvent{}).
		Where("id = ?", eventId).
		Updates(map[string]any{"status": "resolved", "resolved_at": resolvedAt}).Error
}
