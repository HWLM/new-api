package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	displayUsableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	for s, f := range ratio_setting.GetGroupRatioCopy() {
		groupRatio[s] = f
	}
	var group string
	userRole := common.RoleGuestUser
	topupGroupRatio := 1.0
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			group = user.Group
			userRole = user.Role
			if ratio := common.GetTopupGroupRatio(group); ratio > 0 {
				topupGroupRatio = ratio
			}
		}
	}

	usableGroup = service.GetUserUsableGroups(group, userRole)
	displayUsableGroup = service.GetUserUsableGroupsForDisplay(group, userRole)
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	// check groupRatio contains displayUsableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := displayUsableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}
	if group != "" {
		for g := range displayUsableGroup {
			ratio, ok := ratio_setting.GetGroupGroupRatio(group, g)
			if ok {
				groupRatio[g] = ratio
			}
		}
	}

	var pricingData interface{} = pricing
	if active, rate := settlementUSDRate(c); active {
		// 按量计费的 输入/输出/缓存 价格由前端用 model_ratio 派生（price = model_ratio×2×group_ratio×相对倍率），
		// 故换算 model_ratio 即让三者一并 ÷ 汇率；completion_ratio/cache_ratio 是相对倍率不能动。
		// 按次计费的固定价由 model_price 派生，一并换算。group_ratio 是展示用倍率，保持不变。
		pricingData = convertStructsForSettlement(pricing, rate, "model_ratio", "model_price", "official_model_price")
	}

	c.JSON(200, gin.H{
		"success":                         true,
		"data":                            pricingData,
		"vendors":                         model.GetVendors(),
		"group_ratio":                     groupRatio,
		"usable_group":                    displayUsableGroup,
		"supported_endpoint":              model.GetSupportedEndpointMap(),
		"auto_groups":                     service.GetUserAutoGroup(group, userRole),
		"topup_group_ratio":               topupGroupRatio,
		"pricing_discount_column_enabled": common.PricingDiscountColumnEnabled,
		"pricing_version":                 "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
