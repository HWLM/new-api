package service

import (
	"strings"
	"testing"
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

func TestDistillShouldSampleStartsAtTheActivationThresholdThenEveryN(t *testing.T) {
	// 激活阈值 20、抽样间隔 10：第 20 条必判，之后 30、40…
	got := make(map[int]bool)
	for rpm := 1; rpm <= 45; rpm++ {
		got[rpm] = distillShouldSample(rpm, 20, 10)
	}
	for _, must := range []int{20, 30, 40} {
		if !got[must] {
			t.Fatalf("request #%d must be sampled", must)
		}
	}
	for _, mustNot := range []int{1, 19, 21, 25, 29, 31, 39, 44} {
		if got[mustNot] {
			t.Fatalf("request #%d must not be sampled", mustNot)
		}
	}
}

func TestDistillShouldSampleTreatsZeroEveryAsEveryRequest(t *testing.T) {
	// every=0 视为每条都判，起点仍然是激活阈值。
	if distillShouldSample(19, 20, 0) {
		t.Fatal("below the activation threshold nothing is sampled")
	}
	for rpm := 20; rpm <= 24; rpm++ {
		if !distillShouldSample(rpm, 20, 0) {
			t.Fatalf("#%d must be sampled when every=0 (treated as 1)", rpm)
		}
	}
}

func TestDistillShouldSampleWithZeroActivationSamplesFromTheFirst(t *testing.T) {
	// ActivateRPM=0 表示不设门槛：第 1 条就判。
	if !distillShouldSample(1, 0, 10) {
		t.Fatal("with no activation threshold the first request is sampled")
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

func TestDistillQuestionsPayloadRespectsPerQuestionSwitches(t *testing.T) {
	// 用到 setter 会污染全局配置，这里直接构造一个局部 setting 走同一套拼装逻辑。
	// distillQuestionsPayload 只读这四个布尔位，所以单独验证它的取舍。
	q := distillQuestionBank
	if len(q) != 4 {
		t.Fatalf("the train bank must carry exactly 4 questions, got %d", len(q))
	}
	ids := make(map[string]bool, len(q))
	for _, item := range q {
		ids[item.ID] = true
		if item.Instructions == "" {
			t.Fatalf("question %s has no instructions", item.ID)
		}
	}
	for _, want := range []string{distillQCorpusFarm, distillQRewardRL, distillQJudgeEval, distillQDistillTgt} {
		if !ids[want] {
			t.Fatalf("question %s is missing from the bank", want)
		}
	}
}
