package model

import (
	"time"
)

// DistillJevCall 是一次**真实**调用 Jev 判定服务的流水。
//
// 为什么要单独一张表而不是塞进命中记录：
//   - 命中小表只记「判中了」的那几条；运营要看的却是「到底调了多少次、每次
//     为什么失败」，未命中与失败的调用只有这里能看到；
//   - 失败（超时、HTTP 错误、解析失败）也记一行，否则排查「检定服务是不是挂了
//     或者密钥错了」时无从下手；
//   - 去重跳过的那次**不算调用**，不写行（规格：去重只省 Jev 调用）。
//
// 与 distill_detect_hits 的关系：调用成功且命中时两边各有一条，用 user_id +
// request_id + 时间对得上；这里只管「调用本身」。
type DistillJevCall struct {
	Id int64 `json:"id" gorm:"primaryKey;autoIncrement"`
	// UserId 被检测的用户（不是操作者）。
	UserId    int    `json:"user_id" gorm:"index:idx_distill_jev_calls_user_time,priority:1"`
	RequestId string `json:"request_id" gorm:"type:varchar(64);index"`
	Model     string `json:"model" gorm:"type:varchar(128)"`
	ChannelId int    `json:"channel_id"`
	// Rpm 发这次判定时，该用户当前分钟的请求计数（激活阈值的判定依据）。
	Rpm int `json:"rpm"`
	// Outcome 调用结果：success / http_error / timeout / network_error / parse_error。
	Outcome string `json:"outcome" gorm:"type:varchar(32);index"`
	// HttpStatus 判定服务的 HTTP 状态码；没拿到响应（超时/网络错误）时为 0。
	HttpStatus int `json:"http_status"`
	// Score 判定服务给出的最高分；只有 success 才有值，其余为 NULL。
	Score *float64 `json:"score"`
	// Threshold 当时的命中阈值（缺省 0.60），失败记录也写，便于对照。
	Threshold float64 `json:"threshold"`
	// DurationMs 这次调用的耗时（毫秒）。
	DurationMs int64 `json:"duration_ms"`
	// Error 失败时的错误摘要；成功时为空。
	Error string `json:"error" gorm:"type:varchar(500)"`
	// CreatedAt 调用发生的时刻（调用开始时间）。
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_distill_jev_calls_user_time,priority:2"`
}

func (DistillJevCall) TableName() string {
	return "distill_jev_calls"
}
