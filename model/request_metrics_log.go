package model

import (
	"context"
)

// RequestMetricsLog 请求-响应统计埋点明细。
// 每次 relay 完成异步写入一行,只用于"请求-响应统计分析"功能。
// 与 logs 表完全解耦,TTL 默认 3 天(可配置)。
type RequestMetricsLog struct {
	Id        int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	RequestId string `gorm:"type:varchar(64);index:idx_rml_request_id;default:''" json:"request_id"`

	// 维度
	UserId      int    `gorm:"index:idx_rml_user_created,priority:1;default:0" json:"user_id"`
	Username    string `gorm:"type:varchar(64);default:''" json:"username"`
	TokenId     int    `gorm:"default:0" json:"token_id"`
	ChannelId   int    `gorm:"index:idx_rml_chan_created,priority:1;default:0" json:"channel_id"`
	ChannelType int16  `gorm:"index:idx_rml_type_created,priority:1;default:0" json:"channel_type"`
	ModelName   string `gorm:"type:varchar(255);default:''" json:"model_name"`
	Group       string `gorm:"column:group;type:varchar(64);default:''" json:"group"`

	// 指标
	StatusCode       int16 `gorm:"default:200" json:"status_code"`
	DurationMs       int   `gorm:"default:0" json:"duration_ms"`
	FirstTokenMs     int   `gorm:"default:0" json:"first_token_ms"`
	PromptTokens     int   `gorm:"default:0" json:"prompt_tokens"`
	CompletionTokens int   `gorm:"default:0" json:"completion_tokens"`
	IsStream         bool  `gorm:"default:false" json:"is_stream"`

	// 错误信息
	ErrorType       int16  `gorm:"default:0" json:"error_type"`
	IsBusinessError bool   `gorm:"default:false" json:"is_business_error"`
	ErrorCode       string `gorm:"type:varchar(64);default:''" json:"error_code"`
	ErrorMessage    string `gorm:"type:varchar(512);default:''" json:"error_message"`

	// 时间(unix 秒)
	CreatedAt int64 `gorm:"not null;index:idx_rml_created;index:idx_rml_user_created,priority:2;index:idx_rml_chan_created,priority:2;index:idx_rml_type_created,priority:2" json:"created_at"`
}

func (RequestMetricsLog) TableName() string {
	return "request_metrics_logs"
}

// ErrorType 枚举
const (
	MetricsErrorTypeNone     = 0
	MetricsErrorTypeUpstream = 1 // 上游 5xx / 429 / overloaded
	MetricsErrorTypeInternal = 2 // 网关内部异常
	MetricsErrorTypeBusiness = 3 // 业务错误(余额不足/密钥额度等)
	MetricsErrorTypeNetwork  = 4 // 网络层 timeout / 连接失败
)

// BatchInsertRequestMetricsLogs 异步 writer 调用,一次性写入一批。
// 注意:不要在请求路径上同步调用 — 在 buffered worker goroutine 中调用。
func BatchInsertRequestMetricsLogs(ctx context.Context, rows []*RequestMetricsLog) error {
	if len(rows) == 0 {
		return nil
	}
	return LOG_DB.WithContext(ctx).CreateInBatches(rows, 100).Error
}

// CleanupRequestMetricsLogsBefore 删除指定 unix 秒之前的明细行。
// 由 cleanup cron 调用。返回删除行数(用于日志)。
func CleanupRequestMetricsLogsBefore(ctx context.Context, beforeUnix int64) (int64, error) {
	result := LOG_DB.WithContext(ctx).
		Where("created_at < ?", beforeUnix).
		Delete(&RequestMetricsLog{})
	return result.RowsAffected, result.Error
}

