package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/go-redis/redis/v8"
	"github.com/tidwall/gjson"
)

// 蒸馏检测（docs/distill-detection-plan.md）。
//
// 判定口径：请求正文是不是在批量生产训练语料 / 评测集 / RM 偏好数据。
// 判定源：外部服务（Jev `/v1/systemone`），4 道题取最高分，>= 阈值即命中。
//
// 作用域与成本：
//   - 只判「文本类」relay（复用 IsTextClassRelayFormat）；
//   - 只有该用户**当前分钟内的请求数**达到 ActivateRPM 才开始判定；
//   - 判定按**时间桶**抽样：同一用户每 JudgeIntervalSeconds 只判首条
//     （默认 30s ⇒ 每分钟最多 2 次）。⛔ 不是「每 N 条请求抽 1」—— 实测
//     线上 157 万请求/日，那条路最省也要 3.2 万次调用，成本压不住；
//   - 一轮降级期间不再判定（一轮只花一次 Jev）；
//   - 同一用户同一正文哈希在 DedupSeconds 内不重复打分；
//   - 命中要**连续确认**：ConfirmWindowSeconds 滑动窗口内累计 ConfirmHits 次
//     才真正降级，未达到的命中只写 hits。
//
// 观察模式（ObserveOnly=true）下：只写 hits + 告警，不动路由。
// 这是规格 §9 的灰度要求：先量误伤，再决定是否打开重导。

const (
	distillRPMKeyPrefix     = "distill:rpm:"
	distillBucketKeyPrefix  = "distill:bucket:"
	distillConfirmKeyPrefix = "distill:confirm:"
	distillDemoteKeyPre     = "distill:demote:"
	distillDedupKeyPrefix   = "distill:dedup:"
	distillRedisTimeoutMs   = 100
)

// 内置题库的四道题 id。题面本身在 operation_setting.DefaultDistillQuestions()
// 里（与 hw_gateway 的 train 题库逐字一致），这里只保留 id 常量给默认值与测试用。
//
// ⛔ 题库现在是**数据**：管理页可以增删改。判定一律遍历配置里启用的题，
// 不再假设只有这四道。
const (
	distillQCorpusFarm = "corpus_farm"
	distillQRewardRL   = "reward_rl"
	distillQJudgeEval  = "judge_eval"
	distillQDistillTgt = "distill_target"
)

// DistillVerdict 是一次判定的结果。
type DistillVerdict struct {
	// Hit 最高分是否达到阈值。
	Hit bool
	// Score 四题中的最高分。
	Score float64
	// Categories 逐题分数，JSON 文本；只用于落库与排查。
	Categories string
	// QuestionResults 逐题快照 JSON（题名 + 分值 + 当时的 hit），
	// 在判定成功后就地生成；去重复用时直接搬这一份，不重算。
	QuestionResults string
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

// distillTakeBucket 抢占该用户在当前判定窗口的「首条」名额。
//
// 返回 true 表示这条请求是它所在时间桶里的第一条，应当送判定。
//
// ⛔ 抽样单位是**时间桶**，不是请求条数。早期的「每 N 条请求抽 1」被实测否掉了：
// 线上 24h 有 157 万请求，最省一档也要 3.2 万次/日。按时间桶后，一个持续高
// 负载的用户每分钟最多判 60/interval 次（默认 30s ⇒ 2 次），成本与请求量脱钩。
//
// 桶键带桶序号，所以窗口是「自然对齐」的：同一条 30 秒里只有一个请求能抢到。
// TTL 跟间隔走，避免桶键常驻。
func distillTakeBucket(userID, intervalSeconds int) bool {
	if userID <= 0 {
		return false
	}
	if intervalSeconds <= 0 {
		intervalSeconds = 30
	}
	now := time.Now()
	slot := now.Unix() / int64(intervalSeconds)
	key := fmt.Sprintf("%s%d:%d", distillBucketKeyPrefix, userID, slot)
	// TTL 至少留一个完整窗口，再放一点余量给时钟抖动。
	ttl := time.Duration(intervalSeconds) * time.Second
	ttl += ttl / 2

	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		ok, err := common.RDB.SetNX(ctx, key, "1", ttl).Result()
		if err == nil {
			return ok
		}
		common.SysLog(fmt.Sprintf("distill_detect: bucket setnx failed user=%d err=%v, falling back to memory", userID, err))
	}
	return distillMemoryRPM.takeBucket(userID, slot)
}

