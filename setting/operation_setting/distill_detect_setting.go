package operation_setting

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// DistillQuestion 是一道判定题。
//
// 题库是**数据**：管理页可以新增、删除、改题面、启停、排序。判定服务按当前
// 启用的题遍历取最高分，`categories` 存 `{题目id: 分数}`。
type DistillQuestion struct {
	ID      string `json:"id"`      // [a-z0-9_]{1,32}，唯一
	Name    string `json:"name"`    // 展示用短名，1..64
	Prompt  string `json:"prompt"`  // 题面正文，1..8000 字符
	Enabled bool   `json:"enabled"` // 关掉的题不送判定
}

// 蒸馏题库的规模与字段约束。后端强制、前端同步，两边用同一组数。
const (
	DistillQuestionMinCount     = 1
	DistillQuestionMaxCount     = 20
	DistillQuestionMaxIDLen     = 32
	DistillQuestionMaxNameLen   = 64
	DistillQuestionMaxPromptLen = 8000
)

var distillQuestionIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// DefaultDistillQuestions 返回内置题库：hw_gateway「训练数据与评测」题库的
// 四道题，题面**逐字照搬** crates/portunex-gateway/src/services/decision_model/
// config.rs 的 `BUILTIN_TRAIN_QUESTIONS`。
//
// ⛔ 不要改写、压缩或翻译这些题面：判定口径就是这几句话，改一个字命中率都会变，
// 已有的观察期数据也就失去可比性。
func DefaultDistillQuestions() []DistillQuestion {
	return []DistillQuestion{
		{
			ID:     "corpus_farm",
			Name:   "批量语料生产",
			Prompt: "Is this request part of batch production of a training corpus for another model — generating or rewriting prompt/answer pairs, synthetic instructions, or positive/negative preference examples at scale?",
			// hw_gateway 的 BUILTIN_TRAIN_ENABLED 只默认开 corpus_farm 与
			// distill_target；这里四题全开 —— new-api 侧只有这一个题库，
			// 题目的目标就是「训练数据与评测」本身，不需要靠关题收窄口径。
			Enabled: true,
		},
		{
			ID:      "reward_rl",
			Name:    "RM / RL 偏好数据",
			Prompt:  "Is this request producing reward-model or reinforcement-learning training data for another model — preference comparisons, rubric-scored tasks, reward labels, or scored trajectories?",
			Enabled: true,
		},
		{
			ID:      "judge_eval",
			Name:    "LLM-as-judge 评测",
			Prompt:  "Is this request part of an evaluation or scoring pipeline for another model's outputs at batch scale — benchmark items, eval sets, rubric scores, pairwise preferences — where the answer is stored as evaluation data rather than read by an end user?",
			Enabled: true,
		},
		{
			ID:      "distill_target",
			Name:    "输出当训练目标",
			Prompt:  "Does this request aim to use this model's outputs as training targets or distillation data for another model, or to reverse-engineer its training method?",
			Enabled: true,
		},
	}
}

// ValidateDistillQuestions 校验题库。越界一律返回错误而不是静默修正 ——
// 静默修正会让运营以为改生效了，实际跑的是另一套题。
func ValidateDistillQuestions(qs []DistillQuestion) error {
	if len(qs) < DistillQuestionMinCount {
		return fmt.Errorf("题库至少要有 %d 道题", DistillQuestionMinCount)
	}
	if len(qs) > DistillQuestionMaxCount {
		return fmt.Errorf("题库最多 %d 道题，当前 %d 道", DistillQuestionMaxCount, len(qs))
	}
	seen := make(map[string]bool, len(qs))
	enabled := 0
	for i, q := range qs {
		id := strings.TrimSpace(q.ID)
		if !distillQuestionIDPattern.MatchString(id) {
			return fmt.Errorf("第 %d 题的 id %q 不合法：只允许小写字母、数字、下划线，长度 1~%d", i+1, q.ID, DistillQuestionMaxIDLen)
		}
		if seen[id] {
			return fmt.Errorf("题目 id %q 重复", id)
		}
		seen[id] = true
		name := strings.TrimSpace(q.Name)
		if name == "" || len([]rune(name)) > DistillQuestionMaxNameLen {
			return fmt.Errorf("第 %d 题（%s）的名称必须为 1~%d 个字符", i+1, id, DistillQuestionMaxNameLen)
		}
		prompt := strings.TrimSpace(q.Prompt)
		if prompt == "" || len([]rune(prompt)) > DistillQuestionMaxPromptLen {
			return fmt.Errorf("第 %d 题（%s）的题面必须为 1~%d 个字符", i+1, id, DistillQuestionMaxPromptLen)
		}
		if q.Enabled {
			enabled++
		}
	}
	// ⛔ 全关必须拒绝：一道题都不启用时判定永远不命中，功能静默失效，
	// 而管理页看起来「一切正常」—— 这种失败最难被发现。
	if enabled == 0 {
		return fmt.Errorf("至少要启用一道题，否则判定永远不会命中")
	}
	return nil
}

