package sdrealmax

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupHCContext 构造 hc 分支的请求上下文（middleware 归一化后的 SeedanceV3Request）。
func setupHCContext(t *testing.T, req dto.SeedanceV3VideoRequest) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	c.Set(string(constant.ContextKeySeedanceV3Request), req)
	return c
}

// TestEstimateBillingHC1080PWithVideoInput 覆盖 hc 分支：请求 1080p+video_url →
// 复用 doubao 价格表得到 31/46；duration=10 → duration_estimate=2.0；hasVideo → info 回写。
func TestEstimateBillingHC1080PWithVideoInput(t *testing.T) {
	duration := 10
	req := dto.SeedanceV3VideoRequest{
		Model:      ModelName,
		Resolution: "1080p",
		Duration:   &duration,
		Content: []dto.SeedanceV3ContentItem{
			{Type: "text", Text: "animate"},
			{Type: "video_url", VideoURL: &dto.SeedanceV3MediaURL{URL: "https://example.com/ref.mp4"}},
		},
	}
	c := setupHCContext(t, req)
	info := &relaycommon.RelayInfo{
		OriginModelName: ModelName,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: ModelName},
	}

	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)

	require.Contains(t, ratios, "video_input")
	assert.InDelta(t, 31.0/46.0, ratios["video_input"], 1e-6)

	require.Contains(t, ratios, "duration_estimate")
	assert.InDelta(t, 2.0, ratios["duration_estimate"], 1e-9)

	assert.True(t, info.HasVideoInput)
}

// TestEstimateBillingHC720PNoVideo 覆盖 720p 基准无视频输入：video_input 应被过滤
// （命中零值键 1.0），只留 duration_estimate。
func TestEstimateBillingHC720PNoVideo(t *testing.T) {
	duration := 8
	req := dto.SeedanceV3VideoRequest{
		Model:      ModelName,
		Resolution: "720p",
		Duration:   &duration,
		Content: []dto.SeedanceV3ContentItem{
			{Type: "text", Text: "animate"},
		},
	}
	c := setupHCContext(t, req)
	info := &relaycommon.RelayInfo{
		OriginModelName: ModelName,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: ModelName},
	}

	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)

	assert.NotContains(t, ratios, "video_input", "720p 无视频输入命中基准 1.0，应过滤")
	assert.InDelta(t, 8.0/5.0, ratios["duration_estimate"], 1e-9)
	assert.False(t, info.HasVideoInput)
}

// TestEstimateBillingHCDurationSentinel 覆盖 duration=nil（用户没传）应回落到
// MaxTaskDurationSeconds/5 的上限估算。
func TestEstimateBillingHCDurationSentinel(t *testing.T) {
	req := dto.SeedanceV3VideoRequest{
		Model:      ModelName,
		Resolution: "480p",
		Content: []dto.SeedanceV3ContentItem{
			{Type: "text", Text: "animate"},
		},
	}
	c := setupHCContext(t, req)
	info := &relaycommon.RelayInfo{
		OriginModelName: ModelName,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: ModelName},
	}

	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)

	expected := float64(relaycommon.MaxTaskDurationSeconds) / 5.0
	assert.InDelta(t, expected, ratios["duration_estimate"], 1e-9)
}

// TestAdjustBillingRatiosOnCompleteHCStripsDurationEstimate 覆盖 hc 分支：
// hc 上游不返回 resolution，AdjustBillingRatiosOnComplete 只剥 duration_estimate，
// 其他 ratio 原样保留。
func TestAdjustBillingRatiosOnCompleteHCStripsDurationEstimate(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{OriginModelName: dto.SeedanceV3ModelName},
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: dto.SeedanceV3ModelName,
				OtherRatios: map[string]float64{
					"video_input":       51.0 / 46.0,
					"duration_estimate": 2.0,
					"other_dim":         1.3,
				},
			},
		},
	}

	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	newRatios := adaptor.AdjustBillingRatiosOnComplete(task, &relaycommon.TaskInfo{})
	require.NotNil(t, newRatios)

	assert.NotContains(t, newRatios, "duration_estimate")
	assert.InDelta(t, 51.0/46.0, newRatios["video_input"], 1e-6, "hc 请求 resolution 就是最终值，video_input 必须保留")
	assert.InDelta(t, 1.3, newRatios["other_dim"], 1e-9)
}

