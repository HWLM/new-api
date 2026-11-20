package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// 计费分组的解析（规格 §15）。
//
// ⛔ 这个函数是「预扣 / 结算 / 日志三处同源」的唯一入口：只要三处都经由它，
// 就不会出现「预扣按新分组、结算还按老分组」的分裂。

func newGroupTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return ctx
}

// auto_group 优先于 RelayInfo 上带的分组：选渠道 / 兜底重导都会写这个键。
func TestResolveUsingGroupPrefersTheAutoGroupContextKey(t *testing.T) {
	ctx := newGroupTestContext(t)
	info := &RelayInfo{UsingGroup: "default"}

	if got := info.ResolveUsingGroup(ctx); got != "default" {
		t.Fatalf("without an auto_group key the relay info group wins, got %q", got)
	}

	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "backup")
	if got := info.ResolveUsingGroup(ctx); got != "backup" {
		t.Fatalf("auto_group must win over the relay info group, got %q", got)
	}
}

// 空串的 auto_group 不能把分组清空：那会让计费落到一个不存在的分组上。
func TestResolveUsingGroupIgnoresAnEmptyAutoGroup(t *testing.T) {
	ctx := newGroupTestContext(t)
	info := &RelayInfo{UsingGroup: "default"}
	common.SetContextKey(ctx, constant.ContextKeyAutoGroup, "")

	if got := info.ResolveUsingGroup(ctx); got != "default" {
		t.Fatalf("an empty auto_group must not clear the group, got %q", got)
	}
}

func TestResolveUsingGroupToleratesANilContext(t *testing.T) {
	info := &RelayInfo{UsingGroup: "default"}
	if got := info.ResolveUsingGroup(nil); got != "default" {
		t.Fatalf("a nil context must fall back to the relay info group, got %q", got)
	}
}