// NormalizeDistillQuestions 去掉 id/name/prompt 两端空白，返回可落库的副本。
func NormalizeDistillQuestions(qs []DistillQuestion) []DistillQuestion {
	out := make([]DistillQuestion, 0, len(qs))
	for _, q := range qs {
		out = append(out, DistillQuestion{
			ID:      strings.TrimSpace(q.ID),
			Name:    strings.TrimSpace(q.Name),
			Prompt:  strings.TrimSpace(q.Prompt),
			Enabled: q.Enabled,
		})
	}
	return out
}

// EnabledDistillQuestions 返回当前启用的题（按配置顺序）。
func (s *DistillDetectSetting) EnabledDistillQuestions() []DistillQuestion {
	out := make([]DistillQuestion, 0, len(s.Questions))
	for _, q := range s.Questions {
		if q.Enabled {
			out = append(out, q)
		}
	}
	return out
}

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

	// ActivateRPM 每分钟请求量激活阈值：该用户当前分钟内的请求数达到该值才开始判定。
	// 0 或负数表示「不设门槛」（任何请求都可能被判定），一般不这么配。
	ActivateRPM int `json:"activate_rpm"`

	// JudgeIntervalSeconds 判定间隔：同一用户在该窗口内最多判 1 次。
	// 默认 30 秒 ⇒ 每分钟上限 2 次。桶键 `distill:bucket:<user>:<floor(now/interval)>`
	// 用 SET NX 抢首条，TTL 跟着本值走。
	//
	// ⛔ 取代了早期的「每 N 条请求抽 1」：实测线上 24h 有 157 万请求，那条路
	// 最省一档也要 3.2 万次/日，成本压不住。按时间桶才可控 —— 见规格 §11。
	JudgeIntervalSeconds int `json:"judge_interval_seconds"`

	// ConfirmHits 命中确认次数：确认窗口内累计命中达到该次数才真正降级。
	// ⛔ 前几次命中只写 hits、不降级 —— 这是「不因单条误判就把整段流量切走」
	// 的保证。真蒸馏用户命中率高（回放实测 9/10），2–3 分钟就能凑够。
	ConfirmHits int `json:"confirm_hits"`

	// ConfirmWindowSeconds 命中确认窗口：滑动计数、不清零，超出窗口的命中过期。
	// ⛔ 不用固定窗口：单次命中率一旦掉到 0.5 以下，固定窗口会漏掉一大半。
	ConfirmWindowSeconds int `json:"confirm_window_seconds"`

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

	// Questions 题库。默认 = hw_gateway「训练数据与评测」题库的四道题，题面逐字
	// 照搬（见 BUILTIN_TRAIN_QUESTIONS）。⛔ 题库是**数据**不是代码：管理页可以
	// 增删改、启停、排序，判定服务按当前启用的题遍历取最高分。
	Questions []DistillQuestion `json:"questions"`

	// AlertTelegram / AlertWeCom 告警通道开关。
	AlertTelegram bool `json:"alert_telegram"`
	AlertWeCom    bool `json:"alert_wecom"`
	// TgBotToken / TgChatId 蒸馏告警专用的 Telegram 通道。
	//
	// ⛔ 完全独立：不读取、不复用 tg_notify_bot_token / tg_notify_overload_chat_id
	// 那套通知配置。两套互不影响，运营改这里不会动到系统通知，反之亦然。
	TgBotToken string `json:"tg_bot_token"`
	TgChatId   string `json:"tg_chat_id"`
	// AlertThrottleSeconds 同一用户的告警节流窗口：窗口内最多 1 条。
	// 0 = 不节流（每次命中都发）。
	//
	// ⚠️ 默认 600 秒。观察模式下没有「一轮降级只判一次」的抑制，30 秒桶
	// 意味着单用户约 2 判/分，按命中率 0.9 就是 ~1.8 条告警/分 —— 不节流
	// 一旦打开 TG/企微就是刷屏。命中流水本身不节流，运营要的完整名单在
	// 管理页的 hits 列表里。
	AlertThrottleSeconds int `json:"alert_throttle_seconds"`
	// WeComWebhookUrl 蒸馏告警专用的企微群机器人地址；为空则该通道不发送。
	// ⚠️ 不复用 log_alert 的规则表：那套是按错误日志过滤的规则引擎，语义与
	// 「蒸馏命中」无关，硬塞进去会让运营在两处维护同一份 webhook。
	WeComWebhookUrl string `json:"wecom_webhook_url"`

	// DetectChannelIds 检测渠道白名单（规格 §16）：只有本次请求**分发到的渠道**
	// 在列表里，才进入检测链路（计数、时间桶、去重、判定、hits、告警）。
	// 不在列表里的渠道零开销 —— 连每分钟请求量都不累加。
	//
	// ⛔ 空列表 = 关闭检测，等价于 Enabled=false。不是「对所有渠道生效」：
	// 那会让「开了检测却什么都没配」看起来像在跑，实际一条都没判。
	//
	// ⛔ 与 FallbackChannelId 互斥：同一个渠道不能既当检测渠道又当兜底渠道，
	// 保存时由 CheckDistillChannelConflict 拒绝。
	DetectChannelIds []int `json:"detect_channel_ids"`

	// FallbackChannelId 蒸馏兜底渠道（全局单选）。0 = 未配置，兜底不可用。
	// ⛔ 与各渠道自己的 FallbackChannelId、以及拒答降级的兜底完全独立。
	FallbackChannelId int `json:"fallback_channel_id"`

	// DedupSeconds 同一用户同一正文哈希在该窗口内不重复打分（0 = 不去重）。
	DedupSeconds int `json:"dedup_seconds"`
}

