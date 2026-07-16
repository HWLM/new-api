package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserUsableGroupsVisibility(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalVisibleGroups := ratio_setting.UserGroupVisibleGroups2JSONString()
	originalSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(originalVisibleGroups))
		require.NoError(t, types.LoadFromJsonString(ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup, originalSpecialGroups))
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","premium":"Premium"}`))
	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{}`))
	assert.Empty(t, GetUserUsableGroupsForDisplay("vip", common.RoleCommonUser))
	assert.Equal(t, map[string]string{"vip": "用户分组"}, GetUserUsableGroups("vip", common.RoleCommonUser))

	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{"vip":["default"]}`))
	assert.Equal(t, map[string]string{"default": "Default"}, GetUserUsableGroupsForDisplay("vip", common.RoleCommonUser))
	assert.Equal(t, map[string]string{"default": "Default", "vip": "用户分组"}, GetUserUsableGroups("vip", common.RoleCommonUser))

	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{"vip":["vip","premium"]}`))
	assert.Equal(t, map[string]string{"vip": "VIP", "premium": "Premium"}, GetUserUsableGroups("vip", common.RoleCommonUser))

	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{"vip":[]}`))
	assert.Empty(t, GetUserUsableGroupsForDisplay("vip", common.RoleCommonUser))
	assert.Equal(t, map[string]string{"vip": "用户分组"}, GetUserUsableGroups("vip", common.RoleCommonUser))

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","premium":"Premium"}`))
	assert.Equal(t, map[string]string{"vip": "用户分组"}, GetUserUsableGroups("vip", common.RoleCommonUser))

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, types.LoadFromJsonString(
		ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup,
		`{"vip":{"+:premium":"Premium"}}`,
	))
	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{"vip":["premium"]}`))
	assert.Equal(t, map[string]string{"vip": "用户分组"}, GetUserUsableGroups("vip", common.RoleCommonUser))

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","premium":"Premium"}`))
	require.NoError(t, types.LoadFromJsonString(ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup, `{}`))
	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{"vip":[]}`))
	assert.Equal(t, map[string]string{"vip": "用户分组"}, GetUserUsableGroups("vip", common.RoleAdminUser))
	assert.Equal(t, map[string]string{"default": "Default", "vip": "VIP", "premium": "Premium"}, GetUserUsableGroups("vip", common.RoleRootUser))
}

func TestResolveTokenGroupsForUser(t *testing.T) {
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalVisibleGroups := ratio_setting.UserGroupVisibleGroups2JSONString()
	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalAutoGroups := setting.AutoGroups2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(originalVisibleGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"premium":"Premium","backup":"Backup"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":10,"premium":1,"backup":2}`))
	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{"vip":["premium","backup"]}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["missing","backup"]`))

	resolved, ok := ResolveTokenGroupsForUser(
		"vip",
		[]string{"vip"},
		common.RoleCommonUser,
	)
	require.True(t, ok)
	assert.Equal(t, []string{"backup", "premium"}, resolved)

	resolved, ok = ResolveTokenGroupsForUser(
		"vip",
		[]string{"vip", "backup", "premium"},
		common.RoleCommonUser,
	)
	require.True(t, ok)
	assert.Equal(t, []string{"backup", "premium"}, resolved)

	resolved, ok = ResolveTokenGroupsForUser(
		"vip",
		[]string{"backup"},
		common.RoleCommonUser,
	)
	require.True(t, ok)
	assert.Equal(t, []string{"backup"}, resolved)

	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{"vip":[]}`))
	resolved, ok = ResolveTokenGroupsForUser(
		"vip",
		[]string{"vip"},
		common.RoleCommonUser,
	)
	assert.False(t, ok)
	assert.Nil(t, resolved)

	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{"vip":["missing"]}`))
	resolved, ok = ResolveTokenGroupsForUser(
		"vip",
		[]string{"vip"},
		common.RoleCommonUser,
	)
	assert.False(t, ok)
	assert.Nil(t, resolved)

	resolved, ok = ResolveTokenGroupsForUser(
		"vip",
		[]string{"backup"},
		common.RoleCommonUser,
	)
	require.True(t, ok)
	assert.Equal(t, []string{"backup"}, resolved)

	resolved, ok = ResolveTokenGroupsForUser(
		"vip",
		[]string{"vip"},
		common.RoleRootUser,
	)
	require.True(t, ok)
	assert.Equal(t, []string{"vip"}, resolved)
}