// TestAdjustBillingRatiosOnCompleteHCReturnsNilWhenNoDurationEstimate 覆盖没有
// duration_estimate（比如未来老任务或客户端未传 duration 的场景）：直接返回 nil，
// settle 逻辑不做覆盖，避免不必要的 DB 写。
func TestAdjustBillingRatiosOnCompleteHCReturnsNilWhenNoDurationEstimate(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{OriginModelName: dto.SeedanceV3ModelName},
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: dto.SeedanceV3ModelName,
				OtherRatios: map[string]float64{
					"video_input": 51.0 / 46.0,
				},
			},
		},
	}

	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	assert.Nil(t, adaptor.AdjustBillingRatiosOnComplete(task, &relaycommon.TaskInfo{}))
}

// TestAdjustBillingRatiosOnCompleteDoubaoDelegated 覆盖非 hc 模型：应该委托给
// doubao delegate，用上游返回的实际 resolution 覆盖。
func TestAdjustBillingRatiosOnCompleteDoubaoDelegated(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{OriginModelName: "doubao-seedance-2-0-filter-off"},
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "doubao-seedance-2-0-filter-off",
				HasVideoInput:   false,
				OtherRatios: map[string]float64{
					"duration_estimate": 3.0,
				},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-filter-off",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "doubao-seedance-2-0-filter-off"},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	taskResult := &relaycommon.TaskInfo{Resolution: "1080p"}
	newRatios := adaptor.AdjustBillingRatiosOnComplete(task, taskResult)
	require.NotNil(t, newRatios)

	assert.NotContains(t, newRatios, "duration_estimate")
	require.Contains(t, newRatios, "video_input")
	// doubao delegate 用 GetVideoInputRatio("doubao-seedance-2-0-filter-off", "1080p", false) = 51/46
	assert.InDelta(t, 51.0/46.0, newRatios["video_input"], 1e-6)
}

// TestEstimateBillingDoubaoDelegated 覆盖非 hc 模型的 EstimateBilling 委托路径。
// 用 map[string]any 通过 task_request 走 doubao delegate 分支。
func TestEstimateBillingDoubaoDelegated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	// 非 hc → 不设 ContextKeySeedanceV3Request
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2-0-filter-off",
		Prompt: "animate",
		Metadata: map[string]any{
			"resolution": "1080p",
			"duration":   float64(10),
			"content": []any{
				map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://example.com/ref.mp4"}},
			},
		},
	})

	info := &relaycommon.RelayInfo{
		OriginModelName: "doubao-seedance-2-0-filter-off",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "doubao-seedance-2-0-filter-off"},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	ratios := adaptor.EstimateBilling(c, info)
	require.NotNil(t, ratios)

	// 委托给 doubao：1080p+hasVideo → 31/46
	assert.InDelta(t, 31.0/46.0, ratios["video_input"], 1e-6)
	assert.InDelta(t, 2.0, ratios["duration_estimate"], 1e-9)
	assert.True(t, info.HasVideoInput)
}

// TestSettleBillingContextRatiosPersisted 是一个轻量集成断言：
// 走完 EstimateBilling 后，用 common.UnmarshalJsonStr 反序列化 TaskBillingContext
// 应能同时看到 has_video_input 字段的 JSON tag。
func TestBillingContextHasVideoInputRoundTrip(t *testing.T) {
	src := model.TaskBillingContext{
		OriginModelName: dto.SeedanceV3ModelName,
		HasVideoInput:   true,
		OtherRatios:     map[string]float64{"video_input": 31.0 / 46.0},
	}
	data, err := common.Marshal(src)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"has_video_input":true`)

	var got model.TaskBillingContext
	require.NoError(t, common.Unmarshal(data, &got))
	assert.True(t, got.HasVideoInput)
	assert.InDelta(t, 31.0/46.0, got.OtherRatios["video_input"], 1e-6)
}