var distillDetectSetting = DistillDetectSetting{
	Enabled:              true,
	ActivateRPM:          20,
	JudgeIntervalSeconds: 30,
	ConfirmHits:          4,
	ConfirmWindowSeconds: 300,
	Threshold:            0.60,
	DemoteSeconds:        3600,
	ObserveOnly:          true, // 灰度：先只记录 + 告警
	JevBaseURL:           "",
	JevApiKey:            "",
	JevTimeoutMs:         5000,
	MaxStateChars:        16000,
	HeadSampleChars:      4000,
	Questions:            DefaultDistillQuestions(),
	AlertTelegram:        false,
	AlertWeCom:           false,
	TgBotToken:           "",
	TgChatId:             "",
	AlertThrottleSeconds: 600,
	WeComWebhookUrl:      "",
	FallbackChannelId:    0,
	DedupSeconds:         600,
}

func init() {
	config.GlobalConfig.Register("distill_detect_setting", &distillDetectSetting)
}

// GetDistillDetectSetting 返回当前全局配置指针；上层只读。
func GetDistillDetectSetting() *DistillDetectSetting {
	return &distillDetectSetting
}

// DetectsChannel 判断「本次请求分发到的渠道」是否在检测白名单里（规格 §16）。
//
// ⛔ 只看白名单，不看 Enabled / 用户三态：那两个条件由
// service.IsDistillDetectEffectiveForUser 管，混进来的话用户级强制启用
// （override>0）会被全局开关悄悄抹掉。
//
// ⛔ 空列表 = 一个渠道都不检测：配置项没填时检测整体关闭，这是明确语义，
// 不是「对所有渠道生效」。
func (s *DistillDetectSetting) DetectsChannel(channelID int) bool {
	if s == nil || channelID <= 0 || len(s.DetectChannelIds) == 0 {
		return false
	}
	for _, id := range s.DetectChannelIds {
		if id == channelID {
			return true
		}
	}
	return false
}