// distillRecordConfirmedHit 把一个命中记进确认窗口，返回窗口内累计命中数。
//
// ⛔ 滑动计数、不清零：用 ZSET（score=毫秒时间戳，member=唯一命中 id），每次
// 先剔掉窗口外的成员再计数。固定窗口在单次命中率掉到 0.5 以下时会漏掉一大半
// 真阳性 —— 因为窗口一翻转就从头开始数。
func distillRecordConfirmedHit(userID, windowSeconds int) int {
	if userID <= 0 {
		return 0
	}
	if windowSeconds <= 0 {
		windowSeconds = 300
	}
	now := time.Now()
	cutoff := now.Add(-time.Duration(windowSeconds) * time.Second).UnixMilli()
	member := fmt.Sprintf("%d-%d", now.UnixNano(), now.UnixMicro()%1000)

	key := fmt.Sprintf("%s{user:%d}", distillConfirmKeyPrefix, userID)
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		pipe := common.RDB.TxPipeline()
		pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(cutoff, 10))
		pipe.ZAdd(ctx, key, &redis.Z{Score: float64(now.UnixMilli()), Member: member})
		pipe.ZCard(ctx, key)
		pipe.Expire(ctx, key, time.Duration(windowSeconds)*time.Second)
		cmds, err := pipe.Exec(ctx)
		if err == nil && len(cmds) == 4 {
			if n, err := cmds[2].(*redis.IntCmd).Result(); err == nil {
				return int(n)
			}
		}
		common.SysLog(fmt.Sprintf("distill_detect: confirm zset failed user=%d err=%v, falling back to memory", userID, err))
	}
	return distillMemoryRPM.recordConfirm(userID, now, windowSeconds)
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

// distillDedupKey 拼一份正文档的去重键。
func distillDedupKey(userID int, doc string) string {
	sum := sha256.Sum256([]byte(doc))
	return fmt.Sprintf("%s{user:%d}:%s", distillDedupKeyPrefix, userID, hex.EncodeToString(sum[:12]))
}

// distillDedupVerdict 是去重窗口里缓存的那次判定结果（规格 §19）。
//
// ⛔ 缓存的是**判定当时的结论**，不只是「判过」这个事实：蒸馏用户通常反复发同一段
// prompt，去重只该省掉 Jev 调用，不该省掉命中计数 —— 否则同一段正文无论重发多少次，
// 4 次确认永远凑不齐，正好把最容易抓的那类用户放过去。
type distillDedupVerdict struct {
	Hit             bool    `json:"hit"`
	Score           float64 `json:"score"`
	Categories      string  `json:"categories"`
	QuestionResults string  `json:"question_results"`
	DocChars        int     `json:"doc_chars"`
}

// distillDedupLookup 查这份正文在窗口内的上次判定结果。
//
// 返回 seen=true 表示窗口内判过；prior 为 nil 表示**判过但结果不可用**
// （升级前写的老值 "1"、或者解析失败）—— 那种情况按老行为静默跳过，
// 只影响升级那一刻还在窗口里的正文（最长 DedupSeconds）。
func distillDedupLookup(userID int, doc string) (prior *distillDedupVerdict, seen bool) {
	key := distillDedupKey(userID, doc)
	raw := ""
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		value, err := common.RDB.Get(ctx, key).Result()
		if err == nil {
			raw = value
			seen = true
		}
	}
	if !seen {
		raw, seen = distillMemoryRPM.dedupValue(key)
	}
	if !seen {
		return nil, false
	}
	var record distillDedupVerdict
	if err := common.UnmarshalJsonStr(raw, &record); err != nil {
		return nil, true
	}
	return &record, true
}

