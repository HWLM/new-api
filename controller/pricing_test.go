package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pricingResponse struct {
	Success bool `json:"success"`
	Data    []struct {
		ModelName   string   `json:"model_name"`
		EnableGroup []string `json:"enable_groups"`
	} `json:"data"`
}

func decodePricingResponse(t *testing.T, recorder *httptest.ResponseRecorder) pricingResponse {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload pricingResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	return payload
}

func TestGetPricingRootUserSeesAllModels(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	model.InvalidatePricingCache()

	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalVisibleGroups := ratio_setting.UserGroupVisibleGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(originalVisibleGroups))
		model.InvalidatePricingCache()
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, ratio_setting.UpdateUserGroupVisibleGroupsByJSONString(`{}`))

	require.NoError(t, db.Create(&model.User{
		Id:       9001,
		Username: "root-pricing-user",
		Password: "password",
		Group:    "default",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     801,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "channel-801",
		Status: common.ChannelStatusEnabled,
		Name:   "channel-801",
		Group:  "default",
		Models: "root-visible-model,root-hidden-model",
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "root-visible-model", ChannelId: 801, Enabled: true},
		{Group: "premium", Model: "root-hidden-model", ChannelId: 801, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	ctx.Set("id", 9001)

	GetPricing(ctx)

	payload := decodePricingResponse(t, recorder)
	names := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		names[item.ModelName] = struct{}{}
	}
	require.Contains(t, names, "root-visible-model")
	require.Contains(t, names, "root-hidden-model")
}
