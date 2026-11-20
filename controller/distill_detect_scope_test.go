package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
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

// 普通用户的返回体里不能出现运营视角的字段，但**逐题结果要给**（规格 §18）：
// 用户就是要知道自己命中了哪几道题、各多少分。拿走的只有原始 id 键值
// （categories）和题面（prompt 本来就不出管理端）。
func TestDistillHitUserViewHidesOperatorFieldsButKeepsQuestionResults(t *testing.T) {
	raw, err := json.Marshal(DistillHitUserView{
		Id:        1,
		RequestId: "req",
		Model:     "m",
		Score:     0.9,
		QuestionResults: []service.DistillQuestionResult{
			{ID: "corpus_farm", Name: "批量语料生产", Score: 0.9, Hit: true},
		},
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	for _, banned := range []string{"categories", "channel_id", "fallback_id", "alert_sent", "username", "user_id"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("the user-facing view must not expose %q: %s", banned, raw)
		}
	}
	// ⛔ 题名必须来自快照里的 name，而不是让前端拿 id 去查（普通用户没有设置接口）。
	for _, wanted := range []string{"request_id", "model", "score", "sampled_at", "question_results", `"name":"批量语料生产"`, `"hit":true`} {
		if !strings.Contains(string(raw), wanted) {
			t.Fatalf("the user-facing view should keep %q: %s", wanted, raw)
		}
	}
}

// ⛔ 老记录没有逐题快照、也不是新写的命中时，question_results 必须是 []，
// 不是 null：detect_channel_ids 已经在同一个坑里摔过一次。
func TestDistillHitQuestionResultsIsNeverNil(t *testing.T) {
	results := distillHitQuestionResults(model.DistillDetectHit{})
	raw, err := json.Marshal(DistillHitUserView{QuestionResults: results})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(raw), `"question_results":[]`) {
		t.Fatalf("question_results must serialize as [], got %s", raw)
	}
}
