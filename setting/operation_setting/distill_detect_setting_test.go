package operation_setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
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

// ── 旧配置迁移（Deploy 实测到的启动顺序 bug）────────────────────────────

// withLegacyOption 临时把一条 legacy 开关塞进 OptionMap，测试结束还原。
//
// ⚠️ 这个包里 OptionMap 默认是 nil（InitOptionMap 没跑过），要先建出来。
func withLegacyOption(t *testing.T, key, value string) {
	t.Helper()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
		t.Cleanup(func() { common.OptionMap = nil })
	}
	prev, existed := common.OptionMap[key]
	common.OptionMap[key] = value
	t.Cleanup(func() {
		if existed {
			common.OptionMap[key] = prev
		} else {
			delete(common.OptionMap, key)
		}
	})
}

// withCleanQuestions 把题库复位到默认值，测试结束再复位一次。
func withCleanQuestions(t *testing.T) {
	t.Helper()
	distillDetectSetting.Questions = DefaultDistillQuestions()
	t.Cleanup(func() { distillDetectSetting.Questions = DefaultDistillQuestions() })
}

// ⭐ Deploy 在真实实例上踩到的 bug：InitOptionMap 会先跑 ExportAllConfigs，
// 把默认题库 JSON 预填进 OptionMap，于是「看 OptionMap 有没有 questions 键」
// 这个守卫永远成立，迁移一次也不执行 —— 库里 question_reward_rl=false，
// 页面上却显示 enabled=true。
//
// 判据必须是数据库里有没有 questions 这一行，由调用方传进来。
func TestMigrateLegacyTogglesRespectsTheDatabaseRowFlag(t *testing.T) {
	withCleanQuestions(t)
	withLegacyOption(t, "distill_detect_setting.question_reward_rl", "false")
	// 模拟 ExportAllConfigs 已经把默认题库预填进 OptionMap。
	common.OptionMap["distill_detect_setting.questions"] = `[{"id":"reward_rl","enabled":true}]`

	// 库里已经有 questions 行：以库为准，老开关不覆盖。
	MigrateLegacyDistillQuestionToggles(true)
	for _, q := range distillDetectSetting.Questions {
		if q.ID == "reward_rl" && !q.Enabled {
			t.Fatal("a persisted questions row must win over legacy toggles")
		}
	}

	// 库里没有 questions 行：老开关必须生效。
	MigrateLegacyDistillQuestionToggles(false)
	for _, q := range distillDetectSetting.Questions {
		if q.ID == "reward_rl" && q.Enabled {
			t.Fatal("without a persisted row the legacy toggle must be applied")
		}
	}
}

// 老配置把四题全关时必须恢复默认题库：全关等于判定永远不命中，
// 功能会静默失效。
func TestMigrateLegacyTogglesRestoresDefaultsWhenEverythingWasOff(t *testing.T) {
	withCleanQuestions(t)
	for _, legacy := range distillLegacyQuestionKeys {
		withLegacyOption(t, legacy.Key, "false")
	}

	MigrateLegacyDistillQuestionToggles(false)

	if len(distillDetectSetting.EnabledDistillQuestions()) == 0 {
		t.Fatal("an all-disabled legacy bank must fall back to the default bank")
	}
}
