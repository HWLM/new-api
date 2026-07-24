package common

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type StreamEndReason string

const (
	StreamEndReasonNone        StreamEndReason = ""
	StreamEndReasonDone        StreamEndReason = "done"
	StreamEndReasonTimeout     StreamEndReason = "timeout"
	StreamEndReasonClientGone  StreamEndReason = "client_gone"
	StreamEndReasonScannerErr  StreamEndReason = "scanner_error"
	StreamEndReasonHandlerStop StreamEndReason = "handler_stop"
	StreamEndReasonEOF         StreamEndReason = "eof"
	StreamEndReasonPanic       StreamEndReason = "panic"
	StreamEndReasonPingFail    StreamEndReason = "ping_fail"
	// StreamEndReasonUpstreamError 表示流内识别到上游 SSE 错误事件
	// （event: error / event: response.failed / event: response.error），
	// 此时上游已经开始流式响应但中途明确终止在错误状态。
	// 与 EOF 不同，这类结束不应触发本地 token 估算兜底计费。
	StreamEndReasonUpstreamError StreamEndReason = "upstream_error"
)

const maxStreamErrorEntries = 20

type StreamErrorEntry struct {
	Message   string
	Timestamp time.Time
}

type StreamStatus struct {
	EndReason StreamEndReason
	EndError  error
	endOnce   sync.Once

	mu         sync.Mutex
	Errors     []StreamErrorEntry
	ErrorCount int
}

func NewStreamStatus() *StreamStatus {
	return &StreamStatus{}
}

func (s *StreamStatus) SetEndReason(reason StreamEndReason, err error) {
	if s == nil {
		return
	}
	s.endOnce.Do(func() {
		s.EndReason = reason
		s.EndError = err
	})
}

func (s *StreamStatus) RecordError(msg string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ErrorCount++
	if len(s.Errors) < maxStreamErrorEntries {
		s.Errors = append(s.Errors, StreamErrorEntry{
			Message:   msg,
			Timestamp: time.Now(),
		})
	}
}

func (s *StreamStatus) HasErrors() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ErrorCount > 0
}

func (s *StreamStatus) TotalErrorCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ErrorCount
}

func (s *StreamStatus) IsNormalEnd() bool {
	if s == nil {
		return true
	}
	return s.EndReason == StreamEndReasonDone ||
		s.EndReason == StreamEndReasonEOF ||
		s.EndReason == StreamEndReasonHandlerStop
}

// HasUpstreamError 表示流内识别到上游 SSE 错误事件（event: error / response.failed 等）。
// 与 HasErrors() 语义不同：HasErrors 只是软错误累计计数，可能在流仍然正常结束时也非零；
// HasUpstreamError 只在端因判定为 StreamEndReasonUpstreamError 时为 true，
// 用于结算路径判断是否豁免本地 token 估算兜底扣费。
func (s *StreamStatus) HasUpstreamError() bool {
	if s == nil {
		return false
	}
	return s.EndReason == StreamEndReasonUpstreamError
}

// ObservedUpstreamError 表示 scanner 至少观察到过一次上游错误终止事件帧，
// 覆盖 endOnce 竞态兜底：
//
//   - HasUpstreamError() 严格判定 EndReason == StreamEndReasonUpstreamError，
//     要求 scanner 在客户端 context.Done() 之前抢先 SetEndReason；
//   - 实测该竞态几乎必输 —— 上游发 event: error 帧后，客户端 SDK（Claude Code
//     等）迅速关连接，new-api 侧 context.Done() 比 scanner 处理错误帧更快触发，
//     导致 EndReason 被固化为 ClientGone。scanner 后续的 RecordError 仍然会
//     追加 Errors，但 SetEndReason(UpstreamError) 已经是 no-op。
//
// ObservedUpstreamError() 在上述情况下也返回 true：EndReason 是 ClientGone
// 但 HasErrors() 为真，说明 scanner 确实收到过上游错误帧、只是被抢跑。
// UpstreamStreamErrorToAPIError 依据此方法给出退款决定，避免因时序竞争漏放。
func (s *StreamStatus) ObservedUpstreamError() bool {
	if s == nil {
		return false
	}
	if s.EndReason == StreamEndReasonUpstreamError {
		return true
	}
	if s.EndReason == StreamEndReasonClientGone && s.HasErrors() {
		return true
	}
	return false
}

// FirstErrorMessage 返回 StreamStatus 中第一条软错误 message；
// 无错误时返回空串。供 handler 层构造对客错误响应时复用。
func (s *StreamStatus) FirstErrorMessage() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Errors) == 0 {
		return ""
	}
	return s.Errors[0].Message
}

func (s *StreamStatus) Summary() string {
	if s == nil {
		return "StreamStatus<nil>"
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "reason=%s", s.EndReason)
	if s.EndError != nil {
		fmt.Fprintf(b, " end_error=%q", s.EndError.Error())
	}
	s.mu.Lock()
	if s.ErrorCount > 0 {
		fmt.Fprintf(b, " soft_errors=%d", s.ErrorCount)
	}
	s.mu.Unlock()
	return b.String()
}