// distillDedupMark 记下「这份正文刚判定成功」，窗口内不再重复打分。
//
// ⛔ 只在 JudgeDistill 返回了真实分值之后调用（命中与未命中都算判定成功）；
// Skipped 非空意味着压根没判成，绝不能写。
func distillDedupMark(userID int, doc string, ttlSeconds int, verdict DistillVerdict) {
	if ttlSeconds <= 0 {
		return
	}
	key := distillDedupKey(userID, doc)
	record := distillDedupVerdict{
		Hit:             verdict.Hit,
		Score:           verdict.Score,
		Categories:      verdict.Categories,
		QuestionResults: verdict.QuestionResults,
		DocChars:        verdict.DocChars,
	}
	payload, err := common.Marshal(record)
	if err != nil {
		return
	}
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), distillRedisTimeoutMs*time.Millisecond)
		defer cancel()
		if err := common.RDB.Set(ctx, key, string(payload), time.Duration(ttlSeconds)*time.Second).Err(); err == nil {
			return
		}
	}
	distillMemoryRPM.dedupMark(key, ttlSeconds, string(payload))
}

// distillResolveVerdict 在「同一正文已经在去重窗口内判过」时决定本次怎么走（规格 §19）。
//
//   - 上次**没命中**：没有新信息，静默跳过（skip=true）—— 不调 Jev、不写 hits、
//     不计数；
//   - 上次**命中**：不调 Jev，复用当时的分数与逐题快照（reused=true），
//     照常记一次确认命中。⛔ 复用的是判定当时的结论，不按现在的阈值重算。
//
// 没有上次记录时交给 judge。
//
// 抽成纯函数是为了能直接断言「窗口内不再调 Jev」与「命中照样累加」，
// 不必为了测一条判定顺序去搭 Redis 和库。
func distillResolveVerdict(prior *distillDedupVerdict, seen bool, judge func() DistillVerdict) (verdict DistillVerdict, reused bool, skip bool) {
	if !seen {
		return judge(), false, false
	}
	if prior == nil || !prior.Hit {
		return DistillVerdict{}, false, true
	}
	return DistillVerdict{
		Hit:             true,
		Score:           prior.Score,
		Categories:      prior.Categories,
		QuestionResults: prior.QuestionResults,
		DocChars:        prior.DocChars,
	}, true, false
}

// ParseDistillAnswers 从上游响应里取每题分数，返回最高分与逐题 JSON。
//
// 用当前配置里启用的题做解析，等价于 ParseDistillAnswersFor(payload, 启用题)。
func ParseDistillAnswers(payload []byte) (best float64, categories string, ok bool) {
	return ParseDistillAnswersFor(payload, operation_setting.GetDistillDetectSetting().EnabledDistillQuestions())
}

