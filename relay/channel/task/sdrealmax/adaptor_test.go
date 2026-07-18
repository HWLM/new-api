package sdrealmax

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyPreservesV3Fields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	generateAudio := false
	watermark := false
	duration := 4
	context.Set(string(constant.ContextKeySeedanceV3Request), dto.SeedanceV3VideoRequest{
		Model: ModelName,
		Content: []dto.SeedanceV3ContentItem{
			{Type: "text", Text: "animate"},
			{Type: "image_url", ImageURL: &dto.SeedanceV3MediaURL{URL: "asset://asset-1"}, Role: "reference_image"},
		},
		Duration:      &duration,
		Resolution:    "720p",
		Ratio:         "1:1",
		GenerateAudio: &generateAudio,
		Watermark:     &watermark,
	})

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "mapped-model"}}
	reader, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var got dto.SeedanceV3VideoRequest
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "mapped-model", got.Model)
	assert.Equal(t, 4, *got.Duration)
	assert.Equal(t, "720p", got.Resolution)
	assert.Equal(t, "1:1", got.Ratio)
	require.NotNil(t, got.GenerateAudio)
	require.NotNil(t, got.Watermark)
	assert.False(t, *got.GenerateAudio)
	assert.False(t, *got.Watermark)
	assert.Equal(t, "asset://asset-1", got.Content[1].ImageURL.URL)
}

func TestValidateRequestBoundsAndMediaRules(t *testing.T) {
	valid := func() dto.SeedanceV3VideoRequest {
		duration := 4
		return dto.SeedanceV3VideoRequest{
			Model:      ModelName,
			Duration:   &duration,
			Resolution: "480p",
			Ratio:      "16:9",
			Content: []dto.SeedanceV3ContentItem{
				{Type: "text", Text: "animate"},
				{Type: "image_url", ImageURL: &dto.SeedanceV3MediaURL{URL: "https://example.com/image.jpg"}},
			},
		}
	}

	tests := []struct {
		name   string
		mutate func(*dto.SeedanceV3VideoRequest)
	}{
		{name: "duration below minimum", mutate: func(r *dto.SeedanceV3VideoRequest) { *r.Duration = 3 }},
		{name: "duration above maximum", mutate: func(r *dto.SeedanceV3VideoRequest) { *r.Duration = 16 }},
		{name: "unsupported resolution", mutate: func(r *dto.SeedanceV3VideoRequest) { r.Resolution = "4k" }},
		{name: "non-public video URL", mutate: func(r *dto.SeedanceV3VideoRequest) {
			r.Content[1] = dto.SeedanceV3ContentItem{Type: "video_url", VideoURL: &dto.SeedanceV3MediaURL{URL: "data:video/mp4;base64,AAAA"}}
		}},
		{name: "image data URL", mutate: func(r *dto.SeedanceV3VideoRequest) {
			r.Content[1].ImageURL.URL = "data:image/png;base64,AAAA"
		}},
		{name: "empty asset URL", mutate: func(r *dto.SeedanceV3VideoRequest) {
			r.Content[1].ImageURL.URL = "asset://"
		}},
	}

	require.NoError(t, validateRequest(valid()))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid()
			tt.mutate(&request)
			require.Error(t, validateRequest(request))
		})
	}
}

