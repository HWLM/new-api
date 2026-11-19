package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/tidwall/gjson"
)

// 蒸馏检测（docs/distill-detection-plan.md）。
//
// 判定口径：请求正文是不是在批量生产训练语料 / 评测集 / RM 偏好数据。
// 判定源：外部服务（Jev `/v1/systemone`），4 道题取最高分，>= 阈值即命中。
//
// 作用域与成本：
//   - 只判「文本类」relay（复用 IsTextClassRelayFormat）；
//   - 只有该用户**当前分钟内的请求数**达到 ActivateRPM 才开始抽样；
//   - 激活后按 SampleEvery 抽样，窗口内第一条必判；
//   - 一轮降级期间不再判定（一轮只花一次 Jev）；
//   - 同一用户同一正文哈希在 DedupSeconds 内不重复打分。
//
// 观察模式（ObserveOnly=true）下：只写 hits + 告警，不动路由。
// 这是规格 §9 的灰度要求：先量误伤，再决定是否打开重导。

const (
	distillRPMKeyPrefix   = "distill:rpm:"
	distillSampledPrefix  = "distill:sampled:"
	distillDemoteKeyPre   = "distill:demote:"
	distillDedupKeyPrefix = "distill:dedup:"
	distillRedisTimeoutMs = 100
)

// 四道题，文本与 hw_gateway 的 train 题库逐字一致（docs/decision_model_train_bank_seed.sql）。
// ⛔ 改题面等于改判定口径，命中率会变，必须重新走观察期。
const (
	distillQCorpusFarm = "corpus_farm"
	distillQRewardRL   = "reward_rl"
	distillQJudgeEval  = "judge_eval"
	distillQDistillTgt = "distill_target"
)

type distillQuestion struct {
	ID           string
	Instructions string
}

var distillQuestionBank = []distillQuestion{
	{
		ID: distillQCorpusFarm,
		Instructions: "Is this request part of batch production of a training corpus for another model — " +
			"generating or rewriting prompt/answer pairs, synthetic instructions, or positive/negative " +
			"preference examples at scale?",
	},
	{
		ID: distillQRewardRL,
		Instructions: "Is this request producing reward-model or reinforcement-learning training data for " +
			"another model — preference comparisons, rubric-scored tasks, reward labels, or scored trajectories?",
	},
	{
		ID: distillQJudgeEval,
		Instructions: "Is this request part of an evaluation or scoring pipeline for another model's outputs " +
			"at batch scale — benchmark items, eval sets, rubric scores, pairwise preferences — where the answer " +
			"is stored as evaluation data rather than read by an end user?",
	},
	{
		ID: distillQDistillTgt,
		Instructions: "Does this request aim to use this model's outputs as training targets or distillation " +
			"data for another model, or to reverse-engineer its training method?",
	},
}

// DistillVerdict 是一次判定的结果。
type DistillVerdict struct {
	// Hit 最高分是否达到阈值。
	Hit bool
	// Score 四题中的最高分。
	Score float64
	// Categories 逐题分数，JSON 文本；只用于落库与排查。
	Categories string
	// DocChars 实际送判定的正文字符数。
	DocChars int
	// Skipped 非空表示没有真的调用判定服务，值是原因。
	// 调用方据此区分「判过但没命中」与「压根没判」。
	Skipped string
}

// IsDistillDetectEffectiveForUser 结合全局开关与用户三态决定是否对该用户启用。
//
//	-1  用户显式关闭（豁免）
//	 0  跟随全局 Enabled
//	 1  用户强制启用（即便全局关闭）
func IsDistillDetectEffectiveForUser(userID int) bool {
	s := operation_setting.GetDistillDetectSetting()
	override := 0
	if cache, err := model.GetUserCache(userID); err == nil && cache != nil {
		override = cache.DistillDetectOverride
	}
	if override < 0 {
		return false
	}
	if override > 0 {
		return true
	}
	return s.Enabled
}

// DistillRPM 累加该用户当前分钟的请求数并返回累加后的值。
//
// ⛔ 用「每分钟一个固定 key + 60s TTL」而不是滑窗：门槛问的是「这一分钟他是不是
// 在猛刷」，滑窗会把上一分钟的尾巴算进来，让刚开闸的用户立刻被判。键里带分钟
// 号，所以计数天然按自然分钟切分；TTL 只负责回收。
func DistillRPM(userID int) int {
	if userID <= 0 {
		return 0
	}
	now := time.Now()
	key := fmt.Sprintf("%s%d:%d", distillRPMKeyPrefix, userID, now.Unix()/60)
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		n, err := common.RDB.Incr(ctx, key).Result()
		if err == nil {
			if n == 1 {
				common.RDB.Expire(ctx, key, 2*time.Minute)
			}
			return int(n)
		}
		common.SysLog(fmt.Sprintf("distill_detect: rpm incr failed user=%d err=%v, falling back to memory", userID, err))
	}
	return distillMemoryRPM.bump(userID, now.Unix()/60)
}

