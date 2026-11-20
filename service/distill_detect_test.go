package service

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 蒸馏检测的单元测试（docs/distill-detection-plan.md）。
//
// 覆盖三块最容易错、错了又不容易察觉的逻辑：
//   1. 正文采样 —— 头尾采样必须真的保住头部证据，否则批处理语料会被判成普通对话；
//   2. 答案解析 —— 协议 key 与分数提取，取最高分而不是平均分；
//   3. 抽样规则 —— 激活门槛与抽样间隔的边界。

func TestDistillDocumentKeepsHeadEvidenceForBatchJSON(t *testing.T) {
	// 典型的「批量语料生产」请求：system 说明用途，user 里塞一份 JSON 任务表。
	body := []byte(`{
		"system": "You are a data pipeline. For each row, rewrite the answer to be more preferred or less preferred.",
		"messages": [
			{"role": "user", "content": "Here is the batch:\n[{\"prompt\":\"a\",\"answer\":\"b\",\"label\":\"positive\"}]"},
			{"role": "assistant", "content": "ok"}
		]
	}`)

	doc := distillDocument(body)
	if !strings.Contains(doc, "[system prompt 1]") {
		t.Fatalf("system prompt must be labelled, got: %s", doc)
	}
	if !strings.Contains(doc, "rewrite the answer") {
		t.Fatalf("system prompt text must survive: %s", doc)
	}
	if !strings.Contains(doc, "[user message 1]") {
		t.Fatalf("user turn must be labelled: %s", doc)
	}
	if !strings.Contains(doc, `"label":"positive"`) {
		t.Fatalf("the corpus evidence in the user turn must survive: %s", doc)
	}
	// assistant 的输出不是用途证据，必须丢掉。
	if strings.Contains(doc, "assistant") || strings.Contains(doc, "\nok") {
		t.Fatalf("assistant turns must be dropped: %s", doc)
	}
	if !strings.Contains(doc, "[current ask]") {
		t.Fatalf("the last user turn must be repeated as the current ask: %s", doc)
	}
}

func TestDistillDocumentSupportsAnthropicSystemBlocks(t *testing.T) {
	body := []byte(`{
		"system": [{"type":"text","text":"first block"},{"type":"text","text":"second block"}],
		"messages": [{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image","source":{}}]}]
	}`)
	doc := distillDocument(body)
	if !strings.Contains(doc, "first block") || !strings.Contains(doc, "second block") {
		t.Fatalf("both system blocks must survive: %s", doc)
	}
	if !strings.Contains(doc, "hello") {
		t.Fatalf("the text block must survive: %s", doc)
	}
}

func TestDistillDocumentDropsDuplicateUserTurns(t *testing.T) {
	// 重试 / resume 时客户会重贴同样的 context，去重后送上游的量减半。
	body := []byte(`{
		"messages": [
			{"role":"user","content":"same text"},
			{"role":"user","content":"same text"}
		]
	}`)
	doc := distillDocument(body)
	if n := strings.Count(doc, "same text"); n != 2 {
		// 一次在 [user message N]，一次在 [current ask]。
		t.Fatalf("a duplicated turn must be sent once (plus the current ask), got %d occurrences: %s", n, doc)
	}
}

func TestDistillSampleKeepsBothEnds(t *testing.T) {
	head := strings.Repeat("H", 100)
	tail := strings.Repeat("T", 100)
	text := head + strings.Repeat("M", 5000) + tail

	out := distillSample(text, 200, 60)
	if !strings.HasPrefix(out, "HHHH") {
		t.Fatalf("head must be kept: %.80s", out)
	}
	if !strings.HasSuffix(out, "TTTT") {
		t.Fatalf("tail must be kept: %.80s", out)
	}
	if !strings.Contains(out, "[...truncated...]") {
		t.Fatalf("the truncation marker must be present: %.200s", out)
	}
}

func TestDistillSampleWithZeroHeadTakesTheTailOnly(t *testing.T) {
	text := strings.Repeat("A", 100) + strings.Repeat("B", 100)
	out := distillSample(text, 50, 0)
	if out != strings.Repeat("B", 50) {
		t.Fatalf("head=0 must keep the tail only, got %q", out)
	}
}

func TestDistillSampleIsIdempotentForTheSameInput(t *testing.T) {
	// 采样结果决定去重哈希；掺了时间或随机量会让同一份正文重复打分、白花钱。
	text := strings.Repeat("x", 1000) + "证据" + strings.Repeat("y", 1000)
	if a, b := distillSample(text, 300, 100), distillSample(text, 300, 100); a != b {
		t.Fatalf("sampling must be deterministic")
	}
}

