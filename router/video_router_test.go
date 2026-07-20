package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetVideoRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetVideoRouter(engine)

	routes := engine.Routes()
	require.NotNil(t, routes, "routes should not be nil")
	assert.Greater(t, len(routes), 0, "should register at least one route")

	routeMap := make(map[string]bool)
	for _, route := range routes {
		routeMap[route.Method+" "+route.Path] = true
	}

	// Test cases for all registered routes
	tests := []struct {
		name     string
		method   string
		path     string
		expected bool
	}{
		// Seedance V3 routes
		{
			name:     "Seedance V3 POST /tasks",
			method:   http.MethodPost,
			path:     "/api/v3/contents/generations/tasks",
			expected: true,
		},
		{
			name:     "Seedance V3 GET /tasks/:task_id",
			method:   http.MethodGet,
			path:     "/api/v3/contents/generations/tasks/:task_id",
			expected: true,
		},
		// Video Proxy routes
		{
			name:     "Video Proxy GET /videos/:task_id/content",
			method:   http.MethodGet,
			path:     "/v1/videos/:task_id/content",
			expected: true,
		},
		// Video V1 routes
		{
			name:     "Video V1 POST /video/generations",
			method:   http.MethodPost,
			path:     "/v1/video/generations",
			expected: true,
		},
		{
			name:     "Video V1 GET /video/generations/:task_id",
			method:   http.MethodGet,
			path:     "/v1/video/generations/:task_id",
			expected: true,
		},
		{
			name:     "Video V1 POST /videos/:video_id/remix",
			method:   http.MethodPost,
			path:     "/v1/videos/:video_id/remix",
			expected: true,
		},
		{
			name:     "Video V1 POST /videos (OpenAI compatible)",
			method:   http.MethodPost,
			path:     "/v1/videos",
			expected: true,
		},
		{
			name:     "Video V1 GET /videos/:task_id (OpenAI compatible)",
			method:   http.MethodGet,
			path:     "/v1/videos/:task_id",
			expected: true,
		},
		// Kling V1 routes
		{
			name:     "Kling V1 POST /videos/text2video",
			method:   http.MethodPost,
			path:     "/kling/v1/videos/text2video",
			expected: true,
		},
		{
			name:     "Kling V1 POST /videos/image2video",
			method:   http.MethodPost,
			path:     "/kling/v1/videos/image2video",
			expected: true,
		},
		{
			name:     "Kling V1 GET /videos/text2video/:task_id",
			method:   http.MethodGet,
			path:     "/kling/v1/videos/text2video/:task_id",
			expected: true,
		},
		{
			name:     "Kling V1 GET /videos/image2video/:task_id",
			method:   http.MethodGet,
			path:     "/kling/v1/videos/image2video/:task_id",
			expected: true,
		},
		// Jimeng official API routes
		{
			name:     "Jimeng POST /",
			method:   http.MethodPost,
			path:     "/jimeng/",
			expected: true,
		},
		// Seedance V3 asset routes
		{
			name:     "Seedance V3 asset POST /v3/open/CreateAsset",
			method:   http.MethodPost,
			path:     "/v3/open/CreateAsset",
			expected: true,
		},
		{
			name:     "Seedance V3 asset POST /v3/open/GetAsset",
			method:   http.MethodPost,
			path:     "/v3/open/GetAsset",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.method + " " + tt.path
			exists := routeMap[key]
			assert.Equal(t, tt.expected, exists, "route %s should exist: %v", key, tt.expected)
		})
	}

	// Verify total number of routes
	expectedRouteCount := 15
	assert.Equal(t, expectedRouteCount, len(routes), "should register exactly %d routes", expectedRouteCount)
}

func TestSetVideoRouterRegistersSeedanceV3Contract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetVideoRouter(engine)

	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	assert.True(t, routes[http.MethodPost+" /api/v3/contents/generations/tasks"])
	assert.True(t, routes[http.MethodGet+" /api/v3/contents/generations/tasks/:task_id"])
}
