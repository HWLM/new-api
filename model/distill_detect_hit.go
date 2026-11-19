package model

import (
	"time"
)

// DistillDetectHit 是一条「蒸馏检测命中」流水。
//
// 为什么单独建表而不是塞进 logs：
//   - logs 是全量请求日志级别的大表，且有留存清理；命中是要拿去通知客户的
//     凭证，运营会按 request_id 反查，不能被日志清理带走。
//   - 命中量极小（一个用户一轮降级只记一条），单独建表后按用户/时间查询
//     可以直接走索引，不受 logs 体量影响。
//
// 与 logs 的关系：命中**不代替**正常请求日志，它只是一条旁路记录；运营拿到
// RequestId 之后仍然去 logs 看这次请求的完整上下文。
type DistillDetectHit struct {
	Id     int64 `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId int   `json:"user_id" gorm:"index:idx_distill_hits_user_time,priority:1"`
	// RequestId 是运营拿去通知客户的编号，与响应头 X-Oneapi-Request-Id 同值。
	RequestId string `json:"request_id" gorm:"type:varchar(64);index;not null"`
	Model     string `json:"model" gorm:"type:varchar(128)"`
	ChannelId int    `json:"channel_id"`
	// FallbackId 实际重导到的兜底渠道；0 = 未重导（观察模式，或兜底不可用）。
	FallbackId int `json:"fallback_id"`
	// Score 是四题里的最高分；命中即 >= 配置的阈值。
	Score float64 `json:"score"`
	// BankId 目前恒为 "train"（只迁移了「训练数据与评测」这一个题库）。
	BankId string `json:"bank_id" gorm:"type:varchar(32);not null"`
	// Categories 是四题逐题分数，JSON 文本；解析失败时留空，不影响主分数。
	Categories string `json:"categories" gorm:"type:text"`
	// DocChars 实际送判定的正文字符数，用于核对采样是否生效。
	DocChars int `json:"doc_chars"`
	// SampledAt 判定发生的时刻。
	SampledAt time.Time `json:"sampled_at" gorm:"index:idx_distill_hits_user_time,priority:2"`
	// DemoteUntil 本轮降级到期时刻；观察模式下仍然写，便于估算重导后会持续多久。
	DemoteUntil *time.Time `json:"demote_until"`
	// AlertSent 告警是否已发出；发送失败保持 false，运营可对照日志排查。
	AlertSent bool `json:"alert_sent"`
}

func (DistillDetectHit) TableName() string {
	return "distill_detect_hits"
}
