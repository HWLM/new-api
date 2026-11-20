package operation_setting

import (
	"strings"
	"testing"
)

// 题库校验（规格 §14.1）。这里的每一条都是后端**强制**的：前端也会挡，
// 但绕不过 HTTP 调用，所以判定口径必须由这一层守住。

func validQuestions() []DistillQuestion {
	return []DistillQuestion{
		{ID: "a", Name: "A", Prompt: "prompt a", Enabled: true},
		{ID: "b_2", Name: "B", Prompt: "prompt b", Enabled: false},
	}
}

func TestValidateDistillQuestionsAcceptsAWellFormedBank(t *testing.T) {
	if err := ValidateDistillQuestions(validQuestions()); err != nil {
		t.Fatalf("a well-formed bank must pass: %v", err)
	}
}

// ⛔ 一题都不启用必须拒绝：判定会永远不命中，功能静默失效，
// 而管理页看起来一切正常 —— 这是最难被发现的失败。
func TestValidateDistillQuestionsRejectsAnAllDisabledBank(t *testing.T) {
	qs := validQuestions()
	for i := range qs {
		qs[i].Enabled = false
	}
	if err := ValidateDistillQuestions(qs); err == nil {
		t.Fatal("a bank with no enabled question must be rejected")
	}
}

func TestValidateDistillQuestionsRejectsBadIDs(t *testing.T) {
	for _, id := range []string{"", "Upper", "with-dash", "with space", "中文", strings.Repeat("x", DistillQuestionMaxIDLen+1)} {
		qs := validQuestions()
		qs[0].ID = id
		if err := ValidateDistillQuestions(qs); err == nil {
			t.Fatalf("id %q must be rejected", id)
		}
	}
}

func TestValidateDistillQuestionsRejectsDuplicateIDs(t *testing.T) {
	qs := validQuestions()
	qs[1].ID = qs[0].ID
	if err := ValidateDistillQuestions(qs); err == nil {
		t.Fatal("duplicate ids must be rejected")
	}
}

func TestValidateDistillQuestionsEnforcesCountBounds(t *testing.T) {
	if err := ValidateDistillQuestions(nil); err == nil {
		t.Fatal("an empty bank must be rejected")
	}
	tooMany := make([]DistillQuestion, 0, DistillQuestionMaxCount+1)
	for i := 0; i <= DistillQuestionMaxCount; i++ {
		tooMany = append(tooMany, DistillQuestion{
			ID:      "q" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Name:    "n",
			Prompt:  "p",
			Enabled: true,
		})
	}
	if err := ValidateDistillQuestions(tooMany); err == nil {
		t.Fatalf("more than %d questions must be rejected", DistillQuestionMaxCount)
	}
}

func TestValidateDistillQuestionsEnforcesFieldLengths(t *testing.T) {
	qs := validQuestions()
	qs[0].Name = ""
	if err := ValidateDistillQuestions(qs); err == nil {
		t.Fatal("an empty name must be rejected")
	}

	qs = validQuestions()
	qs[0].Name = strings.Repeat("名", DistillQuestionMaxNameLen+1)
	if err := ValidateDistillQuestions(qs); err == nil {
		t.Fatal("an over-long name must be rejected")
	}

	qs = validQuestions()
	qs[0].Prompt = "   "
	if err := ValidateDistillQuestions(qs); err == nil {
		t.Fatal("a blank prompt must be rejected")
	}

	qs = validQuestions()
	qs[0].Prompt = strings.Repeat("p", DistillQuestionMaxPromptLen+1)
	if err := ValidateDistillQuestions(qs); err == nil {
		t.Fatal("an over-long prompt must be rejected")
	}
}

// 归一化去掉两端空白；判定与落库都用归一化后的值，免得 id 末尾多个空格
// 导致 wire key 对不上。
func TestNormalizeDistillQuestionsTrimsFields(t *testing.T) {
	out := NormalizeDistillQuestions([]DistillQuestion{
		{ID: "  a  ", Name: " N ", Prompt: "  p  ", Enabled: true},
	})
	if out[0].ID != "a" || out[0].Name != "N" || out[0].Prompt != "p" {
		t.Fatalf("fields must be trimmed, got %+v", out[0])
	}
}

// EnabledDistillQuestions 只返回启用的题，且保持配置顺序（顺序即送判定的顺序）。
func TestEnabledDistillQuestionsKeepsOrder(t *testing.T) {
	s := &DistillDetectSetting{Questions: []DistillQuestion{
		{ID: "a", Enabled: true},
		{ID: "b", Enabled: false},
		{ID: "c", Enabled: true},
	}}
	got := s.EnabledDistillQuestions()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("enabled questions must keep configuration order, got %+v", got)
	}
}
