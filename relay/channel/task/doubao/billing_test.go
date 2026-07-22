package doubao

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTaskRequestContext 复用 middleware 归一化后的 TaskSubmitReq 语义写入 context。
func setupTaskRequestContext(t *testing.T, metadata map[string]any) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    "doubao-seedance-2-0-filter-off",
		Prompt:   "animate",
		Metadata: metadata,
	})
	return c
}

// TestEstimateBillingAddsVideoInputAndDurationEstimate 覆盖三档 resolution 命中价格表 +
// duration_estimate 的正常路径。
func TestEstimateBillingAddsVideoInputAndDurationEstimate(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "doubao-seedance-2-0-filter-off"}
	c := setupTaskRequestContext(t, map[string]any{
		"resolution": "1080p",
		"duration":   float64(10),
		"content": []any{
			map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://example.com/ref.mp4"}},
		},
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)

	// 1080p + hasVideo → 31/46 ≈ 0.674
	require.Contains(t, ratios, "video_input")
	assert.InDelta(t, 31.0/46.0, ratios["video_input"], 1e-6)

	// duration=10 → 10/5 = 2.0
	require.Contains(t, ratios, "duration_estimate")
	assert.InDelta(t, 2.0, ratios["duration_estimate"], 1e-9)

	// hasVideo=true 应回写到 info.HasVideoInput，供 controller 冻结到 BillingContext
	assert.True(t, info.HasVideoInput)
}

// TestEstimateBillingDurationSentinelUsesMaxUpperBound 覆盖 duration=-1 / 缺失场景，
// 应回落到 MaxTaskDurationSeconds/5 作上限估算，保证预扣够扣。
func TestEstimateBillingDurationSentinelUsesMaxUpperBound(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "doubao-seedance-2-0-filter-off"}
	fallback := float64(relaycommon.MaxTaskDurationSeconds) / 5.0

	// duration=-1
	c := setupTaskRequestContext(t, map[string]any{
		"resolution": "720p",
		"duration":   float64(-1),
	})
	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.InDelta(t, fallback, ratios["duration_estimate"], 1e-9)

	// duration 缺失
	c2 := setupTaskRequestContext(t, map[string]any{"resolution": "720p"})
	ratios2 := (&TaskAdaptor{}).EstimateBilling(c2, info)
	require.NotNil(t, ratios2)
	assert.InDelta(t, fallback, ratios2["duration_estimate"], 1e-9)

	// 720p + hasVideo=false → GetVideoInputRatio 命中零值键返回 1.0，被 EstimateBilling 过滤
	assert.NotContains(t, ratios2, "video_input", "resolution=720p 无视频输入时应无 video_input")
}

// TestEstimateBillingBaselineNoRatios 覆盖 480p / 720p 基准无视频 + duration=5 时
// EstimateBilling 返回 nil 的场景（既没有价差也没有时长估算差异）。
func TestEstimateBillingBaselineNoRatios(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "doubao-seedance-2-0-filter-off"}
	c := setupTaskRequestContext(t, map[string]any{
		"resolution": "480p",
		"duration":   float64(5),
	})
	assert.Nil(t, (&TaskAdaptor{}).EstimateBilling(c, info))
}

// TestEstimateBillingUsesTopLevelDuration ensures the adapter reads the request's
// top-level duration field, not only Metadata["duration"].
func TestEstimateBillingUsesTopLevelDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:     "doubao-seedance-2-0-filter-off",
		Prompt:    "animate",
		Duration:  5,
		Metadata:  map[string]any{"resolution": "720p"},
	})

	info := &relaycommon.RelayInfo{OriginModelName: "doubao-seedance-2-0-filter-off"}
	assert.Nil(t, (&TaskAdaptor{}).EstimateBilling(c, info))
}

// TestParseTaskResultCopiesResolutionAndDuration 确认上游 succeeded 响应里的
// Resolution/Duration 被填进 TaskInfo，供 AdjustBillingRatiosOnComplete 使用。
func TestParseTaskResultCopiesResolutionAndDuration(t *testing.T) {
	body := []byte(`{
		"id":"cgt-1",
		"status":"succeeded",
		"resolution":"1080p",
		"duration":8,
		"content":{"video_url":"https://example.com/video.mp4"},
		"usage":{"completion_tokens":40594,"total_tokens":40594}
	}`)

	result, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, "1080p", result.Resolution)
	assert.Equal(t, 8, result.DurationSeconds)
	assert.Equal(t, 40594, result.TotalTokens)
}

// TestAdjustBillingRatiosOnCompleteOverridesResolution 覆盖典型场景：
// 用户传 adaptive，预扣时无 video_input（GetVideoInputRatio 返回 1.0）；
// 上游最终生成 1080p，结算前应用 51/46 覆盖 video_input，同时剥掉 duration_estimate。
func TestAdjustBillingRatiosOnCompleteOverridesResolution(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{OriginModelName: "doubao-seedance-2-0-filter-off"},
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "doubao-seedance-2-0-filter-off",
				HasVideoInput:   false,
				OtherRatios: map[string]float64{
					"duration_estimate": 3.0,
					// 请求 adaptive → EstimateBilling 未加 video_input
				},
			},
		},
	}
	taskResult := &relaycommon.TaskInfo{Resolution: "1080p", DurationSeconds: 8, TotalTokens: 100}

	newRatios := (&TaskAdaptor{}).AdjustBillingRatiosOnComplete(task, taskResult)
	require.NotNil(t, newRatios)

	assert.NotContains(t, newRatios, "duration_estimate", "duration_estimate 必须被剥离")
	require.Contains(t, newRatios, "video_input")
	assert.InDelta(t, 51.0/46.0, newRatios["video_input"], 1e-6)
}

// TestAdjustBillingRatiosOnCompletePreservesOriginalWhenUpstreamMissing 覆盖上游未返回
// resolution 的边界：应保留预扣阶段冻结的 video_input，只剥 duration_estimate。
func TestAdjustBillingRatiosOnCompletePreservesOriginalWhenUpstreamMissing(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{OriginModelName: "doubao-seedance-2-0-filter-off"},
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "doubao-seedance-2-0-filter-off",
				HasVideoInput:   true,
				OtherRatios: map[string]float64{
					"video_input":       28.0 / 46.0, // 冻结的：请求 720p+hasVideo
					"duration_estimate": 2.0,
					"other_dim":         1.5,
				},
			},
		},
	}
	taskResult := &relaycommon.TaskInfo{Resolution: "", TotalTokens: 100}

	newRatios := (&TaskAdaptor{}).AdjustBillingRatiosOnComplete(task, taskResult)
	require.NotNil(t, newRatios)

	assert.NotContains(t, newRatios, "duration_estimate")
	assert.InDelta(t, 28.0/46.0, newRatios["video_input"], 1e-6, "上游没返回 resolution 应保留原 video_input")
	assert.InDelta(t, 1.5, newRatios["other_dim"], 1e-9, "无关 key 应原样保留")
}

// TestAdjustBillingRatiosOnCompleteReturnsNilWhenNoContext 覆盖 BillingContext 缺失。
func TestAdjustBillingRatiosOnCompleteReturnsNilWhenNoContext(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{OriginModelName: "doubao-seedance-2-0-filter-off"},
	}
	assert.Nil(t, (&TaskAdaptor{}).AdjustBillingRatiosOnComplete(task, &relaycommon.TaskInfo{}))
}
