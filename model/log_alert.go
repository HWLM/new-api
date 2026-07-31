package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	LogAlertPlatformWeComGroup = "wecom_group"
	LogAlertPlatformTelegram   = "telegram_bot"
)

type LogAlertPlatformConfig struct {
	Type       string `json:"type"`
	WebhookURL string `json:"webhook_url,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	BotToken   string `json:"bot_token,omitempty"`
}

// 错误日志告警规则。
// 目标：定时扫描 logs 表中 type=LogTypeError 的记录，按规则的过滤条件命中后按监控对象
// 分桶，通过企微群机器人 webhook 推送告警。
//
// 存储在主库（DB），与业务日志（LOG_DB）分离，因 ClickHouse 等只读日志库不能承载配置表。
type LogAlertRule struct {
	Id   int    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name string `gorm:"type:varchar(128);default:''" json:"name"` // 可选：便于列表识别
	// Enabled 默认由业务侧保证（controller Create 硬编码 true）。
	// 不用 gorm:"default:true" 是因为 MySQL/PostgreSQL 对布尔默认值规范化不同，
	// AutoMigrate 会在每次启动反复 ALTER TABLE。
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `gorm:"default:1" json:"interval_minutes"` // 扫描频率(分钟)；同时作为默认冷却 TTL
	WebhookUrl      string `gorm:"type:text" json:"webhook_url"`      // 企微群机器人 URL

	// Filters：JSON 数组 [{code, keywords[]}]。
	// 语义：行内 AND；行间 OR；code / keywords 均空的行忽略；整体为空则不过滤。
	// code 匹配 content 里 "status_code={code},"；keywords 走 strings.Contains(content, kw)，
	// 多个 keyword 之间 OR。
	Filters string `gorm:"type:text" json:"filters"`

	// ScopeType：监控对象类型
	//   all     — 全部错误汇总告警（无链接）
	//   user    — 按用户过滤 & 分桶
	//   token   — 按密钥过滤 & 分桶
	//   channel — 按渠道过滤 & 分桶
	ScopeType string `gorm:"type:varchar(16);default:'all'" json:"scope_type"`

	// ScopeValues：JSON。
	//   user    → ["username1","username2"]
	//   channel → [1,2,3]
	//   token   → [{user_id: <int>, token_ids: [<int>...] }]（前端级联展示用；后端只需扁平 token_ids）
	ScopeValues string `gorm:"type:text" json:"scope_values"`

	// 每日活跃时段（本地时间，分钟为单位）。默认 0~1439 = 全天；
	// 只有当 now 的 minute-of-day 落在 [ActiveStartMinute, ActiveEndMinute] 时评估器才扫这条规则。
	// 用 int 而非 "HH:MM" 字符串：跨库 diff 稳、比较简单。start > end 不支持（跨天）。
	ActiveStartMinute int `gorm:"default:0" json:"active_start_minute"`
	ActiveEndMinute   int `gorm:"default:1439" json:"active_end_minute"`

	LastScanAt      int64                    `gorm:"default:0" json:"last_scan_at"`
	CreatedAt       int64                    `gorm:"not null" json:"created_at"`
	UpdatedAt       int64                    `gorm:"not null" json:"updated_at"`
	PlatformConfigs string                   `gorm:"type:text" json:"-"`
	Platforms       []LogAlertPlatformConfig `gorm:"-" json:"platform_configs"`
}

func (LogAlertRule) TableName() string { return "log_alert_rules" }

func (r *LogAlertRule) AlertPlatforms() ([]LogAlertPlatformConfig, error) {
	if len(r.Platforms) > 0 {
		return r.Platforms, nil
	}
	if raw := strings.TrimSpace(r.PlatformConfigs); raw != "" {
		var configs []LogAlertPlatformConfig
		if err := common.UnmarshalJsonStr(raw, &configs); err != nil {
			return nil, err
		}
		return configs, nil
	}
	if webhookURL := strings.TrimSpace(r.WebhookUrl); webhookURL != "" {
		return []LogAlertPlatformConfig{{
			Type:       LogAlertPlatformWeComGroup,
			WebhookURL: webhookURL,
		}}, nil
	}
	return nil, nil
}

func (r *LogAlertRule) LoadAlertPlatforms() error {
	platforms, err := r.AlertPlatforms()
	if err != nil {
		return err
	}
	r.Platforms = platforms
	return nil
}

// 每次触发的告警事件；ack 端点通过 event id + token 定位。
type LogAlertEvent struct {
	Id              int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	RuleId          int    `gorm:"index" json:"rule_id"`
	RuleName        string `gorm:"type:varchar(128);default:''" json:"rule_name"`
	ScopeType       string `gorm:"type:varchar(16)" json:"scope_type"`
	ScopeKey        string `gorm:"type:varchar(64);index" json:"scope_key"` // "all" / "user:42" / ...
	ScopeLabel      string `gorm:"type:varchar(128);default:''" json:"scope_label"`
	HitCount        int    `json:"hit_count"`
	SampleLogId     int    `json:"sample_log_id"` // 桶里最新一条 log id
	SampleRequest   string `gorm:"type:varchar(64);default:''" json:"sample_request"`
	RelatedRequests string `gorm:"type:text" json:"related_requests"`
	WindowFromAt    int64  `json:"window_from_at"`
	WindowToAt      int64  `json:"window_to_at"`
	FiredAt         int64  `gorm:"index;not null" json:"fired_at"`
	AckedAt         int64  `gorm:"default:0" json:"acked_at"`
	AckToken        string `gorm:"type:varchar(32);default:''" json:"-"` // 匿名端点校验用
}

func (LogAlertEvent) TableName() string { return "log_alert_events" }

// ===== Rule CRUD =====

func ListLogAlertRules(ctx context.Context) ([]LogAlertRule, error) {
	var rules []LogAlertRule
	err := DB.WithContext(ctx).Order("id desc").Find(&rules).Error
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if err := rules[i].LoadAlertPlatforms(); err != nil {
			common.SysError(fmt.Sprintf("log alert rule=%d platform configs malformed: %s", rules[i].Id, err.Error()))
		}
	}
	return rules, err
}

func ListEnabledLogAlertRules(ctx context.Context) ([]LogAlertRule, error) {
	var rules []LogAlertRule
	err := DB.WithContext(ctx).Where("enabled = ?", true).Find(&rules).Error
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if err := rules[i].LoadAlertPlatforms(); err != nil {
			common.SysError(fmt.Sprintf("log alert rule=%d platform configs malformed: %s", rules[i].Id, err.Error()))
		}
	}
	return rules, err
}

func GetLogAlertRule(ctx context.Context, id int) (*LogAlertRule, error) {
	var rule LogAlertRule
	if err := DB.WithContext(ctx).First(&rule, id).Error; err != nil {
		return nil, err
	}
	if err := rule.LoadAlertPlatforms(); err != nil {
		return nil, err
	}
	return &rule, nil
}

func CreateLogAlertRule(ctx context.Context, rule *LogAlertRule) error {
	return DB.WithContext(ctx).Create(rule).Error
}

func UpdateLogAlertRule(ctx context.Context, rule *LogAlertRule) error {
	return DB.WithContext(ctx).Save(rule).Error
}

func DeleteLogAlertRule(ctx context.Context, id int) error {
	return DB.WithContext(ctx).Delete(&LogAlertRule{}, id).Error
}

// TouchLogAlertRuleLastScan 更新水位；扫描完成时调用，未来窗口从这里开始。
func TouchLogAlertRuleLastScan(ctx context.Context, id int, ts int64) error {
	return DB.WithContext(ctx).Model(&LogAlertRule{}).
		Where("id = ?", id).
		Update("last_scan_at", ts).Error
}

// ===== Event =====

func CreateLogAlertEvent(ctx context.Context, ev *LogAlertEvent) error {
	return DB.WithContext(ctx).Create(ev).Error
}

func GetLogAlertEvent(ctx context.Context, id int64) (*LogAlertEvent, error) {
	var ev LogAlertEvent
	if err := DB.WithContext(ctx).First(&ev, id).Error; err != nil {
		return nil, err
	}
	return &ev, nil
}

func AckLogAlertEvent(ctx context.Context, id int64, ackedAt int64) error {
	return DB.WithContext(ctx).Model(&LogAlertEvent{}).
		Where("id = ?", id).
		Update("acked_at", ackedAt).Error
}

func ListLogAlertEvents(ctx context.Context, ruleId int, limit int) ([]LogAlertEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	q := DB.WithContext(ctx).Order("fired_at desc").Limit(limit)
	if ruleId > 0 {
		q = q.Where("rule_id = ?", ruleId)
	}
	var events []LogAlertEvent
	err := q.Find(&events).Error
	return events, err
}
