package config

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

type testConfigWithMap struct {
	Modes map[string]string `json:"modes"`
	Exprs map[string]string `json:"exprs"`
	Name  string            `json:"name"`
}

func TestUpdateConfigFromMap_MapReplacement(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{
			"model-a": "tiered_expr",
			"model-b": "tiered_expr",
		},
		Exprs: map[string]string{
			"model-a": "p * 5 + c * 25",
			"model-b": "p * 10 + c * 50",
		},
		Name: "billing",
	}

	// Simulate removing model-a: new value only has model-b
	err := UpdateConfigFromMap(cfg, map[string]string{
		"modes": `{"model-b": "tiered_expr"}`,
		"exprs": `{"model-b": "p * 10 + c * 50"}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if _, ok := cfg.Modes["model-a"]; ok {
		t.Errorf("Modes still contains model-a after it was removed from the update; got %v", cfg.Modes)
	}
	if _, ok := cfg.Exprs["model-a"]; ok {
		t.Errorf("Exprs still contains model-a after it was removed from the update; got %v", cfg.Exprs)
	}

	if cfg.Modes["model-b"] != "tiered_expr" {
		t.Errorf("Modes[model-b] = %q, want %q", cfg.Modes["model-b"], "tiered_expr")
	}
	if cfg.Exprs["model-b"] != "p * 10 + c * 50" {
		t.Errorf("Exprs[model-b] = %q, want %q", cfg.Exprs["model-b"], "p * 10 + c * 50")
	}
}

func TestUpdateConfigFromMap_EmptyMapClearsAll(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{
			"model-a": "tiered_expr",
		},
		Exprs: map[string]string{
			"model-a": "p * 5 + c * 25",
		},
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"modes": `{}`,
		"exprs": `{}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if len(cfg.Modes) != 0 {
		t.Errorf("Modes should be empty after updating with {}, got %v", cfg.Modes)
	}
	if len(cfg.Exprs) != 0 {
		t.Errorf("Exprs should be empty after updating with {}, got %v", cfg.Exprs)
	}
}

func TestUpdateConfigFromMap_ScalarFieldsUnchanged(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{"m": "v"},
		Name:  "old",
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"name": "new",
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if cfg.Name != "new" {
		t.Errorf("Name = %q, want %q", cfg.Name, "new")
	}
	// modes was not in configMap, should remain unchanged
	if cfg.Modes["m"] != "v" {
		t.Errorf("Modes should be unchanged, got %v", cfg.Modes)
	}
}

type saveModuleTestA struct {
	Foo string `json:"foo"`
	Bar int    `json:"bar"`
}

type saveModuleTestB struct {
	Baz string `json:"baz"`
}

// SaveModuleToDB 的作用域：只写命名空间自己的键。
//
// 钉住的是 group_ratio_setting.group_ratio 事故的另一半：SaveToDB 会把所有
// 注册模块的内存快照一次性刷库，任何模块的保存动作都会顺手覆盖别的模块。
func TestSaveModuleToDBSavesOnlyItsOwnNamespace(t *testing.T) {
	cm := NewConfigManager()
	cm.Register("module_a", &saveModuleTestA{Foo: "x", Bar: 7})
	cm.Register("module_b", &saveModuleTestB{Baz: "y"})

	var written []string
	err := cm.SaveModuleToDB("module_a", func(key, value string) error {
		written = append(written, key+"="+value)
		return nil
	})
	require.NoError(t, err)

	sort.Strings(written)
	require.Equal(t, []string{"module_a.bar=7", "module_a.foo=x"}, written)
}

// 未注册的命名空间必须报错，不能静默成功——拼错的名字不该表现为「保存成功
// 但什么都没写」。
func TestSaveModuleToDBRejectsUnknownNamespace(t *testing.T) {
	cm := NewConfigManager()

	err := cm.SaveModuleToDB("not_registered", func(key, value string) error {
		t.Fatalf("updateFunc must not be called for an unknown namespace, got %s=%s", key, value)
		return nil
	})
	require.Error(t, err)
}
