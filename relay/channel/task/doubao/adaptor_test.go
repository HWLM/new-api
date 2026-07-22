package doubao

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRequestBodyReconstructsFromMetadata 确认：当 middleware 把整个原始 doubao 风格 body
// 放进 TaskSubmitReq.Metadata 时，doubao adapter 会把 content 数组（含 image/video/audio 项）、
// tools、resolution、ratio、watermark、generate_audio 等字段全部还原到上游请求，
// prompt 会作为一条 text 附加到 content 末尾。
func TestBuildRequestBodyReconstructsFromMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-filter-off",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://example.com",
			ApiKey:            "test-key",
			UpstreamModelName: "doubao-seedance-2-0-filter-off",
		},
	}
	// 模拟 middleware 归一化后的结果
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-filter-off",
		Prompt: "animate the subject",
		Images: []string{"https://example.com/ref.jpg"},
		Metadata: map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "animate the subject"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/ref.jpg"}, "role": "reference_image"},
				map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://example.com/ref.mp4"}, "role": "reference_video"},
				map[string]any{"type": "audio_url", "audio_url": map[string]any{"url": "https://example.com/ref.mp3"}, "role": "reference_audio"},
			},
			"tools":          []any{map[string]any{"type": "web_search"}},
			"duration":       float64(5),
			"ratio":          "16:9",
			"resolution":     "720p",
			"generate_audio": true,
			"watermark":      false,
		},
	})

	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	reader, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	// 用 map 反解，避免 adapter 内部 requestPayload 私有类型泄漏到测试
	var out map[string]any
	require.NoError(t, common.Unmarshal(data, &out))

	assert.Equal(t, "doubao-seedance-2-0-filter-off", out["model"])
	assert.Equal(t, "16:9", out["ratio"])
	assert.Equal(t, "720p", out["resolution"])
	assert.Equal(t, true, out["generate_audio"])
	assert.Equal(t, false, out["watermark"])
	assert.EqualValues(t, 5, out["duration"])

	tools, ok := out["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	assert.Equal(t, "web_search", tools[0].(map[string]any)["type"])

	// content: metadata 里的 image/video/audio 原样保留；text 项被 adapter 过滤后重新用 prompt 追加
	content, ok := out["content"].([]any)
	require.True(t, ok)
	types := make([]string, 0, len(content))
	for _, item := range content {
		types = append(types, item.(map[string]any)["type"].(string))
	}
	assert.Equal(t, []string{"image_url", "video_url", "audio_url", "text"}, types)

	last := content[len(content)-1].(map[string]any)
	assert.Equal(t, "text", last["type"])
	assert.Equal(t, "animate the subject", last["text"])

	video := content[1].(map[string]any)
	assert.Equal(t, "https://example.com/ref.mp4", video["video_url"].(map[string]any)["url"])
	audio := content[2].(map[string]any)
	assert.Equal(t, "https://example.com/ref.mp3", audio["audio_url"].(map[string]any)["url"])
}

func TestBuildRequestBodyUsesTopLevelDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-filter-off",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://example.com",
			ApiKey:            "test-key",
			UpstreamModelName: "doubao-seedance-2-0-filter-off",
		},
	}
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-filter-off",
		Prompt:   "animate the subject",
		Duration: 5,
		Metadata: map[string]any{
			"resolution": "720p",
		},
	})

	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	reader, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, common.Unmarshal(data, &out))
	assert.EqualValues(t, 5, out["duration"])
}

func TestDoResponseUsesSeedanceV3PublicTaskOnUnifiedRoute(t *testing.T) {
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

func TestConvertToSeedanceV3VideoUsesLocalTaskID(t *testing.T) {
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
