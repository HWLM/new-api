package service

import (
	"context"
	"testing"
	"time"
)

// 一键清空运行态（规格 §20）。
//
// ⛔ 这里的用例只覆盖内存那份：测试环境没有 Redis，但线上真正在跑的也是这份
// 兜底 —— Redis 没配 / 连不上时整条链路都在这儿，只清 Redis 等于点了没点。

// seedDistillRuntime 给一个用户塞满六类运行态。
func seedDistillRuntime(uid int, doc string) {
	distillMemoryRPM.bump(uid, time.Now().Unix()/60)
	distillMemoryRPM.buckets[uid] = time.Now().Unix()
	distillMemoryRPM.confirms[uid] = []time.Time{time.Now()}
	distillMemoryRPM.demoteUntil[uid] = time.Now().Add(time.Hour)
	distillDedupMark(uid, doc, 600, DistillVerdict{Hit: true, Score: 0.9})
	if !distillAlertTakeThrottle(uid, 600) {
		panic("seed: throttle must be free")
	}
}

func TestClearDistillRuntimeStateOnlyTouchesTheGivenUser(t *testing.T) {
	resetDistillMemory(t)
	distillAlertClearThrottle(0)

	seedDistillRuntime(9401, "body-a")
	seedDistillRuntime(9402, "body-b")

	result := ClearDistillRuntimeState(context.Background(), 9401)

	if result.UpgradedUsers != 1 {
		t.Fatalf("exactly one demoted user should be released, got %d", result.UpgradedUsers)
	}
	if result.Cleared <= 0 {
		t.Fatal("cleared must report what actually happened")
	}

	distillMemoryRPM.mu.Lock()
	defer distillMemoryRPM.mu.Unlock()
	if _, ok := distillMemoryRPM.rpm[9401]; ok {
		t.Fatal("user 9401's RPM counter must be cleared")
	}
	if _, ok := distillMemoryRPM.buckets[9401]; ok {
		t.Fatal("user 9401's time bucket must be cleared")
	}
	if _, ok := distillMemoryRPM.confirms[9401]; ok {
		t.Fatal("user 9401's confirmation window must be cleared")
	}
	if _, ok := distillMemoryRPM.demoteUntil[9401]; ok {
		t.Fatal("user 9401's demotion must be cleared")
	}
	for key := range distillMemoryRPM.dedup {
		if distillKeyBelongsToUser(key, 9401) {
			t.Fatalf("user 9401's dedup key must be cleared: %s", key)
		}
	}
	// ⛔ 另一个用户的运行态一个字都不能动。
	if _, ok := distillMemoryRPM.rpm[9402]; !ok {
		t.Fatal("another user's RPM counter must survive")
	}
	if _, ok := distillMemoryRPM.demoteUntil[9402]; !ok {
		t.Fatal("another user's demotion must survive")
	}
	if _, ok := distillMemoryRPM.dedup[distillDedupKey(9402, "body-b")]; !ok {
		t.Fatal("another user's dedup window must survive")
	}
	distillAlertThrottleMu.Lock()
	defer distillAlertThrottleMu.Unlock()
	if _, ok := distillAlertThrottle[9401]; ok {
		t.Fatal("user 9401's alert throttle must be cleared")
	}
	if _, ok := distillAlertThrottle[9402]; !ok {
		t.Fatal("another user's alert throttle must survive")
	}
}

func TestClearDistillRuntimeStateAllUsersWipesEverything(t *testing.T) {
	resetDistillMemory(t)
	distillAlertClearThrottle(0)
	seedDistillRuntime(9411, "body-a")
	seedDistillRuntime(9412, "body-b")

	result := ClearDistillRuntimeState(context.Background(), 0)

	if result.UpgradedUsers != 2 {
		t.Fatalf("both demoted users should be released, got %d", result.UpgradedUsers)
	}
	distillMemoryRPM.mu.Lock()
	defer distillMemoryRPM.mu.Unlock()
	if len(distillMemoryRPM.rpm) != 0 || len(distillMemoryRPM.buckets) != 0 ||
		len(distillMemoryRPM.confirms) != 0 || len(distillMemoryRPM.demoteUntil) != 0 ||
		len(distillMemoryRPM.dedup) != 0 {
		t.Fatalf("a full clear must empty every structure: rpm=%d bucket=%d confirm=%d demote=%d dedup=%d",
			len(distillMemoryRPM.rpm), len(distillMemoryRPM.buckets),
			len(distillMemoryRPM.confirms), len(distillMemoryRPM.demoteUntil), len(distillMemoryRPM.dedup))
	}
	distillAlertThrottleMu.Lock()
	defer distillAlertThrottleMu.Unlock()
	if len(distillAlertThrottle) != 0 {
		t.Fatalf("a full clear must empty the alert throttle, got %d entries", len(distillAlertThrottle))
	}
}

// ⛔ 前缀匹配的经典坑：`distill:alert:{user:12}` 是 `distill:alert:{user:123}`
// 的前缀，照抄前缀匹配会把别人的键一起删掉。
func TestDistillKeyBelongsToUserDoesNotOverMatch(t *testing.T) {
	cases := []struct {
		key   string
		user  int
		match bool
	}{
		{"distill:rpm:12:28900000", 12, true},
		{"distill:rpm:123:28900000", 12, false},
		{"distill:bucket:12:9633", 12, true},
		{"distill:confirm:{user:12}", 12, true},
		{"distill:confirm:{user:123}", 12, false},
		{"distill:demote:{user:12}", 12, true},
		{"distill:alert:{user:12}", 12, true},
		{"distill:alert:{user:123}", 12, false},
		{"distill:dedup:{user:12}:abcdef", 12, true},
		{"distill:dedup:{user:123}:abcdef", 12, false},
	}
	for _, c := range cases {
		if got := distillKeyBelongsToUser(c.key, c.user); got != c.match {
			t.Fatalf("key %q for user %d: want %v, got %v", c.key, c.user, c.match, got)
		}
	}
}
