package common

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamStatus_SetEndReason_FirstWins(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	s.SetEndReason(StreamEndReasonDone, nil)
	s.SetEndReason(StreamEndReasonTimeout, nil)
	s.SetEndReason(StreamEndReasonClientGone, fmt.Errorf("context canceled"))

	assert.Equal(t, StreamEndReasonDone, s.EndReason)
	assert.Nil(t, s.EndError)
}

func TestStreamStatus_SetEndReason_WithError(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	expectedErr := fmt.Errorf("read: connection reset")
	s.SetEndReason(StreamEndReasonScannerErr, expectedErr)

	assert.Equal(t, StreamEndReasonScannerErr, s.EndReason)
	assert.Equal(t, expectedErr, s.EndError)
}

func TestStreamStatus_SetEndReason_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	s.SetEndReason(StreamEndReasonDone, nil)
}

func TestStreamStatus_SetEndReason_Concurrent(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	reasons := []StreamEndReason{
		StreamEndReasonDone,
		StreamEndReasonTimeout,
		StreamEndReasonClientGone,
		StreamEndReasonScannerErr,
		StreamEndReasonHandlerStop,
		StreamEndReasonEOF,
		StreamEndReasonPanic,
		StreamEndReasonPingFail,
	}

	var wg sync.WaitGroup
	for _, r := range reasons {
		wg.Add(1)
		go func(reason StreamEndReason) {
			defer wg.Done()
			s.SetEndReason(reason, nil)
		}(r)
	}
	wg.Wait()

	assert.NotEqual(t, StreamEndReasonNone, s.EndReason)
}

func TestStreamStatus_RecordError_Basic(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	s.RecordError("bad json")
	s.RecordError("another bad json")
	s.RecordError("client gone")

	assert.True(t, s.HasErrors())
	assert.Equal(t, 3, s.TotalErrorCount())
	assert.Len(t, s.Errors, 3)
}

func TestStreamStatus_RecordError_CapAtMax(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	for i := 0; i < 30; i++ {
		s.RecordError(fmt.Sprintf("error_%d", i))
	}

	assert.Equal(t, maxStreamErrorEntries, len(s.Errors))
	assert.Equal(t, 30, s.TotalErrorCount())
}

func TestStreamStatus_RecordError_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	s.RecordError("should not panic")
}

func TestStreamStatus_RecordError_Concurrent(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.RecordError(fmt.Sprintf("error_%d", idx))
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 100, s.TotalErrorCount())
	assert.LessOrEqual(t, len(s.Errors), maxStreamErrorEntries)
}

func TestStreamStatus_HasErrors_Empty(t *testing.T) {
	t.Parallel()
	s := NewStreamStatus()
	assert.False(t, s.HasErrors())
	assert.Equal(t, 0, s.TotalErrorCount())
}

func TestStreamStatus_HasErrors_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.False(t, s.HasErrors())
	assert.Equal(t, 0, s.TotalErrorCount())
}

func TestStreamStatus_IsNormalEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		reason StreamEndReason
		normal bool
	}{
		{StreamEndReasonDone, true},
		{StreamEndReasonEOF, true},
		{StreamEndReasonHandlerStop, true},
		{StreamEndReasonTimeout, false},
		{StreamEndReasonClientGone, false},
		{StreamEndReasonScannerErr, false},
		{StreamEndReasonPanic, false},
		{StreamEndReasonPingFail, false},
		{StreamEndReasonUpstreamError, false},
		{StreamEndReasonNone, false},
	}
	for _, tt := range tests {
		s := NewStreamStatus()
		s.SetEndReason(tt.reason, nil)
		assert.Equal(t, tt.normal, s.IsNormalEnd(), "reason=%s", tt.reason)
	}
}

func TestStreamStatus_IsNormalEnd_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.True(t, s.IsNormalEnd())
}

