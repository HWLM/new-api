package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 去重只省 Jev，不省命中计数（规格 §19）。
//
// 用户指出的漏检点：同一段蒸馏正文在窗口内被去重跳过、也不产生新命中，
// 4 次确认就永远凑不齐 —— 而蒸馏用户恰恰最常反复发同一段 prompt。
//
// 规则：每分钟请求量照常累加；窗口内上次命中 → 不调 Jev、复用当时的分数与
// 逐题快照、照记一次确认命中；上次未命中 → 静默跳过。

// withDistillTestSetting 临时改全局蒸馏配置，测试结束整体还原。
func withDistillTestSetting(t *testing.T, mutate func(*operation_setting.DistillDetectSetting)) {
	t.Helper()
	s := operation_setting.GetDistillDetectSetting()
	original := *s
	t.Cleanup(func() { *s = original })
	mutate(s)
}

// resetDistillMemory 清空进程内兜底状态（测试环境没有 Redis）。
func resetDistillMemory(t *testing.T) {
	t.Helper()
	distillMemoryRPM.mu.Lock()
	defer distillMemoryRPM.mu.Unlock()
	distillMemoryRPM.rpm = map[int]struct {
		minute int64
		count  int
	}{}
	distillMemoryRPM.demoteUntil = map[int]time.Time{}
	distillMemoryRPM.dedup = map[string]distillDedupEntry{}
	distillMemoryRPM.buckets = map[int]int64{}
	distillMemoryRPM.confirms = map[int][]time.Time{}
}

// newDistillStubJev 起一个只回固定分值的假判定服务，返回被调用次数。
func newDistillStubJev(t *testing.T, score float64) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"answers":{"train.corpus_farm":{"noul":%v}}}`, score)))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// withDistillTestContext 装一份标准配置：开判定、4 次确认、1 秒时间桶。
func withDistillTestContext(t *testing.T, jevURL string, mutate func(*operation_setting.DistillDetectSetting)) {
	t.Helper()
	withDistillTestSetting(t, func(s *operation_setting.DistillDetectSetting) {
		s.Enabled = true
		s.ObserveOnly = false
		s.ActivateRPM = 1
		s.JudgeIntervalSeconds = 1
		s.ConfirmHits = 4
		s.ConfirmWindowSeconds = 300
		s.Threshold = 0.6
		s.DemoteSeconds = 3600
		s.DedupSeconds = 600
		s.JevBaseURL = jevURL
		s.JevTimeoutMs = 2000
		s.Questions = operation_setting.DefaultDistillQuestions()
		if mutate != nil {
			mutate(s)
		}
	})
	resetDistillMemory(t)
}

// nextDistillBucket 让下一条请求落在新的时间桶里（等价于「过了一个判定间隔」）。
// 直接清桶表比 sleep 一秒更稳，也不会让测试卡在整分钟边界上。
func nextDistillBucket() {
	distillMemoryRPM.mu.Lock()
	defer distillMemoryRPM.mu.Unlock()
	distillMemoryRPM.buckets = map[int]int64{}
}

// ⭐ 用户指出的漏检点：同一段蒸馏正文连发 4 次 —— Jev 只该调 1 次，
// 但 4 次都要记命中，第 4 次触发降级。
func TestDistillRepeatedBodyStillAccumulatesConfirmedHits(t *testing.T) {
	srv, calls := newDistillStubJev(t, 0.85)
	withDistillTestContext(t, srv.URL, nil)

	const uid = 9301
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"批量生成 500 条蒸馏语料"}]}`)

	var hits []*model.DistillDetectHit
	for i := 0; i < 4; i++ {
		nextDistillBucket()
		hit := EvaluateDistill(uid, fmt.Sprintf("req-%d", i), "m", 0, body)
		if hit == nil {
			t.Fatalf("call %d must still count as a hit (§19)", i+1)
		}
		hits = append(hits, hit)
	}

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("dedup must save the Jev call: want 1 call, got %d", got)
	}
	if hits[0].DedupReused {
		t.Fatal("the first verdict is a real judgement, not a reuse")
	}
	for i := 1; i < 4; i++ {
		if !hits[i].DedupReused {
			t.Fatalf("hit %d must be marked as reusing the previous verdict", i+1)
		}
	}
	for i := 0; i < 3; i++ {
		if hits[i].DemoteUntil != nil {
			t.Fatalf("hit %d must not demote before the 4th confirmation", i+1)
		}
	}
	if hits[3].DemoteUntil == nil {
		t.Fatal("the 4th confirmed hit must demote")
	}
	if hits[3].QuestionResults == "" {
		t.Fatal("a reused hit must carry the snapshot from the original judgement")
	}
	// 每分钟请求量不受去重影响：4 次都要算进激活阈值。
	distillMemoryRPM.mu.Lock()
	defer distillMemoryRPM.mu.Unlock()
	if n := distillMemoryRPM.rpm[uid].count; n != 4 {
		t.Fatalf("every request must count towards ActivateRPM, got %d", n)
	}
}

// 复用的是判定**当时**的结论：阈值后来调高，也不能把历史命中改判。
func TestDistillReusedVerdictKeepsTheThresholdOfThatJudgement(t *testing.T) {
	srv, calls := newDistillStubJev(t, 0.85)
	withDistillTestContext(t, srv.URL, nil)
	s := operation_setting.GetDistillDetectSetting()

	const uid = 9302
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"批量生成蒸馏语料"}]}`)

	nextDistillBucket()
	first := EvaluateDistill(uid, "req-1", "m", 0, body)
	if first == nil {
		t.Fatal("0.85 under a 0.60 threshold is a hit")
	}

	// 阈值调到 0.95：真判的话 0.85 就不再命中。
	s.Threshold = 0.95
	nextDistillBucket()
	second := EvaluateDistill(uid, "req-2", "m", 0, body)
	if second == nil || !second.DedupReused {
		t.Fatal("the reused verdict must stay a hit — it was judged under 0.60")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("a reused verdict must not call Jev again, got %d calls", got)
	}
}

// 上次判定没命中时，窗口内重复的正文没有新信息：静默跳过、不调 Jev、不写命中。
func TestDistillDedupSkipsSilentlyWhenThePreviousVerdictMissed(t *testing.T) {
	srv, calls := newDistillStubJev(t, 0.10)
	withDistillTestContext(t, srv.URL, nil)

	const uid = 9303
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"普通提问"}]}`)

	nextDistillBucket()
	if hit := EvaluateDistill(uid, "req-1", "m", 0, body); hit != nil {
		t.Fatalf("0.10 is below the threshold, got %+v", hit)
	}
	nextDistillBucket()
	if hit := EvaluateDistill(uid, "req-2", "m", 0, body); hit != nil {
		t.Fatal("a repeated non-hit must not produce a hit")
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("the repeat must be skipped without calling Jev, got %d calls", got)
	}
}