func TestParseDistillAnswersPicksTheMaximum(t *testing.T) {
	payload := []byte(`{"answers":{
		"train.corpus_farm":{"noul":0.91},
		"train.reward_rl":{"noul":0.12},
		"train.judge_eval":{"noul":0.5},
		"train.distill_target":{"noul":0.05}
	}}`)
	best, categories, ok := ParseDistillAnswers(payload)
	if !ok {
		t.Fatal("a well-formed answers object must parse")
	}
	if best != 0.91 {
		t.Fatalf("the highest score must win, got %v", best)
	}
	for _, want := range []string{"corpus_farm", "reward_rl", "judge_eval", "distill_target"} {
		if !strings.Contains(categories, want) {
			t.Fatalf("per-question scores must be recorded, missing %s: %s", want, categories)
		}
	}
}

func TestParseDistillAnswersAcceptsBareQuestionIDs(t *testing.T) {
	payload := []byte(`{"answers":{"corpus_farm":{"noul":0.77}}}`)
	best, _, ok := ParseDistillAnswers(payload)
	if !ok || best != 0.77 {
		t.Fatalf("a bare question id must also parse, got ok=%v best=%v", ok, best)
	}
}

func TestParseDistillAnswersRejectsPayloadsWithoutScores(t *testing.T) {
	for _, payload := range []string{
		`{}`,
		`{"answers":{}}`,
		`{"answers":{"train.corpus_farm":{}}}`,
		`not json`,
	} {
		if _, _, ok := ParseDistillAnswers([]byte(payload)); ok {
			t.Fatalf("%s must be rejected", payload)
		}
	}
}

// ── 时间桶抽样 ────────────────────────────────────────────────────────

// ⭐ 抽样单位是时间桶：同一用户在一个判定间隔内只有首条被判定。
// 这是在替掉「每 N 条请求抽 1」之后的核心行为，测的是分支本身。
func TestDistillBucketOnlyTheFirstRequestInASlotIsJudged(t *testing.T) {
	const uid = 9001
	distillMemoryRPM.buckets = map[int]int64{}

	// 模拟同一个 30 秒槽里的 5 条请求：只有第一条能拿到名额。
	taken := 0
	for i := 0; i < 5; i++ {
		if distillMemoryRPM.takeBucket(uid, 1000) {
			taken++
		}
	}
	if taken != 1 {
		t.Fatalf("exactly one request per slot may be judged, got %d", taken)
	}
	// 下一个槽重新开始。
	if !distillMemoryRPM.takeBucket(uid, 1001) {
		t.Fatal("a new slot must allow a new judgement")
	}
}

func TestDistillBucketIsPerUser(t *testing.T) {
	distillMemoryRPM.buckets = map[int]int64{}
	if !distillMemoryRPM.takeBucket(100, 500) {
		t.Fatal("user A takes its own slot")
	}
	if !distillMemoryRPM.takeBucket(101, 500) {
		t.Fatal("user B must not be blocked by user A")
	}
}

// ── 命中确认（滑动计数）──────────────────────────────────────────────

// ⭐ 5 分钟内累计 4 次命中才确认：前 3 次只写 hits，第 4 次才降级。
func TestDistillConfirmRequiresFourHitsInTheWindow(t *testing.T) {
	const uid = 9002
	distillMemoryRPM.confirms = map[int][]time.Time{}
	now := time.Now()

	for i := 1; i <= 4; i++ {
		n := distillMemoryRPM.recordConfirm(uid, now.Add(time.Duration(i)*30*time.Second), 300)
		if n != i {
			t.Fatalf("hit #%d must count to %d, got %d", i, i, n)
		}
		if i < 4 && n >= 4 {
			t.Fatalf("must not confirm before the 4th hit")
		}
	}
}

// ⛔ 滑动计数：超出窗口的命中会过期，不会被永久累加。
func TestDistillConfirmSlidesAndExpiresOldHits(t *testing.T) {
	const uid = 9003
	distillMemoryRPM.confirms = map[int][]time.Time{}
	base := time.Now()

	// 三次命中，然后就停了。
	for i := 0; i < 3; i++ {
		distillMemoryRPM.recordConfirm(uid, base.Add(time.Duration(i)*10*time.Second), 300)
	}
	// 过了窗口再来一次：前面三次都已过期，计数归 1 而不是 4。
	n := distillMemoryRPM.recordConfirm(uid, base.Add(600*time.Second), 300)
	if n != 1 {
		t.Fatalf("hits older than the window must expire, expected 1 got %d", n)
	}
}

