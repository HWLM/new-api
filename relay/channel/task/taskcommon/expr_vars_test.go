package taskcommon_test

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
)

func TestExtractTaskExprVars_SecondsPriority(t *testing.T) {
	cases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want float64
	}{
		{"duration_int", relaycommon.TaskSubmitReq{Duration: 8}, 8},
		{"seconds_string", relaycommon.TaskSubmitReq{Seconds: "12"}, 12},
		{"duration_beats_seconds", relaycommon.TaskSubmitReq{Duration: 5, Seconds: "99"}, 5},
		{"metadata_duration_fallback", relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"duration": 7.0}}, 7},
		{"metadata_durationSeconds_fallback", relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"durationSeconds": 9}}, 9},
		{"none", relaycommon.TaskSubmitReq{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := taskcommon.ExtractTaskExprVars(tc.req, nil, nil)
			assert.InDelta(t, tc.want, got.Seconds, 1e-9)
		})
	}
}

func TestExtractTaskExprVars_SecondsSaturation(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Duration: 999999}
	got := taskcommon.ExtractTaskExprVars(req, nil, nil)
	assert.Equal(t, float64(relaycommon.MaxTaskDurationSeconds), got.Seconds, "seconds must be clamped to MaxTaskDurationSeconds")
}

func TestExtractTaskExprVars_ResolutionSources(t *testing.T) {
	cases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want string
	}{
		{"metadata_wins", relaycommon.TaskSubmitReq{Size: "1920x1080", Metadata: map[string]interface{}{"resolution": "4K"}}, "4k"},
		{"size_wxh_1080", relaycommon.TaskSubmitReq{Size: "1920x1080"}, "1080p"},
		{"size_wxh_720", relaycommon.TaskSubmitReq{Size: "1280x720"}, "720p"},
		{"size_wxh_4k", relaycommon.TaskSubmitReq{Size: "3840x2160"}, "4k"},
		{"size_shorthand", relaycommon.TaskSubmitReq{Size: "1080P"}, "1080p"},
		{"none", relaycommon.TaskSubmitReq{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := taskcommon.ExtractTaskExprVars(tc.req, nil, nil)
			assert.Equal(t, tc.want, got.Resolution)
		})
	}
}

func TestExtractTaskExprVars_SeedanceDefaultsAdaptiveResolution(t *testing.T) {
	cases := []struct {
		name      string
		modelName string
		body      string
		want      string
	}{
		{
			name:      "missing_resolution",
			modelName: "doubao-seedance-2-0-filter-off",
			body:      `{"model":"doubao-seedance-2-0-filter-off"}`,
			want:      "720p",
		},
		{
			name:      "adaptive_resolution",
			modelName: "doubao-seedance-2-0",
			body:      `{"resolution":"adaptive"}`,
			want:      "720p",
		},
		{
			name:      "explicit_resolution_wins",
			modelName: "doubao-seedance-2-0-fast",
			body:      `{"resolution":"1080p"}`,
			want:      "1080p",
		},
		{
			name:      "other_models_keep_empty_resolution",
			modelName: "kling-v2",
			body:      `{}`,
			want:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: tc.modelName}
			got := taskcommon.ExtractTaskExprVars(
				relaycommon.TaskSubmitReq{},
				info,
				[]byte(tc.body),
			)
			assert.Equal(t, tc.want, got.Resolution)
		})
	}
}

func TestExtractTaskExprVars_HasVideoDetection(t *testing.T) {
	t.Run("info_flag", func(t *testing.T) {
		info := &relaycommon.RelayInfo{HasVideoInput: true}
		got := taskcommon.ExtractTaskExprVars(relaycommon.TaskSubmitReq{}, info, nil)
		assert.True(t, got.HasVideo)
	})
	t.Run("metadata_content_video_url", func(t *testing.T) {
		req := relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "video_url", "video_url": "https://x/v.mp4"},
			},
		}}
		got := taskcommon.ExtractTaskExprVars(req, nil, nil)
		assert.True(t, got.HasVideo)
	})
	t.Run("metadata_top_level_video_url", func(t *testing.T) {
		req := relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"video_url": "x"}}
		got := taskcommon.ExtractTaskExprVars(req, nil, nil)
		assert.True(t, got.HasVideo)
	})
	t.Run("none", func(t *testing.T) {
		got := taskcommon.ExtractTaskExprVars(relaycommon.TaskSubmitReq{}, nil, nil)
		assert.False(t, got.HasVideo)
	})
}

