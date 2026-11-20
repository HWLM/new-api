package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// 蒸馏兜底的计费分组要落进消费日志（规格 §15）：
// 对账时一眼看出这笔按谁的价格算的。

func newDistillLogTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return ctx
}

func distillAdminInfo(t *testing.T, other map[string]interface{}) map[string]interface{} {
	t.Helper()
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		t.Fatalf("admin_info must be present, got %T", other["admin_info"])
	}
	return adminInfo
}

func TestDistillFallbackGroupsAreWrittenToTheLog(t *testing.T) {
	ctx := newDistillLogTestContext(t)
	common.SetContextKey(ctx, constant.ContextKeyDistillFallbackChannelId, 42)
	common.SetContextKey(ctx, constant.ContextKeyDistillOriginGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyDistillFallbackGroup, "backup")

	other := map[string]interface{}{}
	appendDistillFallbackGroupInfo(ctx, other)

	adminInfo := distillAdminInfo(t, other)
	if adminInfo["distill_origin_group"] != "default" {
		t.Fatalf("distill_origin_group must be logged, got %v", adminInfo["distill_origin_group"])
	}
	if adminInfo["distill_fallback_group"] != "backup" {
		t.Fatalf("distill_fallback_group must be logged, got %v", adminInfo["distill_fallback_group"])
	}
}

// 没真的换渠道（观察模式 / 兜底不可用）时不写这两个字段：
// 记两个相同的值只会让对账时误以为发生过改价。
func TestDistillFallbackGroupsAreAbsentWithoutARedirect(t *testing.T) {
	ctx := newDistillLogTestContext(t)
	common.SetContextKey(ctx, constant.ContextKeyDistillOriginGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyDistillFallbackGroup, "default")

	other := map[string]interface{}{}
	appendDistillFallbackGroupInfo(ctx, other)

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		return // 没有 admin_info 自然也没有这两个字段
	}
	if _, exists := adminInfo["distill_fallback_group"]; exists {
		t.Fatal("the fallback group must not be logged when no redirect happened")
	}
}

// 这两个字段属于路由细节，必须落在 admin_info 之下 —— 普通用户的日志视图
// 会把 admin_info 整个剥掉，否则等于把风控命中告诉被命中的客户。
func TestDistillFallbackGroupsLiveUnderAdminInfo(t *testing.T) {
	ctx := newDistillLogTestContext(t)
	common.SetContextKey(ctx, constant.ContextKeyDistillFallbackChannelId, 42)
	common.SetContextKey(ctx, constant.ContextKeyDistillFallbackGroup, "backup")

	other := map[string]interface{}{}
	appendDistillFallbackGroupInfo(ctx, other)

	if _, topLevel := other["distill_fallback_group"]; topLevel {
		t.Fatal("the routing group must not leak to the top-level log fields")
	}
	if distillAdminInfo(t, other)["distill_fallback_group"] != "backup" {
		t.Fatal("the routing group belongs under admin_info")
	}
}