// NormalizeDistillDetectChannelIds 去重并丢掉非正数，返回可落库的副本。
// 保持运营选择时的顺序，管理页回显才稳定。
func NormalizeDistillDetectChannelIds(ids []int) []int {
	out := make([]int, 0, len(ids))
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// DistillChannelConflictError 表示同一个渠道被同时配成检测渠道和兜底渠道。
//
// ⛔ 这两种角色必须互斥：检测渠道的请求会被判定并可能被切到兜底渠道，
// 兜底渠道自己再进判定就成了自环（判定 → 切到自己 → 再判定）。
type DistillChannelConflictError struct {
	ChannelID int
}

func (e *DistillChannelConflictError) Error() string {
	return fmt.Sprintf("渠道 %d 不能同时作为检测渠道和兜底渠道", e.ChannelID)
}

// CheckDistillChannelConflict 校验检测渠道与兜底渠道互斥。
// 返回 nil 表示没有冲突。
func CheckDistillChannelConflict(detectChannelIds []int, fallbackChannelId int) error {
	if fallbackChannelId <= 0 {
		return nil
	}
	for _, id := range detectChannelIds {
		if id == fallbackChannelId {
			return &DistillChannelConflictError{ChannelID: id}
		}
	}
	return nil
}

// distillLegacyQuestionKeys 是题库改数据化之前的四个布尔开关在 option 表里的键。
// 顺序与默认题库一致。
var distillLegacyQuestionKeys = []struct {
	ID  string
	Key string
}{
	{"corpus_farm", "distill_detect_setting.question_corpus_farm"},
	{"reward_rl", "distill_detect_setting.question_reward_rl"},
	{"judge_eval", "distill_detect_setting.question_judge_eval"},
	{"distill_target", "distill_detect_setting.question_distill_target"},
}

// MigrateLegacyDistillQuestionToggles 把旧版的四个 `question_*` 开关映射成
// 对应题目的 enabled 位。
//
// ⛔ `questionsPersisted` 必须来自**数据库里有没有 `distill_detect_setting.questions`
// 这一行**，不能看 common.OptionMap 里有没有这个键 —— InitOptionMap 会先跑
// `config.GlobalConfig.ExportAllConfigs()`，把默认题库的 JSON 预填进 OptionMap，
// 于是「看 OptionMap」的守卫永远命中，迁移就一次也不会执行。这是实测踩到的：
// 库里 question_reward_rl=false，升级后页面上却是 enabled=true。
//
// ⛔ 只有数据库里没有 questions 行时才生效：一旦用新版保存过题库，老键永久
// 失效 —— 否则运营删掉或改名过的题，会被遗留的老开关反复覆盖。
//
// ⛔ 老配置把四题全关时恢复默认题库：那样判定永远不命中，功能静默失效。
func MigrateLegacyDistillQuestionToggles(questionsPersisted bool) {
	if questionsPersisted {
		return
	}
	applied := false
	for _, legacy := range distillLegacyQuestionKeys {
		raw, ok := common.OptionMap[legacy.Key]
		if !ok {
			continue
		}
		enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		applied = true
		for i := range distillDetectSetting.Questions {
			if distillDetectSetting.Questions[i].ID == legacy.ID {
				distillDetectSetting.Questions[i].Enabled = enabled
			}
		}
	}
	if applied && len(distillDetectSetting.EnabledDistillQuestions()) == 0 {
		distillDetectSetting.Questions = DefaultDistillQuestions()
	}
}
