package taskcommon

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSeedanceV3RouteKeepsDefaultsDisabled(t *testing.T) {
	route, err := NormalizeSeedanceV3Route("https://example.com", nil, http.MethodPost)
	require.NoError(t, err)
	assert.Nil(t, route)
}

func TestBuildSeedanceV3TaskGetRequestSupportsPathAndBodyStyles(t *testing.T) {
	t.Run("GET path template", func(t *testing.T) {
		request, err := BuildSeedanceV3TaskGetRequest(
			"https://example.com",
			&dto.SeedanceV3Route{Method: http.MethodGet, Target: "/tasks/{task_id}"},
			"task/one",
			"secret",
		)
		require.NoError(t, err)
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "https://example.com/tasks/task%2Fone", request.URL.String())
		assert.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
	})

	t.Run("POST JSON body", func(t *testing.T) {
		request, err := BuildSeedanceV3TaskGetRequest(
			"https://example.com",
			&dto.SeedanceV3Route{Method: http.MethodPost, Target: "https://query.example.com/task"},
			"task-one",
			"secret",
		)
		require.NoError(t, err)
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "https://query.example.com/task", request.URL.String())
		assert.JSONEq(t, `{"task_id":"task-one"}`, string(body))
		assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	})

	t.Run("GET query parameters", func(t *testing.T) {
		request, err := BuildSeedanceV3TaskGetRequest(
			"https://example.com",
			&dto.SeedanceV3Route{
				Method: http.MethodGet,
				Target: "/tasks/query?region=old",
				Parameters: map[string]any{
					"job_id": "{task_id}",
					"region": "ap-southeast-1",
				},
			},
			"task-one",
			"secret",
		)
		require.NoError(t, err)
		assert.Equal(t, "/tasks/query", request.URL.Path)
		assert.Equal(t, "task-one", request.URL.Query().Get("job_id"))
		assert.Equal(t, "ap-southeast-1", request.URL.Query().Get("region"))
	})

	t.Run("POST parameter overrides", func(t *testing.T) {
		request, err := BuildSeedanceV3TaskGetRequest(
			"https://example.com",
			&dto.SeedanceV3Route{
				Method: http.MethodPost,
				Target: "/tasks/query",
				Parameters: map[string]any{
					"task_id": nil,
					"job_id":  "{task_id}",
				},
			},
			"task-one",
			"secret",
		)
		require.NoError(t, err)
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"job_id":"task-one"}`, string(body))
	})
}

func TestApplySeedanceV3RouteParametersMergesWithCurrentBody(t *testing.T) {
	body, err := ApplySeedanceV3RouteParameters(
		strings.NewReader(`{
			"model":"seedance",
			"url":"https://example.com/asset.jpg",
			"name":"character",
			"content":[{"type":"text","text":"hello"}],
			"duration":5,
			"options":{"watermark":false,"seed":1}
		}`),
		&dto.SeedanceV3Route{Parameters: map[string]any{
			"duration": nil,
			"url":      nil,
			"name":     nil,
			"URL":      "{url}",
			"Name":     "{name}",
			"Content":  "{content}",
			"Prompt":   "{content.0.text}",
			"region":   "global",
			"metadata": map[string]any{
				"removed": nil,
				"source":  "channel",
			},
			"options": map[string]any{
				"watermark": true,
			},
		}},
	)
	require.NoError(t, err)
	result, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model":"seedance",
		"URL":"https://example.com/asset.jpg",
		"Name":"character",
		"Content":[{"type":"text","text":"hello"}],
		"Prompt":"hello",
		"content":[{"type":"text","text":"hello"}],
		"region":"global",
		"metadata":{"source":"channel"},
		"options":{"watermark":true,"seed":1}
	}`, string(result))
}

func TestApplySeedanceV3RouteParametersRejectsMissingMappingSource(t *testing.T) {
	_, err := ApplySeedanceV3RouteParameters(
		strings.NewReader(`{"model":"seedance"}`),
		&dto.SeedanceV3Route{Parameters: map[string]any{"URL": "{url}"}},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mapping source not found: url")
}

func TestApplySeedanceV3RouteResponseMappingPreservesMappedTypes(t *testing.T) {
	responseBody, err := ApplySeedanceV3RouteResponseMapping(
		[]byte(`{
			"payload":{
				"task":{"id":"task-1"},
				"outputs":[{"url":"https://example.com/video.mp4"}]
			},
			"legacy":true
		}`),
		&dto.SeedanceV3Route{ResponseMapping: map[string]any{
			"id":      "{payload.task.id}",
			"outputs": "{payload.outputs}",
			"first":   "{payload.outputs.0}",
			"payload": nil,
			"legacy":  nil,
		}},
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id":"task-1",
		"outputs":[{"url":"https://example.com/video.mp4"}],
		"first":{"url":"https://example.com/video.mp4"}
	}`, string(responseBody))
}

func TestApplySeedanceV3RouteResponseMappingRejectsMissingSource(t *testing.T) {
	_, err := ApplySeedanceV3RouteResponseMapping(
		[]byte(`{"payload":{}}`),
		&dto.SeedanceV3Route{ResponseMapping: map[string]any{"id": "{payload.id}"}},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mapping source not found: payload.id")
}