func TestExtractTaskExprVars_HasImageDetection(t *testing.T) {
	cases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want bool
	}{
		{"single_image", relaycommon.TaskSubmitReq{Image: "https://x/i.png"}, true},
		{"images_slice", relaycommon.TaskSubmitReq{Images: []string{"a"}}, true},
		{"metadata_image", relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"image": "x"}}, true},
		{"metadata_first_frame", relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"first_frame_url": "x"}}, true},
		{"metadata_content_image_url", relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{
			"content": []interface{}{map[string]interface{}{"type": "image_url"}},
		}}, true},
		{"empty_string_image_ignored", relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"image": ""}}, false},
		{"none", relaycommon.TaskSubmitReq{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := taskcommon.ExtractTaskExprVars(tc.req, nil, nil)
			assert.Equal(t, tc.want, got.HasImage)
		})
	}
}

func TestExtractTaskExprVars_NAndMode(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Mode: "std",
		Metadata: map[string]interface{}{
			"n":    4,
			"mode": "pro",
		},
	}
	got := taskcommon.ExtractTaskExprVars(req, nil, nil)
	assert.Equal(t, 4.0, got.N)
	assert.Equal(t, "pro", got.Mode, "metadata.mode should beat req.Mode")
}

func TestExtractTaskExprVars_NDefaultsToOne(t *testing.T) {
	got := taskcommon.ExtractTaskExprVars(relaycommon.TaskSubmitReq{}, nil, nil)
	assert.Equal(t, 1.0, got.N)
}

func TestNormalizeResolutionFromSize_Table(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"1920x1080":    "1080p",
		"1080x1920":    "1080p",
		"1280 x 720":   "720p",
		"3840x2160":    "4k",
		"2160p":        "4k",
		"4K":           "4k",
		"1080p":        "1080p",
		"720P":         "720p",
		"640*480":      "480p",
		"garbagevalue": "",
	}
	for in, want := range cases {
		got := taskcommon.NormalizeResolutionFromSize(in)
		assert.Equalf(t, want, got, "input=%q", in)
	}
}

func TestExtractTaskExprVars_ReturnsModelType(t *testing.T) {
	// Compile-time assertion that ExtractTaskExprVars returns model.TaskExprVars
	// so TaskBillingContext can persist it without an extra conversion step.
	var v model.TaskExprVars = taskcommon.ExtractTaskExprVars(relaycommon.TaskSubmitReq{}, nil, nil)
	_ = v
}

// Regression: doubao/seedance-style requests put resolution / content /
// duration at the JSON top level (outside TaskSubmitReq.Metadata). Without
// rawBody fallback, extracted vars would be all zero and v2 tiered_expr
// would pre-consume $0 for any expression that keys off these fields.
func TestExtractTaskExprVars_RawBodyFallback_Doubao(t *testing.T) {
	body := []byte(`{
		"model": "doubao-seedance-2-0-filter-off",
		"content": [
			{"type": "text", "text": "cat playing piano"},
			{"type": "video_url", "video_url": {"url": "asset://abc"}}
		],
		"duration": 8,
		"resolution": "1080p"
	}`)
	// TaskSubmitReq only parses `duration` at top level — resolution / content
	// are dropped by struct unmarshal, so must come from the rawBody fallback.
	req := relaycommon.TaskSubmitReq{Duration: 8}
	got := taskcommon.ExtractTaskExprVars(req, nil, body)
	assert.Equal(t, 8.0, got.Seconds)
	assert.Equal(t, "1080p", got.Resolution)
	assert.True(t, got.HasVideo, "video_url in top-level content[] must set HasVideo")
	assert.False(t, got.HasImage)
}

func TestExtractTaskExprVars_RawBodyFallback_TopLevelImage(t *testing.T) {
	body := []byte(`{
		"model": "seedance-i2v",
		"content": [
			{"type": "text", "text": "make it move"},
			{"type": "image_url", "image_url": {"url": "https://x/a.png"}}
		],
		"resolution": "720p",
		"duration": 4
	}`)
	got := taskcommon.ExtractTaskExprVars(relaycommon.TaskSubmitReq{}, nil, body)
	assert.Equal(t, 4.0, got.Seconds)
	assert.Equal(t, "720p", got.Resolution)
	assert.True(t, got.HasImage)
	assert.False(t, got.HasVideo)
}

// Struct fields take precedence over rawBody — never regress the older path.
func TestExtractTaskExprVars_StructBeatsBody(t *testing.T) {
	body := []byte(`{"resolution": "480p", "duration": 99}`)
	req := relaycommon.TaskSubmitReq{
		Duration: 5,
		Metadata: map[string]interface{}{"resolution": "4K"},
	}
	got := taskcommon.ExtractTaskExprVars(req, nil, body)
	assert.Equal(t, 5.0, got.Seconds, "req.Duration wins over body.duration")
	assert.Equal(t, "4k", got.Resolution, "metadata.resolution wins over body.resolution")
}
