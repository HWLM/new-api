package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// DistillDetectSetting 控制「蒸馏检测 → 临时降级到兜底渠道」功能。
//
// 来源：hw_gateway 的 decision_model「训练数据与评测」题库（train），
// 迁移成 new-api 侧的判定服务。规格见 docs/distill-detection-plan.md。
//
// 与拒答降级的区别：
//   - 拒答降级看的是「上游返回 stop_reason=refusal 的比例」；
//   - 蒸馏检测看的是「请求正文本身是不是在批量生产训练语料 / 评测集」，
//     由外部判定服务（Jev /v1/systemone）打 4 道题，取最高分。
//
// 成本控制（这是与拒答降级最大的不同）：只有该用户**每分钟请求量达到阈值**
// 才激活判定，且激活后按 N 抽样。达不到阈值的普通用户一次 Jev 都不调用。
type DistillDetectSetting struct {
	// Enabled 全局总开关；关闭时不抽样、不判定、不告警。
	Enabled bool `json:"enabled"`

	// ActivateRPM 每分钟请求量激活阈值：该用户当前分钟内的请求数达到该值才开始抽样判定。
	// 0 或负数表示「不设门槛」（任何请求都可能被判定），一般不这么配。
	ActivateRPM int `json:"activate_rpm"`

	// SampleEvery 抽样间隔 N：激活后每 N 条请求抽 1 条判定；窗口内第一条必判。
	SampleEvery int `json:"sample_every"`

	// Threshold 判定阈值：4 题最高分 >= 该值即命中。与 hw_gateway 现网一致为 0.6。
	Threshold float64 `json:"threshold"`

	// DemoteSeconds 命中后降级持续秒数；观察模式下只用于 hits 记录与告警节流。
	DemoteSeconds int `json:"demote_seconds"`

	// ObserveOnly 只观察：命中只写 hits 与告警，**不重导流量**。
	// 灰度阶段（规格 §9）必须为 true；确认误伤后再关掉。
	ObserveOnly bool `json:"observe_only"`

	// JevBaseURL 判定服务地址。空 = 未配置，整条链路跳过。
	JevBaseURL string `json:"jev_base_url"`
	// JevApiKey 判定服务密钥。只在管理页保存，代码与文档不留任何默认值。
	JevApiKey string `json:"jev_api_key"`
	// JevTimeoutMs 判定调用超时（毫秒）。失败一律按未命中放行。
	JevTimeoutMs int `json:"jev_timeout_ms"`

	// MaxStateChars 送判定的正文上限字符数。
	MaxStateChars int `json:"max_state_chars"`
	// HeadSampleChars 头部保留字符数；0 = 只取尾部（老行为），>0 = 头尾采样。
	// 批处理语料的证据常在头部，纯尾部截断会丢。
	HeadSampleChars int `json:"head_sample_chars"`

	// QuestionCorpusFarm / QuestionRewardRL / QuestionJudgeEval / QuestionDistillTarget
	// 四道题各自可单独禁用（与 hw_gateway 的题库 enabled 位对应）。
	QuestionCorpusFarm    bool `json:"question_corpus_farm"`
	QuestionRewardRL      bool `json:"question_reward_rl"`
	QuestionJudgeEval     bool `json:"question_judge_eval"`
	QuestionDistillTarget bool `json:"question_distill_target"`

	// AlertTelegram / AlertWeCom 告警通道开关。
	AlertTelegram bool `json:"alert_telegram"`
	AlertWeCom    bool `json:"alert_wecom"`
	// WeComWebhookUrl 蒸馏告警专用的企微群机器人地址；为空则该通道不发送。
	// ⚠️ 不复用 log_alert 的规则表：那套是按错误日志过滤的规则引擎，语义与
	// 「蒸馏命中」无关，硬塞进去会让运营在两处维护同一份 webhook。
	WeComWebhookUrl string `json:"wecom_webhook_url"`

	// FallbackChannelId 蒸馏兜底渠道（全局单选）。0 = 未配置，兜底不可用。
	// ⛔ 与各渠道自己的 FallbackChannelId、以及拒答降级的兜底完全独立。
	FallbackChannelId int `json:"fallback_channel_id"`

	// DedupSeconds 同一用户同一正文哈希在该窗口内不重复打分（0 = 不去重）。
	DedupSeconds int `json:"dedup_seconds"`
}

var distillDetectSetting = DistillDetectSetting{
	Enabled:               true,
	ActivateRPM:           20, // 占位：等 Hermes 用线上数据校准后只改默认值
	SampleEvery:           10, // 占位：同上
	Threshold:             0.60,
	DemoteSeconds:         3600,
	ObserveOnly:           true, // 灰度：先只记录 + 告警
	JevBaseURL:            "",
	JevApiKey:             "",
	JevTimeoutMs:          5000,
	MaxStateChars:         16000,
	HeadSampleChars:       4000,
	QuestionCorpusFarm:    true,
	QuestionRewardRL:      true,
	QuestionJudgeEval:     true,
	QuestionDistillTarget: true,
	AlertTelegram:         false,
	AlertWeCom:            false,
	WeComWebhookUrl:       "",
	FallbackChannelId:     0,
	DedupSeconds:          600,
}

func init() {
	config.GlobalConfig.Register("distill_detect_setting", &distillDetectSetting)
}

// GetDistillDetectSetting 返回当前全局配置指针；上层只读。
func GetDistillDetectSetting() *DistillDetectSetting {
	return &distillDetectSetting
}