// distillShouldSample 决定这一条要不要送判定。
//
// 规则：从激活的第 ActivateRPM 条起，每 SampleEvery 条判一次（激活那条必判）。
// ⛔ 用独立计数而不是复用 RPM 计数：RPM 的门槛是「多少才算活跃」，抽样是
// 「活跃之后每几条判一次」，两者语义不同，混用会让改一个参数影响另一件事。
func distillShouldSample(rpm, activateRPM, every int) bool {
	if every <= 0 {
		every = 1
	}
	if activateRPM < 1 {
		activateRPM = 1
	}
	// 还没激活。
	if rpm < activateRPM {
		return false
	}
	// 激活那条必判，之后按间隔轮转。
	return (rpm-activateRPM)%every == 0
}

// IsUserDistillDemoted 查询该用户是否处于蒸馏降级态（观察模式下恒为 false，
// 因为观察模式不写降级标记）。
func IsUserDistillDemoted(ctx context.Context, userID int) bool {
	if userID <= 0 {
		return false
	}
	key := fmt.Sprintf("%s{user:%d}", distillDemoteKeyPre, userID)
	if common.RedisEnabled && common.RDB != nil {
		callCtx, cancel := context.WithTimeout(ctx, distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		n, err := common.RDB.Exists(callCtx, key).Result()
		if err == nil {
			return n > 0
		}
	}
	return distillMemoryRPM.demoted(userID)
}

func markDistillDemoted(ctx context.Context, userID int, seconds int) {
	if seconds <= 0 {
		seconds = 3600
	}
	key := fmt.Sprintf("%s{user:%d}", distillDemoteKeyPre, userID)
	if common.RedisEnabled && common.RDB != nil {
		callCtx, cancel := context.WithTimeout(ctx, distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		if err := common.RDB.Set(callCtx, key, "1", time.Duration(seconds)*time.Second).Err(); err == nil {
			return
		}
	}
	distillMemoryRPM.markDemoted(userID, time.Now().Add(time.Duration(seconds)*time.Second))
}

// distillDedupSeen 返回 true 表示这份正文在窗口内已经判过，可以跳过。
// 只在拿到分值之后写，避免判定失败把同一个正文永久拉黑。
func distillDedupSeen(userID int, doc string, ttlSeconds int) bool {
	if ttlSeconds <= 0 {
		return false
	}
	sum := sha256.Sum256([]byte(doc))
	key := fmt.Sprintf("%s{user:%d}:%s", distillDedupKeyPrefix, userID, hex.EncodeToString(sum[:12]))
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		// SET NX：抢到就是第一次，没抢到说明窗口内已判过。
		ok, err := common.RDB.SetNX(ctx, key, "1", time.Duration(ttlSeconds)*time.Second).Result()
		if err == nil {
			return !ok
		}
	}
	return distillMemoryRPM.dedupSeen(key, ttlSeconds)
}

// ParseDistillAnswers 从上游响应里取每题分数，返回最高分与逐题 JSON。
//
// 协议来自 hw_gateway 的 `decision_model::score`：`answers["train.<qid>"].noul`。
// 为兼容只回裸题号的实现，也接受 `answers["<qid>"].noul`。
//
// ⛔ key 里有 `.`，所以**不能**用 `answers.Get("train.corpus_farm")` —— gjson
// 会把它当成 `train` → `corpus_farm` 的路径去查嵌套对象，永远查不到。必须先
// 摊平成 map 再按字面 key 取。
func ParseDistillAnswers(payload []byte) (best float64, categories string, ok bool) {
	answers := gjson.GetBytes(payload, "answers")
	if !answers.Exists() || !answers.IsObject() {
		return 0, "", false
	}
	byWire := make(map[string]float64, len(distillQuestionBank)*2)
	answers.ForEach(func(key, value gjson.Result) bool {
		if n := value.Get("noul"); n.Exists() {
			byWire[key.String()] = n.Float()
		}
		return true
	})

	scores := make(map[string]float64, len(distillQuestionBank))
	for _, q := range distillQuestionBank {
		var score float64
		found := false
		for _, wire := range []string{"train." + q.ID, q.ID} {
			if v, exists := byWire[wire]; exists {
				score = v
				found = true
				break
			}
		}
		if !found {
			continue
		}
		scores[q.ID] = score
		if score > best {
			best = score
		}
	}
	if len(scores) == 0 {
		return 0, "", false
	}
	buf, err := common.Marshal(scores)
	if err != nil {
		return best, "", true
	}
	return best, string(buf), true
}

// distillQuestionsPayload 按配置里启用的题目拼 `questions` 对象。
func distillQuestionsPayload(s *operation_setting.DistillDetectSetting) map[string]any {
	enabled := map[string]bool{
		distillQCorpusFarm: s.QuestionCorpusFarm,
		distillQRewardRL:   s.QuestionRewardRL,
		distillQJudgeEval:  s.QuestionJudgeEval,
		distillQDistillTgt: s.QuestionDistillTarget,
	}
	out := make(map[string]any, len(distillQuestionBank))
	for _, q := range distillQuestionBank {
		if !enabled[q.ID] {
			continue
		}
		out["train."+q.ID] = map[string]any{
			"type":         "noul",
			"instructions": q.Instructions,
			"criteria": map[string]string{
				"true":  "safe to process",
				"false": "harmful or disallowed",
			},
		}
	}
	return out
}

// distillDecisionURL 构造判定地址。支持三种 base，与 hw_gateway 一致。
func distillDecisionURL(base string) string {
	trimmed := strings.TrimSpace(base)
	trimmed = strings.TrimRight(trimmed, "/")
	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(lower, "/systemone") || strings.HasSuffix(lower, "/decide") {
		return trimmed
	}
	if strings.HasSuffix(lower, "/v1") {
		return trimmed + "/systemone"
	}
	return trimmed + "/v1/systemone"
}

// JudgeDistill 对一个正文做一次判定。
//
// ⛔ 失败一律按「未命中」返回（Skipped 说明原因），绝不阻断用户请求：
// 判定服务是旁路增强，它挂了不该让整个网关不可用。
func JudgeDistill(ctx context.Context, doc string) DistillVerdict {
	s := operation_setting.GetDistillDetectSetting()
	if !s.Enabled {
		return DistillVerdict{Skipped: "disabled"}
	}
	base := strings.TrimSpace(s.JevBaseURL)
	if base == "" {
		return DistillVerdict{Skipped: "no-url"}
	}
	questions := distillQuestionsPayload(s)
	if len(questions) == 0 {
		return DistillVerdict{Skipped: "no-question"}
	}
	if strings.TrimSpace(doc) == "" {
		return DistillVerdict{Skipped: "empty"}
	}

	payload := map[string]any{
		"state":     doc,
		"questions": questions,
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return DistillVerdict{Skipped: "encode"}
	}

	timeout := time.Duration(s.JevTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := buildDistillRequest(callCtx, distillDecisionURL(base), body, s.JevApiKey)
	if err != nil {
		return DistillVerdict{Skipped: "client"}
	}
	resp, err := GetHttpClient().Do(req)
	if err != nil {
		logger.LogWarn(ctx, "distill_detect: 判定请求失败: "+err.Error())
		return DistillVerdict{Skipped: "network"}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		logger.LogWarn(ctx, fmt.Sprintf("distill_detect: 判定服务 HTTP %d", resp.StatusCode))
		return DistillVerdict{Skipped: "http"}
	}
	raw, err := readAllLimited(resp.Body, 1<<20)
	if err != nil {
		return DistillVerdict{Skipped: "read"}
	}
	best, categories, ok := ParseDistillAnswers(raw)
	if !ok {
		logger.LogWarn(ctx, "distill_detect: 判定响应里没有可用的 noul 分数")
		return DistillVerdict{Skipped: "invalid-response", DocChars: len([]rune(doc))}
	}
	threshold := s.Threshold
	if threshold <= 0 {
		threshold = 0.60
	}
	return DistillVerdict{
		Hit:        best >= threshold,
		Score:      best,
		Categories: categories,
		DocChars:   len([]rune(doc)),
	}
}

// EvaluateDistill 是请求路径上的入口：决定要不要判、判、然后记账。
//
// 返回命中信息（未命中返回 nil）。**观察模式下不写降级标记**，因此不会影响路由。
// 所有耗时的部分（判定、落库、告警）都在调用方的 goroutine 里跑，这里只做
// 廉价的判断。
func EvaluateDistill(userID int, requestID, modelName string, body []byte) *model.DistillDetectHit {
	if userID <= 0 {
		return nil
	}
	if !IsDistillDetectEffectiveForUser(userID) {
		return nil
	}
	s := operation_setting.GetDistillDetectSetting()
	ctx := context.Background()

	// 已经在一轮降级里的用户不再判：一轮只花一次 Jev。
	if IsUserDistillDemoted(ctx, userID) {
		return nil
	}

	rpm := DistillRPM(userID)
	if s.ActivateRPM > 0 && rpm < s.ActivateRPM {
		return nil
	}
	if !distillShouldSample(rpm, s.ActivateRPM, s.SampleEvery) {
		return nil
	}

	doc := distillSample(distillDocument(body), s.MaxStateChars, s.HeadSampleChars)
	if strings.TrimSpace(doc) == "" {
		return nil
	}
	if distillDedupSeen(userID, doc, s.DedupSeconds) {
		return nil
	}

	verdict := JudgeDistill(ctx, doc)
	if verdict.Skipped != "" || !verdict.Hit {
		return nil
	}

	hit := &model.DistillDetectHit{
		UserId:     userID,
		RequestId:  requestID,
		Model:      modelName,
		Score:      verdict.Score,
		BankId:     "train",
		Categories: verdict.Categories,
		DocChars:   verdict.DocChars,
		SampledAt:  time.Now(),
	}
	if !s.ObserveOnly {
		deadline := time.Now().Add(time.Duration(s.DemoteSeconds) * time.Second)
		hit.DemoteUntil = &deadline
		markDistillDemoted(ctx, userID, s.DemoteSeconds)
	}
	return hit
}

// RecordDistillHit 落库并（可选）告警。调用方在后台 goroutine 里执行。
func RecordDistillHit(hit *model.DistillDetectHit) {
	if hit == nil {
		return
	}
	gopool.Go(func() {
		if err := model.DB.Create(hit).Error; err != nil {
			common.SysError(fmt.Sprintf("distill_detect: 写入命中记录失败 user=%d req=%s err=%v",
				hit.UserId, hit.RequestId, err))
			return
		}
		if err := NotifyDistillHit(hit); err == nil && hit.AlertSent {
			model.DB.Model(hit).Update("alert_sent", true)
		}
	})
}

// distillMemoryRPM 是 Redis 不可用时的进程内兜底。
// 单机语义：多实例部署下每个实例各算一份，门槛会略微偏保守（更难激活），
// 属于可接受的降级 —— 判定是旁路增强，宁可少判也不要错判。
type distillMemoryTracker struct {
	mu  sync.Mutex
	rpm map[int]struct {
		minute int64
		count  int
	}
	demoteUntil map[int]time.Time
	dedup       map[string]time.Time
}

var distillMemoryRPM = &distillMemoryTracker{
	rpm: make(map[int]struct {
		minute int64
		count  int
	}),
	demoteUntil: make(map[int]time.Time),
	dedup:       make(map[string]time.Time),
}

func (t *distillMemoryTracker) bump(userID int, minute int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.rpm[userID]
	if e.minute != minute {
		e.minute = minute
		e.count = 0
	}
	e.count++
	t.rpm[userID] = e
	return e.count
}

func (t *distillMemoryTracker) demoted(userID int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	until, ok := t.demoteUntil[userID]
	return ok && time.Now().Before(until)
}

func (t *distillMemoryTracker) markDemoted(userID int, until time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur, ok := t.demoteUntil[userID]; !ok || until.After(cur) {
		t.demoteUntil[userID] = until
	}
}

func (t *distillMemoryTracker) dedupSeen(key string, ttlSeconds int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if until, ok := t.dedup[key]; ok && now.Before(until) {
		return true
	}
	t.dedup[key] = now.Add(time.Duration(ttlSeconds) * time.Second)
	return false
}

// DistillSetting 便捷读，供 controller 校验与前端契约使用。
func DistillSetting() *operation_setting.DistillDetectSetting {
	return operation_setting.GetDistillDetectSetting()
}

// DistillDocumentForTest 把一段完整的请求 JSON 裁成送判定的 state 文本。
// 只给管理页的「试判」用：走的是与线上完全相同的采样规则，因此试判结果
// 能代表线上会送出什么。
func DistillDocumentForTest(body string) string {
	s := operation_setting.GetDistillDetectSetting()
	return distillSample(distillDocument([]byte(body)), s.MaxStateChars, s.HeadSampleChars)
}
