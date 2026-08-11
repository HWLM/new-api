// Copyright (C) 2023-2026 QuantumNous
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// For commercial licensing, please contact support@quantumnous.com

package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// requireAdminApikey 校验当前 apikey 属于"管理员用户"，且不是代理身份 apikey。
// 用于同步类白名单接口：调用方必须持有管理员账号自建的 apikey 才能拉全量定价数据。
// 校验通过返回 true；未通过时直接写入错误响应并返回 false，调用方无需再处理。
func requireAdminApikey(c *gin.Context) bool {
	tokenId := c.GetInt("token_id")
	if tokenId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "missing token context",
		})
		return false
	}
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	if token.IsAgentToken {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "代理身份 apikey 不允许访问同步接口",
		})
		return false
	}
	user, err := model.GetUserById(token.UserId, false)
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	if user.Role < common.RoleAdminUser {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "仅管理员创建的 apikey 可调用同步接口",
		})
		return false
	}
	return true
}

// GetModelPricingForSync 返回全量模型定价（用于外部 new-api 站点同步）。
// 返回值形态直接对齐本站 options 表：key 就是 options 表主键，value 是 options 表 value 列
// 的 JSON 字符串。对端拿到后直接遍历写自己的 options 表 / 逐条调 UpdateOption 即可。
//
// 覆盖的 options 表 key：
//   - 独立 key（8）：ModelRatio / CompletionRatio / CacheRatio / CreateCacheRatio /
//     ImageRatio / AudioRatio / AudioCompletionRatio / ModelPrice
//   - 新配置前缀 key（2）：billing_setting.billing_mode / billing_setting.billing_expr
func GetModelPricingForSync(c *gin.Context) {
	if !requireAdminApikey(c) {
		return
	}
	// billing_setting 的两个字段没有独立的 2JSONString helper，走 config.SaveToDB 时是靠反射
	// 序列化的，这里手动 marshal 一次以拿到与 config.SaveToDB 一致的 value 形态。
	billingMode, _ := common.Marshal(billing_setting.GetBillingModeCopy())
	billingExpr, _ := common.Marshal(billing_setting.GetBillingExprCopy())

	common.ApiSuccess(c, gin.H{
		"ModelRatio":                   ratio_setting.ModelRatio2JSONString(),
		"CompletionRatio":              ratio_setting.CompletionRatio2JSONString(),
		"CacheRatio":                   ratio_setting.CacheRatio2JSONString(),
		"CreateCacheRatio":             ratio_setting.CreateCacheRatio2JSONString(),
		"ImageRatio":                   ratio_setting.ImageRatio2JSONString(),
		"AudioRatio":                   ratio_setting.AudioRatio2JSONString(),
		"AudioCompletionRatio":         ratio_setting.AudioCompletionRatio2JSONString(),
		"ModelPrice":                   ratio_setting.ModelPrice2JSONString(),
		"billing_setting.billing_mode": string(billingMode),
		"billing_setting.billing_expr": string(billingExpr),
	})
}

// GetGroupRatioForSync 返回全量分组定价（用于外部 new-api 站点同步）。
// 返回形态与 GetModelPricingForSync 相同：key 是 options 表主键，value 是 options 表 value
// 列的 JSON 字符串。
//
// 覆盖的 options 表 key：
//   - 独立 key（4）：GroupRatio / GroupGroupRatio / UserUsableGroups / TopupGroupRatio
//   - 新配置前缀 key（4）：group_ratio_setting.group_ratio /
//     group_ratio_setting.group_group_ratio /
//     group_ratio_setting.group_special_usable_group /
//     group_ratio_setting.user_group_visible_groups
//
// 说明：老 key 和新前缀 key 是历史遗留的两套并存机制，本站启动加载时先读老 key、再被同名
// 的新前缀 key 覆盖，同步时两套都下发保证对端两种加载路径都拿到一致数据。
func GetGroupRatioForSync(c *gin.Context) {
	if !requireAdminApikey(c) {
		return
	}
	groupSetting := ratio_setting.GetGroupRatioSetting()
	common.ApiSuccess(c, gin.H{
		"GroupRatio":       ratio_setting.GroupRatio2JSONString(),
		"GroupGroupRatio":  ratio_setting.GroupGroupRatio2JSONString(),
		"UserUsableGroups": setting.UserUsableGroups2JSONString(),
		"TopupGroupRatio":  common.TopupGroupRatio2JSONString(),
		"group_ratio_setting.group_ratio":                ratio_setting.GroupRatio2JSONString(),
		"group_ratio_setting.group_group_ratio":          ratio_setting.GroupGroupRatio2JSONString(),
		"group_ratio_setting.group_special_usable_group": groupSetting.GroupSpecialUsableGroup.MarshalJSONString(),
		"group_ratio_setting.user_group_visible_groups":  ratio_setting.UserGroupVisibleGroups2JSONString(),
	})
}

// GetModelTablesForSync returns raw rows grouped by table name for downstream import.
// Channels are intentionally excluded for now.
func GetModelTablesForSync(c *gin.Context) {
	if !requireAdminApikey(c) {
		return
	}

	var vendors []model.Vendor
	if err := model.DB.Unscoped().Order("id ASC").Find(&vendors).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var models []model.Model
	if err := model.DB.Unscoped().Order("id ASC").Find(&models).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var abilities []model.Ability
	if err := model.DB.Unscoped().Order("channel_id ASC, model ASC, " + model.UsersGroupCol() + " ASC").Find(&abilities).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	type vendorSyncRow struct {
		model.Vendor
		DeletedAt *time.Time `json:"deleted_at"`
	}
	vendorRows := make([]vendorSyncRow, len(vendors))
	for i := range vendors {
		vendorRows[i].Vendor = vendors[i]
		if vendors[i].DeletedAt.Valid {
			deletedAt := vendors[i].DeletedAt.Time
			vendorRows[i].DeletedAt = &deletedAt
		}
	}

	type modelSyncRow struct {
		model.Model
		DeletedAt *time.Time `json:"deleted_at"`
	}
	modelRows := make([]modelSyncRow, len(models))
	for i := range models {
		modelRows[i].Model = models[i]
		if models[i].DeletedAt.Valid {
			deletedAt := models[i].DeletedAt.Time
			modelRows[i].DeletedAt = &deletedAt
		}
	}

	common.ApiSuccess(c, gin.H{
		"table_order": []string{"vendors", "models", "abilities"},
		"vendors":     vendorRows,
		"models":      modelRows,
		"abilities":   abilities,
	})
}
