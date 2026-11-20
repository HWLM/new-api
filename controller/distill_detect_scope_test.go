package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

// 命中记录的作用域（规格 §17.1）。
//
// ⛔ 这是鉴权问题，不是前端问题：普通用户手改 URL 上的 ?user_id= 也不能看到
// 别人的记录。前面的前端隐藏参数、隐藏列都不算防线。

func TestDistillHitUserScopeForcesTheCallersOwnRecords(t *testing.T) {
	const selfID = 42
	for _, role := range []int{common.RoleGuestUser, common.RoleCommonUser} {
		got, err := distillHitUserScope(role, selfID, "999")
		if err != nil {
			t.Fatalf("role %d: overriding the id must not be an error, got %v", role, err)
		}
		if got != selfID {
			t.Fatalf("role %d: must be forced to self, got %d", role, got)
		}
		if got, _ := distillHitUserScope(role, selfID, ""); got != selfID {
			t.Fatalf("role %d: without a filter it must still be self, got %d", role, got)
		}
		// 非法值也不能放过：直接覆盖，别把参数交给下游解析。
		if got, err := distillHitUserScope(role, selfID, "abc"); err != nil || got != selfID {
			t.Fatalf("role %d: a malformed user_id must be overridden, got %d err=%v", role, got, err)
		}
	}
}

func TestDistillHitUserScopeLetsAdminsFilterAnyone(t *testing.T) {
	for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		if got, err := distillHitUserScope(role, 1, ""); err != nil || got != 0 {
			t.Fatalf("role %d: no filter means no scoping, got %d err=%v", role, got, err)
		}
		if got, err := distillHitUserScope(role, 1, "7"); err != nil || got != 7 {
			t.Fatalf("role %d: an admin must be able to filter by user, got %d err=%v", role, got, err)
		}
		if _, err := distillHitUserScope(role, 1, "abc"); err == nil {
			t.Fatalf("role %d: a malformed admin filter should be reported", role)
		}
	}
}

// 普通用户的返回体里不能出现运营视角的字段：逐题分数（categories）一旦露出去，
// 等于把判定口径交给被判定的人。
func TestDistillHitUserViewHidesOperatorFields(t *testing.T) {
	raw, err := json.Marshal(DistillHitUserView{Id: 1, RequestId: "req", Model: "m", Score: 0.9})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	for _, banned := range []string{"categories", "channel_id", "fallback_id", "alert_sent", "username", "user_id"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("the user-facing view must not expose %q: %s", banned, raw)
		}
	}
	for _, wanted := range []string{"request_id", "model", "score", "sampled_at"} {
		if !strings.Contains(string(raw), wanted) {
			t.Fatalf("the user-facing view should keep %q: %s", wanted, raw)
		}
	}
}