// 窗口内的命中不会因为「跨了一个桶」就被清零 —— 这正是滑动计数相对固定窗口
// 的意义：单次命中率掉下来时不会漏掉真阳性。
func TestDistillConfirmKeepsHitsAcrossSlots(t *testing.T) {
	const uid = 9004
	distillMemoryRPM.confirms = map[int][]time.Time{}
	base := time.Now()

	// 每 30 秒一次，跨了多个判定槽但都在 5 分钟窗口内。
	n := 0
	for i := 0; i < 4; i++ {
		n = distillMemoryRPM.recordConfirm(uid, base.Add(time.Duration(i)*30*time.Second), 300)
	}
	if n != 4 {
		t.Fatalf("hits within the window must accumulate across slots, got %d", n)
	}
}

func TestDistillDecisionURLAcceptsThreeBaseShapes(t *testing.T) {
	cases := map[string]string{
		"https://api.example.com":               "https://api.example.com/v1/systemone",
		"https://api.example.com/v1":            "https://api.example.com/v1/systemone",
		"https://api.example.com/v1/systemone":  "https://api.example.com/v1/systemone",
		"https://api.example.com/v1/systemone/": "https://api.example.com/v1/systemone",
		"https://api.example.com/decide":        "https://api.example.com/decide",
	}
	for base, want := range cases {
		if got := distillDecisionURL(base); got != want {
			t.Fatalf("base %q: got %q want %q", base, got, want)
		}
	}
}

// ── 可编辑题库（规格 §14）──────────────────────────────────────────────

// ⭐ 题库是数据：只有启用的题进 payload，题面用的是配置里那份（管理页改过
// 的就是这份），wire key 仍是 `train.<id>` 与 hw_gateway 对齐。
func TestDistillQuestionsPayloadUsesEnabledConfiguredQuestions(t *testing.T) {
	s := &operation_setting.DistillDetectSetting{
		Questions: []operation_setting.DistillQuestion{
			{ID: "corpus_farm", Name: "A", Prompt: "prompt-a", Enabled: true},
			{ID: "reward_rl", Name: "B", Prompt: "prompt-b", Enabled: false},
			{ID: "my_custom", Name: "C", Prompt: "prompt-c", Enabled: true},
		},
	}
	payload := distillQuestionsPayload(s)

	if len(payload) != 2 {
		t.Fatalf("only enabled questions may be sent, got %d", len(payload))
	}
	for _, want := range []string{"train.corpus_farm", "train.my_custom"} {
		entry, ok := payload[want].(map[string]any)
		if !ok {
			t.Fatalf("missing wire question %s", want)
		}
		if entry["type"] != "noul" {
			t.Fatalf("%s must be a noul question", want)
		}
	}
	if _, ok := payload["train.reward_rl"]; ok {
		t.Fatal("a disabled question must not be sent")
	}
	// 题面必须取配置里那份：管理页改过题面后，送出去的就该是改后的文字。
	if got := payload["train.corpus_farm"].(map[string]any)["instructions"]; got != "prompt-a" {
		t.Fatalf("the configured prompt must be used verbatim, got %v", got)
	}
}

// ⛔ 上游多回了没问过的题时忽略 —— 题库改小之后，旧答案不能把分数抬上去。
func TestParseDistillAnswersIgnoresQuestionsNotAsked(t *testing.T) {
	asked := []operation_setting.DistillQuestion{
		{ID: "corpus_farm", Name: "A", Prompt: "p", Enabled: true},
	}
	payload := []byte(`{"answers":{
		"train.corpus_farm":{"noul":0.2},
		"train.removed_question":{"noul":0.99}
	}}`)
	best, categories, ok := ParseDistillAnswersFor(payload, asked)
	if !ok {
		t.Fatal("a well-formed answers object must parse")
	}
	if best != 0.2 {
		t.Fatalf("an unasked question must not set the score, got %v", best)
	}
	if strings.Contains(categories, "removed_question") {
		t.Fatalf("an unasked question must not appear in categories: %s", categories)
	}
}