// TestDoRequestForwardsImageURLsWithoutUploading 覆盖“网关不再自动上传素材”的核心不变式：
// 无论 content 里传的是公网 HTTPS URL 还是 asset://，DoRequest 都应把 body 原样发给
// /v1/video/generate；SD Real Max 素材接口 /v1/sd/assets 绝不能被调用。
func TestDoRequestForwardsImageURLsWithoutUploading(t *testing.T) {
	service.InitHttpClient()
	var assetCalls, videoCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sd/assets":
			assetCalls.Add(1)
			http.Error(w, "asset upload must not be called", http.StatusInternalServerError)
		case "/v1/video/generate":
			videoCalls.Add(1)
			assert.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			// 两张图 URL 都要按原样透传，不做任何 asset:// 替换
			assert.Contains(t, string(body), "https://example.com/plain.jpg")
			assert.Contains(t, string(body), "asset://already-uploaded")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"task":{"id":"mvt-1","status":"pending","outputs":[],"error":null}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	context.Set(string(constant.ContextKeySeedanceV3Request), dto.SeedanceV3VideoRequest{Model: ModelName})
	info := &relaycommon.RelayInfo{
		OriginModelName: ModelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    server.URL,
			ApiKey:            "upstream-key",
			UpstreamModelName: ModelName,
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	body := `{"model":"dreamina-seedance-2-0-hc","content":[` +
		`{"type":"text","text":"animate"},` +
		`{"type":"image_url","image_url":{"url":"https://example.com/plain.jpg"},"role":"reference_image"},` +
		`{"type":"image_url","image_url":{"url":"asset://already-uploaded"},"role":"reference_image"}` +
		`]}`

	response, err := adaptor.DoRequest(context, info, strings.NewReader(body))
	require.NoError(t, err)
	require.NotNil(t, response)
	_ = response.Body.Close()

	assert.Equal(t, int32(0), assetCalls.Load(), "asset upload must never fire from the adapter")
	assert.Equal(t, int32(1), videoCalls.Load())
}

func TestDoRequestDelegatesToDoubaoForOtherModels(t *testing.T) {
	service.InitHttpClient()
	var assetCalls, videoV3Calls, tasksCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sd/assets", "/v3/open/CreateAsset", "/v3/open/GetAsset":
			assetCalls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		case "/v1/video/generate":
			videoV3Calls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		case "/api/v3/contents/generations/tasks":
			tasksCalls.Add(1)
			assert.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
			// 非 hc 请求应把 image URL 原样透传，不做 asset:// 替换
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), "https://example.com/input.jpg")
			assert.NotContains(t, string(body), "asset://")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cgt-doubao-task"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	otherModel := "doubao-seedance-2-0-filter-off"
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	// 非 SeedanceV3 分支：不写 ContextKeySeedanceV3Request，仅靠 model 名进入 doubao 委托
	info := &relaycommon.RelayInfo{
		OriginModelName: otherModel,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    server.URL,
			ApiKey:            "upstream-key",
			UpstreamModelName: otherModel,
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	requestBody, err := common.Marshal(dto.SeedanceV3VideoRequest{
		Model: otherModel,
		Content: []dto.SeedanceV3ContentItem{
			{Type: "text", Text: "animate"},
			{Type: "image_url", ImageURL: &dto.SeedanceV3MediaURL{URL: "https://example.com/input.jpg"}},
		},
	})
	require.NoError(t, err)

	response, err := adaptor.DoRequest(context, info, strings.NewReader(string(requestBody)))
	require.NoError(t, err)
	require.NotNil(t, response)
	_ = response.Body.Close()

	assert.Equal(t, int32(0), assetCalls.Load(), "asset endpoints must not fire from the adapter")
	assert.Equal(t, int32(0), videoV3Calls.Load(), "SeedanceV3 /v1/video/generate must not be hit for non-SeedanceV3 models")
	assert.Equal(t, int32(1), tasksCalls.Load(), "doubao-native /api/v3/contents/generations/tasks should be hit exactly once")
}

func TestBuildRequestURLBranchesByModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://sd.example.com",
			ApiKey:         "k",
		},
	})

	hcInfo := &relaycommon.RelayInfo{
		OriginModelName: ModelName,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://sd.example.com", UpstreamModelName: ModelName},
	}
	url, err := adaptor.BuildRequestURL(hcInfo)
	require.NoError(t, err)
	assert.Equal(t, "https://sd.example.com/v1/video/generate", url)

	otherInfo := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-filter-off",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://sd.example.com", UpstreamModelName: "doubao-seedance-2-0-filter-off"},
	}
	url, err = adaptor.BuildRequestURL(otherInfo)
	require.NoError(t, err)
	assert.Equal(t, "https://sd.example.com/api/v3/contents/generations/tasks", url)
}

// TestBuildRequestBodyDoubaoDoesNotUploadAssets 覆盖非 hc 分支：BuildRequestBody 应直接
// 委托给 doubao 的 metadata overlay，不再触发任何素材上传（就算客户端不小心传了 contains_face）。
func TestBuildRequestBodyDoubaoDoesNotUploadAssets(t *testing.T) {
	service.InitHttpClient()
	var assetCalls, getAssetCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/open/CreateAsset":
			assetCalls.Add(1)
			http.Error(w, "asset upload must not be called", http.StatusInternalServerError)
		case "/v3/open/GetAsset":
			getAssetCalls.Add(1)
			http.Error(w, "asset polling must not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)

	otherModel := "doubao-seedance-2-0-filter-off"
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  otherModel,
		Prompt: "animate",
		Images: []string{"https://example.com/plain.jpg", "https://example.com/face.jpg"},
		Metadata: map[string]any{
			"model": otherModel,
			"content": []any{
				map[string]any{"type": "text", "text": "animate"},
				map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": "https://example.com/plain.jpg"},
					"role":      "reference_image",
				},
				map[string]any{
					// 客户端可能仍带这个字段，但网关不再关心它，透传由 doubao adapter 处理
					"type":          "image_url",
					"image_url":     map[string]any{"url": "https://example.com/face.jpg"},
					"role":          "reference_image",
					"contains_face": true,
				},
			},
		},
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: otherModel,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    server.URL,
			ApiKey:            "upstream-key",
			UpstreamModelName: otherModel,
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	reader, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, int32(0), assetCalls.Load(), "adapter must not call CreateAsset")
	assert.Equal(t, int32(0), getAssetCalls.Load(), "adapter must not poll GetAsset")

	// 两张图的原始 URL 都应保留下来
	assert.Contains(t, string(data), "https://example.com/plain.jpg")
	assert.Contains(t, string(data), "https://example.com/face.jpg")
	assert.NotContains(t, string(data), "asset://")
}

func TestDoResponseReturnsPublicTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"task":{"id":"mvt-upstream-secret","status":"pending","model":"dreamina-seedance-2-0-hc","outputs":[],"error":null}
		}`)),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		OriginModelName: ModelName,
	}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(context, response, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "mvt-upstream-secret", upstreamID)
	assert.NotContains(t, recorder.Body.String(), "mvt-upstream-secret")
	var publicTask dto.SeedanceV3PublicTask
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &publicTask))
	assert.Equal(t, "task_public", publicTask.ID)
	assert.Equal(t, "queued", publicTask.Status)
}

func TestDoResponseReturnsPublicTaskIDForDoubaoModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"doubao-upstream-secret"}`)),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		OriginModelName: dto.SeedanceV3DoubaoFilterOffModel,
	}

	upstreamID, responseBody, taskErr := (&TaskAdaptor{}).DoResponse(context, response, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "doubao-upstream-secret", upstreamID)
	assert.Contains(t, string(responseBody), "doubao-upstream-secret")
	assert.NotContains(t, recorder.Body.String(), "doubao-upstream-secret")
	var publicTask dto.SeedanceV3PublicTask
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &publicTask))
	assert.Equal(t, "task_public", publicTask.ID)
	assert.Equal(t, "queued", publicTask.Status)
}

func TestParseTaskResultMapsCompletionAndUsage(t *testing.T) {
	body := []byte(`{
		"task":{
			"id":"mvt-1",
			"status":"completed",
			"outputs":["https://example.com/video.mp4"],
			"usage":{"completion_tokens":40594,"total_tokens":40594},
			"error":null
		}
	}`)

	result, err := (&TaskAdaptor{}).ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, result.Status)
	assert.Equal(t, "https://example.com/video.mp4", result.Url)
	assert.Equal(t, 40594, result.CompletionTokens)
	assert.Equal(t, 40594, result.TotalTokens)
}

func TestConvertToSeedanceV3VideoUsesPublicTaskData(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		CreatedAt: 1784217600,
		UpdatedAt: 1784218200,
		Properties: model.Properties{
			OriginModelName: ModelName,
		},
		PrivateData: model.TaskPrivateData{ResultURL: "https://example.com/video.mp4"},
		Data: []byte(`{
			"task":{
				"id":"mvt-upstream-secret",
				"status":"completed",
				"outputs":["https://example.com/video.mp4"],
				"usage":{"completion_tokens":40594,"total_tokens":40594},
				"error":null
			}
		}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToSeedanceV3Video(task)

	require.NoError(t, err)
	assert.NotContains(t, string(data), "mvt-upstream-secret")
	var response dto.SeedanceV3PublicTask
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "task_public", response.ID)
	assert.Equal(t, "succeeded", response.Status)
	assert.Equal(t, "https://example.com/video.mp4", response.Content.VideoURL)
	require.NotNil(t, response.Usage)
	assert.Equal(t, 40594, response.Usage.TotalTokens)
}

func TestConvertToSeedanceV3VideoNormalizesDoubaoTask(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		CreatedAt: 1784217600,
		UpdatedAt: 1784218200,
		Properties: model.Properties{
			OriginModelName: dto.SeedanceV3DoubaoFilterOffModel,
		},
		PrivateData: model.TaskPrivateData{ResultURL: "https://example.com/video.mp4"},
		Data: []byte(`{
			"id":"doubao-upstream-secret",
			"status":"succeeded",
			"content":{"video_url":"https://example.com/video.mp4"},
			"usage":{"completion_tokens":40594,"total_tokens":40594}
		}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToSeedanceV3Video(task)

	require.NoError(t, err)
	assert.NotContains(t, string(data), "doubao-upstream-secret")
	var response dto.SeedanceV3PublicTask
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "task_public", response.ID)
	assert.Equal(t, "succeeded", response.Status)
	assert.Equal(t, "https://example.com/video.mp4", response.Content.VideoURL)
	require.NotNil(t, response.Usage)
	assert.Equal(t, 40594, response.Usage.TotalTokens)
}

func TestFetchTaskUsesDocumentedEndpointAndBearerKey(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/video/tasks/mvt-1", r.URL.Path)
		assert.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task":{"id":"mvt-1","status":"processing","outputs":[],"error":null}}`))
	}))
	t.Cleanup(server.Close)

	response, err := (&TaskAdaptor{}).FetchTask(server.URL, "upstream-key", map[string]any{"task_id": "mvt-1"}, "")

	require.NoError(t, err)
	require.NotNil(t, response)
	_ = response.Body.Close()
}
