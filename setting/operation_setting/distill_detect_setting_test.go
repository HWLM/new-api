package operation_setting

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
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

// ── 检测渠道白名单（规格 §16）────────────────────────────────────────

// 白名单是「本次请求分发到的渠道」是否要检测的唯一判据。
// ⛔ 空列表必须一个都不检测：早期语义含混时，运营「开了检测却没配渠道」
// 会以为在跑，实际全放过。
func TestDetectsChannelRequiresAnExplicitWhitelist(t *testing.T) {
	s := &DistillDetectSetting{}
	if s.DetectsChannel(7) {
		t.Fatal("an empty whitelist must detect nothing")
	}
	s.DetectChannelIds = []int{7, 9}
	if !s.DetectsChannel(7) || !s.DetectsChannel(9) {
		t.Fatal("a whitelisted channel must be detected")
	}
	if s.DetectsChannel(8) {
		t.Fatal("a channel outside the whitelist must be skipped")
	}
	if s.DetectsChannel(0) || s.DetectsChannel(-1) {
		t.Fatal("a non-positive channel id is never detected")
	}
}

// 归一化：去重、丢非正数、保持运营选择顺序（管理页回显要稳定）。
func TestNormalizeDistillDetectChannelIds(t *testing.T) {
	got := NormalizeDistillDetectChannelIds([]int{9, 0, 7, 9, -3, 7, 11})
	want := []int{9, 7, 11}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

// ⛔ 检测渠道与兜底渠道互斥：同一渠道既被判定又当兜底，就是
// 「判定 → 切到自己 → 再判定」的自环。
func TestCheckDistillChannelConflict(t *testing.T) {
	if err := CheckDistillChannelConflict([]int{3, 5}, 0); err != nil {
		t.Fatalf("no fallback configured means no conflict: %v", err)
	}
	if err := CheckDistillChannelConflict([]int{3, 5}, 9); err != nil {
		t.Fatalf("a fallback outside the whitelist must pass: %v", err)
	}
	err := CheckDistillChannelConflict([]int{3, 5}, 5)
	if err == nil {
		t.Fatal("a channel in both lists must be rejected")
	}
	var conflict *DistillChannelConflictError
	if !errors.As(err, &conflict) || conflict.ChannelID != 5 {
		t.Fatalf("conflict must name channel 5, got %v", err)
	}
}

// 白名单也要跟着配置走：这是 config 包里按 json tag 展开成 kv 的字段，
// 名字一旦拼错，保存成功但重启后失效。
func TestDetectChannelIdsSurvivesConfigRoundTrip(t *testing.T) {
	cm := config.NewConfigManager()
	s := &DistillDetectSetting{DetectChannelIds: []int{4, 8}}
	cm.Register("test_distill_detect", s)

	saved := map[string]string{}
	if err := cm.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	raw, ok := saved["test_distill_detect.detect_channel_ids"]
	if !ok {
		t.Fatalf("detect_channel_ids must be persisted, got keys %v", saved)
	}
	if !strings.Contains(raw, "4") || !strings.Contains(raw, "8") {
		t.Fatalf("persisted value lost the ids: %s", raw)
	}

	restored := &DistillDetectSetting{}
	cm2 := config.NewConfigManager()
	cm2.Register("test_distill_detect", restored)
	if err := cm2.LoadFromDB(map[string]string{"test_distill_detect.detect_channel_ids": "[4,8]"}); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(restored.DetectChannelIds) != 2 || restored.DetectChannelIds[0] != 4 || restored.DetectChannelIds[1] != 8 {
		t.Fatalf("round trip lost the whitelist: %+v", restored.DetectChannelIds)
	}
}