// 默认题库必须是 hw_gateway 那四题，id 齐全且都有题面 —— 题面被改写或压缩
// 会让判定口径变化，已有观察期数据失去可比性。
func TestDefaultDistillQuestionsMirrorHwGatewayTrainBank(t *testing.T) {
	qs := operation_setting.DefaultDistillQuestions()
	if len(qs) != 4 {
		t.Fatalf("the builtin train bank must carry exactly 4 questions, got %d", len(qs))
	}
	ids := make(map[string]bool, len(qs))
	for _, q := range qs {
		ids[q.ID] = true
		if q.Prompt == "" || q.Name == "" {
			t.Fatalf("question %s has no name/prompt", q.ID)
		}
		if !q.Enabled {
			t.Fatalf("question %s must default to enabled", q.ID)
		}
	}
	for _, want := range []string{distillQCorpusFarm, distillQRewardRL, distillQJudgeEval, distillQDistillTgt} {
		if !ids[want] {
			t.Fatalf("question %s is missing from the bank", want)
		}
	}
	if err := operation_setting.ValidateDistillQuestions(qs); err != nil {
		t.Fatalf("the default bank must be valid: %v", err)
	}
}

// ── dedup 时序（Reviewer 首轮审查的必修项）──────────────────────────────

// ⭐ 这是本轮修掉的真实缺陷：早期版本在判分**之前**就用 SETNX 写「已判」键，
// 于是 Jev 超时 / 5xx 一次，同一份正文在 DedupSeconds（默认 600s）内再也不会
// 被重判 —— 故障恢复后整整十分钟的召回是哑的。
//
// 现在的分工：distillDedupSeen 只读，distillDedupMark 才写，且只在
// JudgeDistill 返回了真实分值之后调用。
func TestDistillDedupIsOnlyWrittenAfterASuccessfulVerdict(t *testing.T) {
	const uid = 9101
	distillMemoryRPM.dedup = map[string]time.Time{}
	doc := "batch corpus payload #1"

	if distillDedupSeen(uid, doc) {
		t.Fatal("a body never judged must not be reported as already judged")
	}
	// 判定失败：不写键。再问一次必须仍然是「没判过」。
	if distillDedupSeen(uid, doc) {
		t.Fatal("a failed verdict must not poison the dedup window")
	}

	distillDedupMark(uid, doc, 600)
	if !distillDedupSeen(uid, doc) {
		t.Fatal("after a successful verdict the body must be deduplicated")
	}
}

// dedup 按用户隔离：两个用户发同样的正文，各自都要判。
func TestDistillDedupIsPerUser(t *testing.T) {
	distillMemoryRPM.dedup = map[string]time.Time{}
	doc := "identical corpus payload"
	distillDedupMark(9201, doc, 600)
	if distillDedupSeen(9202, doc) {
		t.Fatal("dedup must not leak across users")
	}
}

// dedupMark 顺手清理过期条目：Redis 长期不可用时长出来的表是无界的，
// 键是 (user, 正文哈希)，每个新正文一条。
func TestDistillMemoryDedupEvictsExpiredEntries(t *testing.T) {
	distillMemoryRPM.dedup = map[string]time.Time{}
	now := time.Now()
	// 塞两条已过期的陈旧记录。
	distillMemoryRPM.dedup["stale-a"] = now.Add(-time.Minute)
	distillMemoryRPM.dedup["stale-b"] = now.Add(-time.Hour)

	distillMemoryRPM.dedupMark("fresh", 600)

	if _, ok := distillMemoryRPM.dedup["stale-a"]; ok {
		t.Fatal("expired dedup entries must be evicted on write")
	}
	if _, ok := distillMemoryRPM.dedup["stale-b"]; ok {
		t.Fatal("expired dedup entries must be evicted on write")
	}
	if _, ok := distillMemoryRPM.dedup["fresh"]; !ok {
		t.Fatal("the entry being written must survive the sweep")
	}
}

// ── 告警节流 ────────────────────────────────────────────────────────

// ⭐ 观察模式下没有「一轮降级只判一次」的抑制：30 秒桶 ≈ 2 判/分/人，
// 命中率 0.9 时不节流就是 ~1.8 条告警/分。默认 600 秒窗口内每人最多一条。
func TestDistillAlertThrottleAllowsOnePerWindow(t *testing.T) {
	distillAlertThrottleMu.Lock()
	distillAlertThrottle = map[int]time.Time{}
	distillAlertThrottleMu.Unlock()

	const uid = 9301
	if !distillAlertTakeThrottle(uid, 600) {
		t.Fatal("the first hit must be allowed through")
	}
	for i := 0; i < 5; i++ {
		if distillAlertTakeThrottle(uid, 600) {
			t.Fatal("subsequent hits inside the throttle window must be suppressed")
		}
	}
	// 另一个用户不受影响。
	if !distillAlertTakeThrottle(9302, 600) {
		t.Fatal("throttling must be per user")
	}
}