// ParseDistillAnswersFor 按给定的题集解析上游响应。
//
// 协议来自 hw_gateway 的 `decision_model::score`：`answers["train.<qid>"].noul`。
// 为兼容只回裸题号的实现，也接受 `answers["<qid>"].noul`。
//
// ⛔ key 里有 `.`，所以**不能**用 `answers.Get("train.corpus_farm")` —— gjson
// 会把它当成 `train` → `corpus_farm` 的路径去查嵌套对象，永远查不到。必须先
// 摊平成 map 再按字面 key 取。
//
// ⛔ 只认**送出去的题**：返回的 answers 里若带了没问过的 key，直接忽略，
// 免得题库改小之后旧答案把分数抬上去。
func ParseDistillAnswersFor(payload []byte, questions []operation_setting.DistillQuestion) (best float64, categories string, ok bool) {
	answers := gjson.GetBytes(payload, "answers")
	if !answers.Exists() || !answers.IsObject() {
		return 0, "", false
	}
	byWire := make(map[string]float64, len(questions)*2)
	answers.ForEach(func(key, value gjson.Result) bool {
		if n := value.Get("noul"); n.Exists() {
			byWire[key.String()] = n.Float()
		}
		return true
	})

	scores := make(map[string]float64, len(questions))
	for _, q := range questions {
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
//
// ⛔ 一次请求带上所有启用题（不是逐题各发一次）：hw_gateway 的
// `questions_payload` 就是这么做的，上游一次回全部 answers。成本模型
// （28,657 次/日）就是按「每次判定 = 1 次调用」算的，改成逐题调用会直接翻倍
// 到 4 倍，与用户「成本不能高」的硬约束冲突。
func distillQuestionsPayload(s *operation_setting.DistillDetectSetting) map[string]any {
	enabled := s.EnabledDistillQuestions()
	out := make(map[string]any, len(enabled))
	for _, q := range enabled {
		out["train."+q.ID] = map[string]any{
			"type":         "noul",
			"instructions": q.Prompt,
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

// JevCallMeta 描述「这次判定属于哪个用户、哪个请求」。
//
// 只用于调用流水（distill_jev_calls）：判定函数本身不需要这些字段，
// 但它们是从请求上下文一路带到判定点的、最便宜的一次传递。
type JevCallMeta struct {
	UserID    int
	RequestID string
	Model     string
	ChannelID int
	// RPM 发这次判定时该用户当前分钟的请求计数（激活阈值的判定依据）。
	RPM int
}

// recordJevCall 落一行调用流水。失败只记日志，绝不影响判定结果。
//
// ⛔ 同步写：一次 Jev 调用本身就要几百毫秒到几秒，一条小 INSERT 的耗时是噪声；
// 放后台反而会让「刚判完就查」的人查不到。DB 未初始化（早期启动/单测）时跳过。
func recordJevCall(meta JevCallMeta, outcome string, httpStatus int, score *float64, threshold float64, durationMs int64, errMsg string) {
	if model.DB == nil {
		return
	}
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	row := &model.DistillJevCall{
		UserId:     meta.UserID,
		RequestId:  meta.RequestID,
		Model:      meta.Model,
		ChannelId:  meta.ChannelID,
		Rpm:        meta.RPM,
		Outcome:    outcome,
		HttpStatus: httpStatus,
		Score:      score,
		Threshold:  threshold,
		DurationMs: durationMs,
		Error:      errMsg,
		CreatedAt:  time.Now(),
	}
	if err := model.DB.Create(row).Error; err != nil {
		common.SysError(fmt.Sprintf("distill_detect: 写入 Jev 调用记录失败 user=%d req=%s err=%v",
			meta.UserID, meta.RequestID, err))
	}
}

// JudgeDistill 对一个正文做一次判定。
//
// ⛔ 失败一律按「未命中」返回（Skipped 说明原因），绝不阻断用户请求：
// 判定服务是旁路增强，它挂了不该让整个网关不可用。
func JudgeDistill(ctx context.Context, doc string, meta JevCallMeta) DistillVerdict {
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

	threshold := s.Threshold
	if threshold <= 0 {
		threshold = 0.60
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
	// ⛔ 没跑过 InitHttpClient（单测、极早期启动）时 httpClient 是 nil，
	// 直接调 Do 会空指针把整个请求打挂。判定是旁路能力，退回默认客户端即可。
	client := GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	// ── 从这里开始是**真实调用**：无论成败都记一行调用流水 ──────────────
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		elapsed := time.Since(started).Milliseconds()
		outcome := "network_error"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			outcome = "timeout"
		}
		recordJevCall(meta, outcome, 0, nil, threshold, elapsed, err.Error())
		logger.LogWarn(ctx, "distill_detect: 判定请求失败: "+err.Error())
		return DistillVerdict{Skipped: "network"}
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		elapsed := time.Since(started).Milliseconds()
		recordJevCall(meta, "http_error", resp.StatusCode, nil, threshold, elapsed,
			fmt.Sprintf("判定服务返回 HTTP %d", resp.StatusCode))
		logger.LogWarn(ctx, fmt.Sprintf("distill_detect: 判定服务 HTTP %d", resp.StatusCode))
		return DistillVerdict{Skipped: "http"}
	}
	raw, err := readAllLimited(resp.Body, 1<<20)
	if err != nil {
		elapsed := time.Since(started).Milliseconds()
		recordJevCall(meta, "network_error", resp.StatusCode, nil, threshold, elapsed, "读取响应失败: "+err.Error())
		return DistillVerdict{Skipped: "read"}
	}
	best, categories, ok := ParseDistillAnswers(raw)
	if !ok {
		elapsed := time.Since(started).Milliseconds()
		recordJevCall(meta, "parse_error", resp.StatusCode, nil, threshold, elapsed, "响应里没有可用的 noul 分数")
		logger.LogWarn(ctx, "distill_detect: 判定响应里没有可用的 noul 分数")
		return DistillVerdict{Skipped: "invalid-response", DocChars: len([]rune(doc))}
	}
	elapsed := time.Since(started).Milliseconds()
	recordJevCall(meta, "success", resp.StatusCode, &best, threshold, elapsed, "")
	return DistillVerdict{
		Hit:        best >= threshold,
		Score:      best,
		Categories: categories,
		DocChars:   len([]rune(doc)),
	}
}

// PersistDistillHitSync 同步落库一条命中（含告警）。
//
// ⛔ 重导模式必须用这个而不是 RecordDistillHit 的后台版：那次请求本身就要被切到
// 兜底渠道，运营随后可能立刻拿 request_id 来查，记录晚到就会查不到。
func PersistDistillHitSync(hit *model.DistillDetectHit) {
	if hit == nil {
		return
	}
	if err := model.DB.Create(hit).Error; err != nil {
		common.SysError(fmt.Sprintf("distill_detect: 写入命中记录失败 user=%d req=%s err=%v",
			hit.UserId, hit.RequestId, err))
		return
	}
	gopool.Go(func() {
		if err := NotifyDistillHit(hit); err == nil && hit.AlertSent {
			model.DB.Model(&model.DistillDetectHit{}).Where("id = ?", hit.Id).Update("alert_sent", true)
		}
	})
}

// UpdateDistillHitRouting 回填这条命中「最后实际走了哪个渠道」。
//
// 观察模式下 fallbackID 恒为 0；重导模式下：
//   - 重导成功 → fallbackID = 兜底渠道，channelId 保留原渠道；
//   - 兜底不可用 → 两者都是原渠道、fallbackID=0，运营据此看到「应该重导但没成」。
//
// 只在请求结束后调用一次；失败仅记日志，不影响用户请求。
func UpdateDistillHitRouting(hit *model.DistillDetectHit, channelID, fallbackID int) {
	if hit == nil || hit.Id <= 0 {
		return
	}
	// channelID<=0：请求在选渠道之前就被拒了（例如兜底不可用直接 429），
	// 此时保留原记录里的渠道，只清掉「已重导」的痕迹。
	var updates map[string]any
	if channelID > 0 {
		updates = map[string]any{"channel_id": channelID, "fallback_id": fallbackID}
	} else {
		updates = map[string]any{"fallback_id": 0}
	}
	if err := model.DB.Model(&model.DistillDetectHit{}).Where("id = ?", hit.Id).
		Updates(updates).Error; err != nil {
		common.SysLog(fmt.Sprintf("distill_detect: 回填渠道信息失败 id=%d err=%v", hit.Id, err))
	}
}

// DistillQuestionResult 是一条命中记录里某道题的**快照**（规格 §18）。
//
// ⛔ 题名与 hit 都在写入时定死：题目以后改名/删除、阈值以后调整，历史记录
// 仍然显示当时判定时看到的东西 —— 拿现在的配置去解释历史记录，等于改判。
type DistillQuestionResult struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Score float64 `json:"score"`
	Hit   bool    `json:"hit"`
}

// BuildDistillQuestionResults 把 categories（`{题目id: 分数}`）配上题名与阈值，
// 生成逐题快照 JSON。
//
// 同一函数两处用：
//   - 写入命中时：传**当时**的题库与阈值，结果落进 hits.question_results；
//   - 老记录展示时（这一列为空）：传**当前**题库与阈值，用 categories 兜底 ——
//     那时候没有快照，只能这样近似，并在前端注明是老记录。
//
// 输出顺序：先按题库顺序（运营看惯的顺序），题库里已经没有的题（被删了）
// 排在后面、按 id 排序，名字退回 id。
func BuildDistillQuestionResults(categories string, questions []operation_setting.DistillQuestion, threshold float64) string {
	scores, ok := parseDistillCategoryScores(categories)
	if !ok || len(scores) == 0 {
		return ""
	}
	order := make(map[string]int, len(questions))
	names := make(map[string]string, len(questions))
	for i, q := range questions {
		order[q.ID] = i
		names[q.ID] = q.Name
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		oi, iKnown := order[ids[i]]
		oj, jKnown := order[ids[j]]
		switch {
		case iKnown && jKnown:
			return oi < oj
		case iKnown != jKnown:
			return iKnown
		default:
			return ids[i] < ids[j]
		}
	})

	results := make([]DistillQuestionResult, 0, len(ids))
	for _, id := range ids {
		name := names[id]
		if strings.TrimSpace(name) == "" {
			name = id
		}
		score := scores[id]
		results = append(results, DistillQuestionResult{
			ID:    id,
			Name:  name,
			Score: score,
			Hit:   score >= threshold,
		})
	}
	buf, err := common.Marshal(results)
	if err != nil {
		return ""
	}
	return string(buf)
}

// ParseDistillQuestionResults 解析落库的逐题快照；空或坏数据都返回 nil。
func ParseDistillQuestionResults(raw string) []DistillQuestionResult {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var results []DistillQuestionResult
	if err := common.UnmarshalJsonStr(raw, &results); err != nil || len(results) == 0 {
		return nil
	}
	return results
}

// parseDistillCategoryScores 解析 `{题目id: 分数}`。
func parseDistillCategoryScores(categories string) (map[string]float64, bool) {
	if strings.TrimSpace(categories) == "" {
		return nil, false
	}
	scores := map[string]float64{}
	if err := common.UnmarshalJsonStr(categories, &scores); err != nil {
		return nil, false
	}
	return scores, true
}

// EvaluateDistill 是请求路径上的入口：决定要不要判、判、然后记账。
//
// 返回命中信息（未命中返回 nil）。**观察模式下不写降级标记**，因此不会影响路由。
// 所有耗时的部分（判定、落库、告警）都在调用方的 goroutine 里跑，这里只做
// 廉价的判断。
func EvaluateDistill(userID int, requestID, modelName string, channelID int, body []byte) *model.DistillDetectHit {
	if userID <= 0 {
		return nil
	}
	// ⛔ 用户三态（豁免 / 跟随全局 / 强制启用）由**调用方**判：它要读用户缓存，
	// 而这里每判一次都要走一遍，放在调用方还能顺手省掉重复查询。
	// 唯一调用方 middleware.runDistillDetection 已经在白名单之后判过了。
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
	// 时间桶：同一用户在一个判定间隔内只有首条会走到判定。
	//
	// ⛔ 去重不影响上面两步（规格 §19）：每分钟请求量该累加照样累加，
	// 否则「反复发同一段蒸馏正文」的用户永远到不了激活阈值。
	if !distillTakeBucket(userID, s.JudgeIntervalSeconds) {
		return nil
	}

	doc := distillSample(distillDocument(body), s.MaxStateChars, s.HeadSampleChars)
	if strings.TrimSpace(doc) == "" {
		return nil
	}

	// ⛔ 去重只省 Jev 调用，不省命中计数（规格 §19）：
	//   - 窗口内上次**未命中** → 静默跳过；
	//   - 窗口内上次**命中** → 不调 Jev，复用当时的分数与逐题快照，照记一次确认命中。
	// 否则同一段正文重发多少次都只算一次，4 次确认永远凑不齐 —— 而蒸馏用户
	// 恰恰最常反复发同一段 prompt。
	prior, seen := distillDedupLookup(userID, doc)
	verdict, reused, skip := distillResolveVerdict(prior, seen, func() DistillVerdict {
		// ⛔ 只有走到这里才是**真实调用** Jev；去重命中的复用不会进闭包，
		// 因此调用流水天然不会记「没调 Jev」的行。
		return JudgeDistill(ctx, doc, JevCallMeta{
			UserID:    userID,
			RequestID: requestID,
			Model:     modelName,
			ChannelID: channelID,
			RPM:       rpm,
		})
	})
	if skip {
		return nil
	}
	if verdict.Skipped != "" {
		// 判定没跑成（超时 / 5xx / 响应不合协议）：一个字都不记，下一轮同正文
		// 还要能重判。这是 dedup 时序反了会造成的最严重的后果。
		return nil
	}
	if !reused {
		// 逐题快照（规格 §18）：题名与 hit 在这里定死，后面改名/改阈值都不影响
		// 这条历史记录的解释。快照同时进缓存，复用时原样搬走（§19）。
		verdict.QuestionResults = BuildDistillQuestionResults(verdict.Categories, s.EnabledDistillQuestions(), s.Threshold)
		distillDedupMark(userID, doc, s.DedupSeconds, verdict)
	}
	if !verdict.Hit {
		return nil
	}

	// 命中不等于确认：先记进滑动窗口，累计到 ConfirmHits 次才真正降级。
	confirmed := distillRecordConfirmedHit(userID, s.ConfirmWindowSeconds)
	threshold := s.ConfirmHits
	if threshold <= 0 {
		threshold = 1
	}

	hit := &model.DistillDetectHit{
		UserId:     userID,
		RequestId:  requestID,
		Model:      modelName,
		Score:      verdict.Score,
		BankId:     "train",
		Categories: verdict.Categories,
		// 复用前次判定的那几行要标明，运营才看得懂同一条正文为什么连续命中。
		DedupReused:     reused,
		QuestionResults: verdict.QuestionResults,
		DocChars:        verdict.DocChars,
		SampledAt:       time.Now(),
	}
	// ⛔ 只记录「这一条命中是否让确认数达到阈值」，不记录累计值 —— 累计是
	// 滑动窗口的中间态，落库只会变成一份难对账的流水。
	if !s.ObserveOnly && confirmed >= threshold {
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
	// dedup 缓存「(user, 正文哈希) → 上次判定结果」；payload 是判定快照 JSON，
	// 去重跳过时靠它复用分数与 hit，而不是只知道「判过」（规格 §19）。
	dedup map[string]distillDedupEntry
	// buckets 记录每个用户在哪个时间槽已经判过（Redis SETNX 的等价物）。
	buckets map[int]int64
	// confirms 是确认窗口内每次命中的时刻，滑动计数用。
	confirms map[int][]time.Time
}

var distillMemoryRPM = &distillMemoryTracker{
	rpm: make(map[int]struct {
		minute int64
		count  int
	}),
	demoteUntil: make(map[int]time.Time),
	dedup:       make(map[string]distillDedupEntry),
	buckets:     make(map[int]int64),
	confirms:    make(map[int][]time.Time),
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

// dedupAlive 报告这份正文在窗口内是否判定成功过（只读）。
// clearState 清掉进程内的运行态（规格 §20），返回清理条目数与解除降级人数。
//
// userID <= 0 表示清全部用户。
func (t *distillMemoryTracker) clearState(userID int) (cleared int, demoted int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if userID <= 0 {
		now := time.Now()
		// ⛔ 两个数分开算：
		//   cleared = 实际清掉的条目数，降级标记也要算进去（与「单用户」分支对称）；
		//   demoted = **仍在生效**的降级数 —— 内存里的 demoteUntil 过期不自动删，
		//   把过期条目算进去会让「解除了几个用户」系统性高估。
		cleared = len(t.rpm) + len(t.buckets) + len(t.dedup) + len(t.confirms) + len(t.demoteUntil)
		for _, until := range t.demoteUntil {
			if now.Before(until) {
				demoted++
			}
		}
		t.rpm = map[int]struct {
			minute int64
			count  int
		}{}
		t.demoteUntil = map[int]time.Time{}
		t.dedup = map[string]distillDedupEntry{}
		t.buckets = map[int]int64{}
		t.confirms = map[int][]time.Time{}
		return cleared, demoted
	}
	if _, ok := t.rpm[userID]; ok {
		delete(t.rpm, userID)
		cleared++
	}
	if until, ok := t.demoteUntil[userID]; ok && time.Now().Before(until) {
		demoted++
	}
	if _, ok := t.demoteUntil[userID]; ok {
		delete(t.demoteUntil, userID)
		cleared++
	}
	if _, ok := t.buckets[userID]; ok {
		delete(t.buckets, userID)
		cleared++
	}
	if _, ok := t.confirms[userID]; ok {
		delete(t.confirms, userID)
		cleared++
	}
	for key := range t.dedup {
		if distillKeyBelongsToUser(key, userID) {
			delete(t.dedup, key)
			cleared++
		}
	}
	return cleared, demoted
}

// distillDedupEntry 是内存版去重表的一条：过期时刻 + 上次判定快照。
type distillDedupEntry struct {
	until   time.Time
	payload string
}

// dedupValue 返回窗口内缓存的上次判定快照；过期视为不存在。
func (t *distillMemoryTracker) dedupValue(key string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.dedup[key]
	if !ok || !time.Now().Before(entry.until) {
		return "", false
	}
	return entry.payload, true
}

// dedupMark 记录一次判定成功。
//
// ⛔ 顺手做过期清理：这张表只在 Redis 长期不可用时长出来，键是
// (user, 正文哈希)，每个新正文一条，不清理就是无界增长。清理成本 O(n)，
// 但只在写入时做、且 n 是十分钟窗口内的正文数，可以接受。
func (t *distillMemoryTracker) dedupMark(key string, ttlSeconds int, payload string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if len(t.dedup) > 0 {
		for k, entry := range t.dedup {
			if !now.Before(entry.until) {
				delete(t.dedup, k)
			}
		}
	}
	t.dedup[key] = distillDedupEntry{until: now.Add(time.Duration(ttlSeconds) * time.Second), payload: payload}
}

// takeBucket 是 Redis SETNX 的进程内等价物：同一时间槽只有第一个调用返回 true。
func (t *distillMemoryTracker) takeBucket(userID int, slot int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cur, ok := t.buckets[userID]; ok && cur == slot {
		return false
	}
	t.buckets[userID] = slot
	return true
}

// recordConfirm 滑动计数：先剔除窗口外的命中，再记这一次，返回窗口内总数。
func (t *distillMemoryTracker) recordConfirm(userID int, now time.Time, windowSeconds int) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := now.Add(-time.Duration(windowSeconds) * time.Second)
	kept := t.confirms[userID][:0]
	for _, at := range t.confirms[userID] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	t.confirms[userID] = kept
	return len(kept)
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
