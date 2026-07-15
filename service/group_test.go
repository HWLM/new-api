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