// TestStreamStatus_ObservedUpstreamError 覆盖 endOnce 竞态兜底路径：
// scanner 观察到过上游错误终止事件（RecordError），无论 EndReason 是不是
// 被 ClientGone 抢跑成功，都应返回 true。
func TestStreamStatus_ObservedUpstreamError(t *testing.T) {
	t.Parallel()

	// nil 安全
	var nilS *StreamStatus
	assert.False(t, nilS.ObservedUpstreamError())

	// 严格路径：EndReason=UpstreamError → true（不管有没有 RecordError）
	s := NewStreamStatus()
	s.SetEndReason(StreamEndReasonUpstreamError, nil)
	assert.True(t, s.ObservedUpstreamError(),
		"EndReason=UpstreamError 时必须为 true（严格路径）")

	// 严格路径 + RecordError → 仍 true
	s = NewStreamStatus()
	s.RecordError("boom")
	s.SetEndReason(StreamEndReasonUpstreamError, nil)
	assert.True(t, s.ObservedUpstreamError())

	// 竞态兜底：EndReason=ClientGone 但 scanner 曾 RecordError → true
	s = NewStreamStatus()
	s.RecordError("upstream sent event: error")
	s.SetEndReason(StreamEndReasonClientGone, nil)
	assert.True(t, s.ObservedUpstreamError(),
		"ClientGone 抢跑但 scanner 记录过错误 → 视作观察到上游错误，兜底走退款")

	// ClientGone 且无 RecordError → false（真正的客户端主动断开）
	s = NewStreamStatus()
	s.SetEndReason(StreamEndReasonClientGone, nil)
	assert.False(t, s.ObservedUpstreamError(),
		"ClientGone 且无错误记录 → 真·客户端断开，不能升级为上游错误")

	// 其他端因不该被误升级：EOF / Done / Timeout 等即便 RecordError 过也不算
	// （EOF 场景的 soft error 是网络抖动重试成功后的残留，不是终止错误）
	for _, reason := range []StreamEndReason{
		StreamEndReasonEOF,
		StreamEndReasonDone,
		StreamEndReasonHandlerStop,
		StreamEndReasonTimeout,
		StreamEndReasonScannerErr,
		StreamEndReasonPanic,
		StreamEndReasonPingFail,
	} {
		s = NewStreamStatus()
		s.RecordError("some soft error")
		s.SetEndReason(reason, nil)
		assert.False(t, s.ObservedUpstreamError(),
			"EndReason=%s 有软错误也不能升级为上游错误", reason)
	}
}

func TestStreamStatus_Summary(t *testing.T) {
	t.Parallel()

	s := NewStreamStatus()
	s.SetEndReason(StreamEndReasonDone, nil)
	summary := s.Summary()
	assert.Contains(t, summary, "reason=done")
	assert.NotContains(t, summary, "soft_errors")

	s2 := NewStreamStatus()
	s2.SetEndReason(StreamEndReasonTimeout, nil)
	s2.RecordError("bad json")
	s2.RecordError("write failed")
	summary2 := s2.Summary()
	assert.Contains(t, summary2, "reason=timeout")
	assert.Contains(t, summary2, "soft_errors=2")
}

func TestStreamStatus_Summary_NilSafe(t *testing.T) {
	t.Parallel()
	var s *StreamStatus
	assert.Equal(t, "StreamStatus<nil>", s.Summary())
}

func TestStreamStatus_HasUpstreamError(t *testing.T) {
	t.Parallel()

	// nil safe
	var nilStatus *StreamStatus
	assert.False(t, nilStatus.HasUpstreamError())

	// 未设置端因：false
	s := NewStreamStatus()
	assert.False(t, s.HasUpstreamError())

	// 端因是 Done / EOF：false（即使有软错误累计）
	s = NewStreamStatus()
	s.RecordError("soft")
	s.SetEndReason(StreamEndReasonEOF, nil)
	assert.False(t, s.HasUpstreamError(), "EOF 结束即使有软错误也不算 upstream error")

	// 端因是 UpstreamError：true
	s = NewStreamStatus()
	s.SetEndReason(StreamEndReasonUpstreamError, fmt.Errorf("boom"))
	assert.True(t, s.HasUpstreamError())
}

func TestStreamStatus_FirstErrorMessage(t *testing.T) {
	t.Parallel()

	// nil safe
	var nilStatus *StreamStatus
	assert.Equal(t, "", nilStatus.FirstErrorMessage())

	// 无错误
	s := NewStreamStatus()
	assert.Equal(t, "", s.FirstErrorMessage())

	// 多条错误：返回第一条
	s = NewStreamStatus()
	s.RecordError("first error")
	s.RecordError("second error")
	assert.Equal(t, "first error", s.FirstErrorMessage())
}