// 节流窗口过期后可以再次告警：一次降级生命周期结束后还要能喊人。
func TestDistillAlertThrottleReopensAfterTheWindow(t *testing.T) {
	distillAlertThrottleMu.Lock()
	distillAlertThrottle = map[int]time.Time{9401: time.Now().Add(-time.Second)}
	distillAlertThrottleMu.Unlock()

	if !distillAlertTakeThrottle(9401, 600) {
		t.Fatal("an expired throttle window must let the next alert through")
	}
}

// ── 逐题快照（规格 §18）────────────────────────────────────────────────

// ⭐ 快照就是「当时看到的东西」：题名来自写入时的题库，hit 按写入时的阈值算。
// 题目以后改名、阈值以后调整，历史记录都不能被改判。
func TestBuildDistillQuestionResultsSnapshotsNamesAndVerdicts(t *testing.T) {
	bank := []operation_setting.DistillQuestion{
		{ID: "corpus_farm", Name: "批量语料生产", Prompt: "p", Enabled: true},
		{ID: "reward_rl", Name: "RM / RL 偏好数据", Prompt: "p", Enabled: true},
		{ID: "judge_eval", Name: "LLM-as-judge 评测", Prompt: "p", Enabled: true},
	}
	raw := BuildDistillQuestionResults(`{"judge_eval":0.62,"corpus_farm":0.85}`, bank, 0.6)
	results := ParseDistillQuestionResults(raw)
	if len(results) != 2 {
		t.Fatalf("want 2 scored questions, got %+v", results)
	}
	// 顺序跟题库走（运营看惯的顺序），不跟 JSON 里的键序走。
	if results[0].ID != "corpus_farm" || results[1].ID != "judge_eval" {
		t.Fatalf("results must follow the bank order, got %+v", results)
	}
	if results[0].Name != "批量语料生产" || results[0].Score != 0.85 || !results[0].Hit {
		t.Fatalf("corpus_farm snapshot is wrong: %+v", results[0])
	}
	if results[1].Score != 0.62 || !results[1].Hit {
		t.Fatalf("judge_eval snapshot is wrong: %+v", results[1])
	}
}

// hit 是写入时按当时阈值算好的，不是展示时再算一遍。
func TestBuildDistillQuestionResultsFreezesTheVerdictAtTheThresholdUsed(t *testing.T) {
	bank := []operation_setting.DistillQuestion{{ID: "corpus_farm", Name: "批量语料生产", Prompt: "p", Enabled: true}}
	low := ParseDistillQuestionResults(BuildDistillQuestionResults(`{"corpus_farm":0.55}`, bank, 0.6))
	if len(low) != 1 || low[0].Hit {
		t.Fatalf("0.55 under a 0.60 threshold must not be a hit: %+v", low)
	}
	high := ParseDistillQuestionResults(BuildDistillQuestionResults(`{"corpus_farm":0.55}`, bank, 0.5))
	if len(high) != 1 || !high[0].Hit {
		t.Fatalf("the same score under a 0.50 threshold is a hit: %+v", high)
	}
}

// 题目被删掉之后，历史记录里那一题仍然要能显示：名字退回 id，排在最后。
func TestBuildDistillQuestionResultsKeepsDeletedQuestionsWithTheirID(t *testing.T) {
	bank := []operation_setting.DistillQuestion{{ID: "corpus_farm", Name: "批量语料生产", Prompt: "p", Enabled: true}}
	results := ParseDistillQuestionResults(BuildDistillQuestionResults(`{"gone_q":0.7,"corpus_farm":0.8}`, bank, 0.6))
	if len(results) != 2 {
		t.Fatalf("a deleted question must still show up, got %+v", results)
	}
	if results[0].ID != "corpus_farm" {
		t.Fatalf("known questions come first, got %+v", results)
	}
	if results[1].ID != "gone_q" || results[1].Name != "gone_q" || !results[1].Hit {
		t.Fatalf("a deleted question keeps its id as the name: %+v", results[1])
	}
}

// 没有逐题分数（上游没回、或判定失败）时快照为空，不造一条假记录。
func TestBuildDistillQuestionResultsIsEmptyWithoutScores(t *testing.T) {
	bank := operation_setting.DefaultDistillQuestions()
	for _, cats := range []string{"", "  ", "not json", "{}"} {
		if raw := BuildDistillQuestionResults(cats, bank, 0.6); raw != "" {
			t.Fatalf("categories %q must produce an empty snapshot, got %s", cats, raw)
		}
	}
	if results := ParseDistillQuestionResults("oops"); results != nil {
		t.Fatalf("a malformed snapshot must parse to nil, got %+v", results)
	}
	if results := ParseDistillQuestionResults(""); results != nil {
		t.Fatalf("an empty snapshot must parse to nil, got %+v", results)
	}
}
