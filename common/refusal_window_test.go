package common

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryRefusalTracker_WindowLRU(t *testing.T) {
	tr := newInMemoryRefusalTracker()
	userID := 1001
	windowSize := 5
	// 推 windowSize+3 条，1-5 条全是拒答，之后 3 条非拒答 → 窗口里应保留的拒答数为 2。
	for i := 0; i < 5; i++ {
		tr.Record(userID, true, windowSize)
	}
	for i := 0; i < 3; i++ {
		tr.Record(userID, false, windowSize)
	}
	total, refused := tr.Snapshot(userID)
	assert.Equal(t, windowSize, total, "window should cap at windowSize")
	assert.Equal(t, 2, refused, "oldest entries should be evicted first")
}

func TestInMemoryRefusalTracker_RatioMath(t *testing.T) {
	tr := newInMemoryRefusalTracker()
	userID := 1002
	for i := 0; i < 7; i++ {
		tr.Record(userID, true, 10)
	}
	for i := 0; i < 3; i++ {
		tr.Record(userID, false, 10)
	}
	total, refused := tr.Snapshot(userID)
	assert.Equal(t, 10, total)
	assert.Equal(t, 7, refused)
}

func TestInMemoryRefusalTracker_DemoteUntilExpires(t *testing.T) {
	tr := newInMemoryRefusalTracker()
	userID := 1003
	until := time.Now().Add(20 * time.Millisecond)
	tr.MarkDemoted(userID, until)
	assert.True(t, tr.IsDemoted(userID), "should be demoted right after MarkDemoted")
	time.Sleep(40 * time.Millisecond)
	assert.False(t, tr.IsDemoted(userID), "should expire after until")
}

func TestInMemoryRefusalTracker_MarkDemotedMonotonic(t *testing.T) {
	tr := newInMemoryRefusalTracker()
	userID := 1004
	far := time.Now().Add(10 * time.Second)
	near := time.Now().Add(1 * time.Second)
	tr.MarkDemoted(userID, far)
	tr.MarkDemoted(userID, near) // should not shrink
	tr.mu.Lock()
	e := tr.entries[userID]
	require.NotNil(t, e)
	assert.True(t, e.demoteUntil.Equal(far), "later MarkDemoted with earlier until must not shrink")
	tr.mu.Unlock()
}

func TestInMemoryRefusalTracker_ConcurrentRecord(t *testing.T) {
	tr := newInMemoryRefusalTracker()
	userID := 1005
	windowSize := 50
	var wg sync.WaitGroup
	// 100 并发写入，一半拒答一半正常，最终窗口大小必须恰为 windowSize。
	for i := 0; i < 100; i++ {
		wg.Add(1)
		refused := i%2 == 0
		go func() {
			defer wg.Done()
			tr.Record(userID, refused, windowSize)
		}()
	}
	wg.Wait()
	total, _ := tr.Snapshot(userID)
	assert.Equal(t, windowSize, total)
}

func TestInMemoryRefusalTracker_Reset(t *testing.T) {
	tr := newInMemoryRefusalTracker()
	userID := 1006
	tr.Record(userID, true, 5)
	tr.MarkDemoted(userID, time.Now().Add(10*time.Second))
	tr.Reset(userID)
	total, refused := tr.Snapshot(userID)
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, refused)
	assert.False(t, tr.IsDemoted(userID))
}

func TestInMemoryRefusalTracker_SweepExpired(t *testing.T) {
	tr := &InMemoryRefusalTracker{
		entries:            make(map[int]*refusalWindowEntry),
		cleanupMinInterval: 1 * time.Millisecond,
	}
	userID := 1007
	tr.Record(userID, true, 3)
	// 手动把 touchedAt 推到过去，模拟长期不活跃。
	tr.mu.Lock()
	tr.entries[userID].touchedAt = time.Now().Add(-1 * time.Hour)
	tr.mu.Unlock()
	tr.sweepExpired()
	total, _ := tr.Snapshot(userID)
	assert.Equal(t, 0, total, "idle entry should be swept")
}
