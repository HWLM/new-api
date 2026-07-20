package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedanceV3RequestConvertNormalizesDistributorRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{
		"model":"dreamina-seedance-2-0-hc",
		"content":[
			{"type":"image_url","image_url":{"url":"https://example.com/input.jpg"},"role":"reference_image"},
			{"type":"text","text":" make the subject dance "}
		],
		"duration":4,
		"generate_audio":false,
		"watermark":false
	}`
	request := httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	SeedanceV3RequestConvert()(context)

	require.False(t, context.IsAborted())
	var normalized relaycommon.TaskSubmitReq
	require.NoError(t, common.UnmarshalBodyReusable(context, &normalized))
	assert.Equal(t, "dreamina-seedance-2-0-hc", normalized.Model)
	assert.Equal(t, "make the subject dance", normalized.Prompt)
	assert.Equal(t, []string{"https://example.com/input.jpg"}, normalized.Images)
	assert.Equal(t, 4, normalized.Duration)

	stored, exists := context.Get(string(constant.ContextKeySeedanceV3Request))
	require.True(t, exists)
	original, ok := stored.(dto.SeedanceV3VideoRequest)
	require.True(t, ok)
	require.NotNil(t, original.GenerateAudio)
	require.NotNil(t, original.Watermark)
	assert.False(t, *original.GenerateAudio)
	assert.False(t, *original.Watermark)

	modelRequest, shouldSelectChannel, err := getModelRequest(context)
	require.NoError(t, err)
	require.NotNil(t, modelRequest)
	assert.True(t, shouldSelectChannel)
	assert.Equal(t, dto.SeedanceV3ModelName, modelRequest.Model)
	assert.Equal(t, relayconstant.RelayModeVideoSubmit, context.GetInt("relay_mode"))
}

func TestSeedanceV3RequestConvertPassesThroughInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(`{"model":`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	SeedanceV3RequestConvert()(context)

	// 不做 SeedanceV3 转换，把请求原样交给下游（distributor 会按各自规则处理）
	assert.False(t, context.IsAborted())
	_, exists := context.Get(string(constant.ContextKeySeedanceV3Request))
	assert.False(t, exists)
}

func TestSeedanceV3RequestConvertPassesThroughNonSeedanceModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{
		"model":"doubao-seedance-2-0-filter-off",
		"content":[
			{"type":"text","text":"animate"},
			{"type":"image_url","image_url":{"url":"https://example.com/ref.jpg"},"role":"reference_image"},
			{"type":"video_url","video_url":{"url":"https://example.com/ref.mp4"},"role":"reference_video"},
			{"type":"audio_url","audio_url":{"url":"https://example.com/ref.mp3"},"role":"reference_audio"}
		],
		"tools":[{"type":"web_search"}],
		"duration":5,
		"ratio":"16:9",
		"resolution":"720p",
		"generate_audio":true,
		"watermark":false
	}`
	request := httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	SeedanceV3RequestConvert()(context)

	// 非 SeedanceV3：不 abort、不写 SeedanceV3 上下文键，走 doubao adapter
	require.False(t, context.IsAborted())
	_, exists := context.Get(string(constant.ContextKeySeedanceV3Request))
	assert.False(t, exists)

	// distributor 能从改写后的 body 读到正确的 model
	modelRequest, shouldSelectChannel, err := getModelRequest(context)
	require.NoError(t, err)
	require.NotNil(t, modelRequest)
	assert.True(t, shouldSelectChannel)
	assert.Equal(t, "doubao-seedance-2-0-filter-off", modelRequest.Model)

	// 归一化后的 TaskSubmitReq：prompt/images/duration 摘出，其他字段（tools/ratio/resolution/video_url/audio_url/generate_audio/watermark）
	// 都完整保留在 Metadata 里供 doubao adapter overlay 使用
	var normalized relaycommon.TaskSubmitReq
	require.NoError(t, common.UnmarshalBodyReusable(context, &normalized))
	assert.Equal(t, "doubao-seedance-2-0-filter-off", normalized.Model)
	assert.Equal(t, "animate", normalized.Prompt)
	assert.Equal(t, []string{"https://example.com/ref.jpg"}, normalized.Images)
	assert.Equal(t, 5, normalized.Duration)

	require.NotNil(t, normalized.Metadata)
	assert.Equal(t, "16:9", normalized.Metadata["ratio"])
	assert.Equal(t, "720p", normalized.Metadata["resolution"])
	assert.Equal(t, true, normalized.Metadata["generate_audio"])
	assert.Equal(t, false, normalized.Metadata["watermark"])
	tools, ok := normalized.Metadata["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	toolMap, ok := tools[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "web_search", toolMap["type"])

	contentAny, ok := normalized.Metadata["content"].([]any)
	require.True(t, ok)
	require.Len(t, contentAny, 4)
	// video_url / audio_url 条目原样保留在 metadata.content 里
	videoItem, _ := contentAny[2].(map[string]any)
	require.NotNil(t, videoItem)
	assert.Equal(t, "video_url", videoItem["type"])
	audioItem, _ := contentAny[3].(map[string]any)
	require.NotNil(t, audioItem)
	assert.Equal(t, "audio_url", audioItem["type"])
}

func TestSeedanceV3RequestConvertPreservesDoubaoAutomaticDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(`{
		"model":"doubao-seedance-2-0-filter-off",
		"content":[{"type":"text","text":"animate"}],
		"duration":-1
	}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	SeedanceV3RequestConvert()(context)

	require.False(t, context.IsAborted())
	var normalized relaycommon.TaskSubmitReq
	require.NoError(t, common.UnmarshalBodyReusable(context, &normalized))
	assert.Zero(t, normalized.Duration)
	require.NotNil(t, normalized.Metadata)
	assert.EqualValues(t, -1, normalized.Metadata["duration"])
}

func TestSeedanceV3RequestConvertRejectsOversizedDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(`{
		"model":"doubao-seedance-2-0-filter-off",
		"content":[{"type":"text","text":"animate"}],
		"duration":18446744073709551615
	}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	SeedanceV3RequestConvert()(context)

	assert.True(t, context.IsAborted())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}
