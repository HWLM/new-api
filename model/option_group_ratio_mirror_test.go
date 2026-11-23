package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 单测进程里没有跑 InitOptionMap，updateOptionMap 会往 common.OptionMap
// 写键，先备好一张空表，结束后还原，避免影响同包其它用例。
func useMirrorTestOptionMap(t *testing.T) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
}

// 残留的 group_ratio_setting.group_ratio 镜像行不得再覆盖旧键 GroupRatio。
//
// 事故形态（fix/group-ratio-mirror 之前）：
//   - 蒸馏设置保存时 config.SaveToDB 把内存快照写成
//     `group_ratio_setting.group_ratio` 一行；
//   - 之后有人在分组定价页新增分组（只写 GroupRatio 旧键）；
//   - AllOption() 没有 ORDER BY，镜像行只要排在旧键之后加载，
//     RWMap.UnmarshalJSON 就会用旧快照全量替换内存表，新分组在
//     一个 SYNC_FREQUENCY 周期内消失。
//
// 修复后 GroupRatioSetting 不再注册 group_ratio 字段，残留行在加载时
// 因找不到对应字段而被忽略。本用例钉住这一行为：镜像行存在时反复同步，
// 新加的分组不掉。
func TestStaleGroupRatioMirrorRowDoesNotDropNewGroups(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	useMirrorTestOptionMap(t)

	previousGroups := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	// 用户在分组定价页新增分组（走旧键）——先写旧键，镜像行随后才被写出，
	// 保证镜像行 rowid 更大、在 AllOption() 的插入序里排在旧键之后加载。
	require.NoError(t, UpdateOption("GroupRatio", `{"default":1,"new_group":2.5}`))
	require.True(t, ratio_setting.ContainsGroupRatio("new_group"))

	// 残留镜像行：模拟随后一次 SaveToDB 写出的旧快照，不含 new_group。
	// 修复前它排在旧键之后加载，会把内存表整体换回这份快照（假绿的成因
	// 就是顺序反了：镜像先行、旧键后灌，旧键永远赢）。
	require.NoError(t, db.Create(&Option{
		Key:   "group_ratio_setting.group_ratio",
		Value: `{"default":1}`,
	}).Error)

	// 模拟多个 SYNC_FREQUENCY 周期的全量加载。
	for round := 1; round <= 3; round++ {
		loadOptionsFromDatabase()
		assert.True(t, ratio_setting.ContainsGroupRatio("new_group"),
			"new group dropped on sync round %d", round)
		assert.InDelta(t, 2.5, ratio_setting.GetGroupRatio("new_group"), 1e-9,
			"new group ratio changed on sync round %d", round)
	}
}

// group_group_ratio 同理：残留行不得覆盖内存表。
func TestStaleGroupGroupRatioMirrorRowIsInert(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	useMirrorTestOptionMap(t)

	previousRates := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousRates))
	})
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))

	// 同前：先写旧键，镜像行在后，保证它在插入序里后加载。
	require.NoError(t, UpdateOption("GroupGroupRatio", `{"svip":{"fresh_group":0.8}}`))

	require.NoError(t, db.Create(&Option{
		Key:   "group_ratio_setting.group_group_ratio",
		Value: `{"vip":{"stale_group":0.5}}`,
	}).Error)

	for round := 1; round <= 3; round++ {
		loadOptionsFromDatabase()
		ratio, ok := ratio_setting.GetGroupGroupRatio("svip", "fresh_group")
		assert.True(t, ok, fmt.Sprintf("fresh entry dropped on sync round %d", round))
		assert.InDelta(t, 0.8, ratio, 1e-9)
		if _, staleOK := ratio_setting.GetGroupGroupRatio("vip", "stale_group"); staleOK {
			t.Fatalf("stale mirror row resurrected on sync round %d", round)
		}
	}
}
