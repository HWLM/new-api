/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetOverloadThrottleMem 单元测试专用：清空进程内节流状态。
func resetOverloadThrottleMem() {
	overloadThrottleOnce = sync.Once{}
	overloadThrottleInst = nil
}

func TestOverloadThrottleMemStore_FirstAcquireSucceeds(t *testing.T) {
	resetOverloadThrottleMem()
	s := overloadThrottleMem()

	require.True(t, s.tryAcquire("concurrency:1", time.Minute),
		"the first call for a fresh user must succeed")
}

func TestOverloadThrottleMemStore_SecondAcquireBlockedInsideWindow(t *testing.T) {
	resetOverloadThrottleMem()
	s := overloadThrottleMem()

	require.True(t, s.tryAcquire("concurrency:7", time.Minute))
	assert.False(t, s.tryAcquire("concurrency:7", time.Minute),
		"the second call inside the window must be blocked")
}

func TestOverloadThrottleMemStore_DifferentKeysIndependent(t *testing.T) {
	resetOverloadThrottleMem()
	s := overloadThrottleMem()

	assert.True(t, s.tryAcquire("concurrency:10", time.Minute))
	assert.True(t, s.tryAcquire("concurrency:11", time.Minute),
		"different users must not share throttle state")
	// 不同 kind（prefix）也应独立
	assert.True(t, s.tryAcquire("refusal:10", time.Minute),
		"different throttle kinds for same user must not share state")
}

func TestOverloadThrottleMemStore_ReacquiresAfterWindow(t *testing.T) {
	resetOverloadThrottleMem()
	s := overloadThrottleMem()

	require.True(t, s.tryAcquire("concurrency:42", time.Millisecond))
	// 零窗口的话第二次会同一纳秒命中「<window」，必须 >0；用极短窗口加 sleep
	time.Sleep(5 * time.Millisecond)
	assert.True(t, s.tryAcquire("concurrency:42", time.Millisecond),
		"after the window expires the same user can send again")
}

func TestBuildConcurrencyOverloadText_IncludesCoreFields(t *testing.T) {
	u := overloadNotifyUser{ID: 123, Username: "alice"}
	text := buildConcurrencyOverloadText(u, 50, 10, 77, 88, OverloadFallbackRedirected)

	assert.Contains(t, text, "并发超限告警")
	assert.Contains(t, text, "alice", "username must appear when known")
	assert.Contains(t, text, "id=123", "user id must appear")
	assert.Contains(t, text, "50 / 上限 10", "current count and limit must appear verbatim")
	assert.Contains(t, text, "原渠道：77", "originalChannelID must be rendered when > 0")
	assert.Contains(t, text, "已重导到兜底渠道：88", "redirected outcome must name the fallback channel id")
}

func TestBuildConcurrencyOverloadText_FallsBackToIdWhenNoUsername(t *testing.T) {
	u := overloadNotifyUser{ID: 999, Username: ""}
	text := buildConcurrencyOverloadText(u, 1, 1, 0, 0, OverloadFallbackUnavailable)

	assert.Contains(t, text, "id=999",
		"a missing username must still produce an id-only display")
	assert.NotContains(t, text, "原渠道", "originalChannelID=0 must not be rendered")
	assert.Contains(t, text, "无可用兜底渠道", "unavailable outcome must say so explicitly")
	assert.Contains(t, text, "429", "unavailable outcome must mention the 429 result for the client")
}

func TestBuildRefusalDemoteText_RendersRatioAndDuration(t *testing.T) {
	u := overloadNotifyUser{ID: 55, Username: "bob"}
	text := buildRefusalDemoteText(u, 7, 10, 30, 1800, 42, 99, OverloadFallbackRedirected)

	assert.Contains(t, text, "拒答比超限告警")
	assert.Contains(t, text, "bob (id=55)")
	// 70% ratio should surface, exact format matters for the on-call pager.
	assert.Contains(t, text, "70.0%")
	assert.Contains(t, text, "(7/10)")
	assert.Contains(t, text, "阈值 30%")
	assert.Contains(t, text, "降级持续：1800 秒")
	assert.Contains(t, text, "原渠道：42")
	assert.Contains(t, text, "已重导到兜底渠道：99")
}

func TestBuildRefusalDemoteText_HandlesZeroWindowSafely(t *testing.T) {
	// A zero window must not panic (division by zero) and must emit 0.0%.
	u := overloadNotifyUser{ID: 1, Username: ""}
	text := buildRefusalDemoteText(u, 0, 0, 30, 60, 0, 0, OverloadFallbackUnavailable)

	assert.Contains(t, text, "0.0%", "zero sample window renders 0% ratio, no panic")
	assert.True(t, strings.Contains(text, "id=1"))
	assert.Contains(t, text, "无可用兜底渠道", "unavailable outcome must say so explicitly")
}

func TestFormatUserDisplay(t *testing.T) {
	assert.Equal(t, "alice (id=1)", formatUserDisplay(overloadNotifyUser{ID: 1, Username: "alice"}))
	assert.Equal(t, "id=2", formatUserDisplay(overloadNotifyUser{ID: 2, Username: ""}))
}
