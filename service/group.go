package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// getConfiguredUsableGroups returns the groups that a user group is allowed to
// see and use. An explicit visibility mapping is authoritative; without one,
// no groups are visible to non-root users.
func getConfiguredUsableGroups(userGroup string, userRole int) map[string]string {
	selectableGroups := setting.GetUserUsableGroupsCopy()
	groupsCopy := make(map[string]string, len(selectableGroups))
	for groupName, desc := range selectableGroups {
		groupsCopy[groupName] = desc
	}
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if strings.HasPrefix(specialGroup, "-:") {
					// 移除分组
					groupToRemove := strings.TrimPrefix(specialGroup, "-:")
					delete(groupsCopy, groupToRemove)
				} else if strings.HasPrefix(specialGroup, "+:") {
					// 添加分组
					groupToAdd := strings.TrimPrefix(specialGroup, "+:")
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
	}
	if userRole >= common.RoleRootUser {
		return groupsCopy
	}

	visibleGroupNames, configured := ratio_setting.GetGroupRatioSetting().UserGroupVisibleGroups.Get(userGroup)
	if !configured {
		return map[string]string{}
	}
	visibleGroups := make(map[string]string, len(visibleGroupNames))
	for _, groupName := range visibleGroupNames {
		_, selectable := selectableGroups[groupName]
		if desc, ok := groupsCopy[groupName]; ok && selectable {
			visibleGroups[groupName] = desc
		}
	}
	return visibleGroups
}

func GetUserUsableGroupsForDisplay(userGroup string, userRole int) map[string]string {
	return getConfiguredUsableGroups(userGroup, userRole)
}

func GetUserUsableGroups(userGroup string, userRole int) map[string]string {
	groups := getConfiguredUsableGroups(userGroup, userRole)
	if userGroup != "" {
		if _, ok := groups[userGroup]; !ok {
			groups[userGroup] = "用户分组"
		}
	}
	return groups
}

func GroupInUserUsableGroups(userGroup, groupName string, userRole int) bool {
	_, ok := GetUserUsableGroups(userGroup, userRole)[groupName]
	return ok
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string, userRole int) []string {
	groups := GetUserUsableGroups(userGroup, userRole)
	autoGroups := make([]string, 0)
	for _, group := range setting.GetAutoGroups() {
		if _, ok := groups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
