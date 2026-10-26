package common

import (
	"sync"
	"time"
)

// RefusalWindowEntry 跟踪单个用户的滑窗与降级标记。
// 字段仅在 UserRefusalTracker 的 mu 持有期间访问。
type refusalWindowEntry struct {
	// ring 保存最近 N 条请求的拒答位（1=拒答，0=正常），FIFO 覆盖写入。
	// 下标 0 为最老条目；append 到末尾，容量超限时丢最前。
	ring []byte
	// demoteUntil 零值表示未处于降级态；> now 表示降级中。
	demoteUntil time.Time
	// touchedAt 最近一次读写时间，供清理 goroutine 判定是否可回收。
	touchedAt time.Time
}

// InMemoryRefusalTracker 在 Redis 不可用时跟踪每个用户的拒答滑窗与降级状态。
// 进程重启会丢失全部状态，等同于「全体用户重置」。
type InMemoryRefusalTracker struct {
	mu      sync.Mutex
	entries map[int]*refusalWindowEntry
	// cleanupMinInterval 控制清理 goroutine 的最小睡眠周期。
	cleanupMinInterval time.Duration
}

var defaultRefusalTracker = newInMemoryRefusalTracker()

func newInMemoryRefusalTracker() *InMemoryRefusalTracker {
	t := &InMemoryRefusalTracker{
		entries:            make(map[int]*refusalWindowEntry),
		cleanupMinInterval: 10 * time.Minute,
	}
	go t.janitorLoop()
	return t
}

// UserRefusalTrackerInstance 返回进程级单例。
func UserRefusalTrackerInstance() *InMemoryRefusalTracker {
	return defaultRefusalTracker
}

// Record 向 userId 的滑窗推入一条样本并裁剪到 windowSize。
// 返回写入后的 (总样本数, 拒答数)。windowSize <= 0 时按 1 处理。
func (t *InMemoryRefusalTracker) Record(userId int, refused bool, windowSize int) (total, refusedCount int) {
	if windowSize <= 0 {
		windowSize = 1
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entries[userId]
	if e == nil {
		e = &refusalWindowEntry{ring: make([]byte, 0, windowSize)}
		t.entries[userId] = e
	}
	bit := byte(0)
	if refused {
		bit = 1
	}
	e.ring = append(e.ring, bit)
	if len(e.ring) > windowSize {
		// 多出部分从头丢弃，保持 FIFO 语义。
		e.ring = e.ring[len(e.ring)-windowSize:]
	}
	e.touchedAt = time.Now()
	total = len(e.ring)
	for _, b := range e.ring {
		if b == 1 {
			refusedCount++
		}
	}
	return total, refusedCount
}

// Snapshot 读取 userId 当前滑窗的 (总样本数, 拒答数)。不改状态。
func (t *InMemoryRefusalTracker) Snapshot(userId int) (total, refusedCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entries[userId]
	if e == nil {
		return 0, 0
	}
	total = len(e.ring)
	for _, b := range e.ring {
		if b == 1 {
			refusedCount++
		}
	}
	return total, refusedCount
}

// MarkDemoted 把 userId 标记为降级到 until 时间点。until 必须是未来时刻；
// 更早的 until 不会缩短已有降级期（防止并发写抖动）。
func (t *InMemoryRefusalTracker) MarkDemoted(userId int, until time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entries[userId]
	if e == nil {
		e = &refusalWindowEntry{ring: make([]byte, 0)}
		t.entries[userId] = e
	}
	if until.After(e.demoteUntil) {
		e.demoteUntil = until
	}
	e.touchedAt = time.Now()
}

// IsDemoted 判断 userId 当前是否处于降级态。
func (t *InMemoryRefusalTracker) IsDemoted(userId int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entries[userId]
	if e == nil {
		return false
	}
	return time.Now().Before(e.demoteUntil)
}

// Reset 用于测试：清空指定用户状态。
func (t *InMemoryRefusalTracker) Reset(userId int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, userId)
}

// janitorLoop 周期清理已不再活跃且未处于降级的用户条目，避免 map 无界增长。
// idleTTL 以滑窗样本的保留窗口为参考，这里固定取「两倍清理周期」。
func (t *InMemoryRefusalTracker) janitorLoop() {
	ticker := time.NewTicker(t.cleanupMinInterval)
	defer ticker.Stop()
	for range ticker.C {
		t.sweepExpired()
	}
}

func (t *InMemoryRefusalTracker) sweepExpired() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	idleTTL := 2 * t.cleanupMinInterval
	for uid, e := range t.entries {
		if now.Before(e.demoteUntil) {
			continue
		}
		if now.Sub(e.touchedAt) >= idleTTL {
			delete(t.entries, uid)
		}
	}
}
