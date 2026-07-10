package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type getOptionsResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    []model.Option `json:"data"`
}

func TestGetOptionsImageResultObjectStoreAdminOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawObjectStore := `{"accessKey":"ak","secretKey":"sk","bucketName":"bucket","region":"us-east-1","publicUrl":"https://cdn.example.com"}`
	restore := setOptionMapForOptionTest(map[string]string{
		"ImageResultObjectStore":        rawObjectStore,
		"ImageResultObjectStoreEnabled": "true",
		"SystemName":                    "new-api",
	})
	defer restore()

	commonUserOptions := getOptionsByRole(t, common.RoleCommonUser)
	assert.NotContains(t, commonUserOptions, "ImageResultObjectStore")
	assert.NotContains(t, commonUserOptions, "ImageResultObjectStoreEnabled")
	assert.Equal(t, "new-api", commonUserOptions["SystemName"])

	adminOptions := getOptionsByRole(t, common.RoleAdminUser)
	assert.Equal(t, rawObjectStore, adminOptions["ImageResultObjectStore"])
	assert.Equal(t, "true", adminOptions["ImageResultObjectStoreEnabled"])
}

func setOptionMapForOptionTest(values map[string]string) func() {
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = values
	common.OptionMapRWMutex.Unlock()
	return func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	}
}

func getOptionsByRole(t *testing.T, role int) map[string]string {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	ctx.Set("role", role)

	GetOptions(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response getOptionsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)

	options := make(map[string]string, len(response.Data))
	for _, option := range response.Data {
		options[option.Key] = option.Value
	}
	return options
}
