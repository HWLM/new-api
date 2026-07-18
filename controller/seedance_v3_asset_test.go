package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelaySeedanceV3AssetForwardsToAssetBaseUrl(t *testing.T) {
	service.InitHttpClient()

	var callsToAssetBase, callsToMainBase atomic.Int32
	mainBase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsToMainBase.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(mainBase.Close)

	assetBase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsToAssetBase.Add(1)
		assert.Equal(t, "/v3/open/CreateAsset", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer channel-key", r.Header.Get("Authorization"))
		// 上游收到的应该是客户端原始 body（含 model / url / name / AssetType）
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, "doubao-seedance-2-0", got["model"])
		assert.Equal(t, "https://example.com/x.jpg", got["url"])
		assert.Equal(t, "avatar_front", got["name"])
		assert.Equal(t, "Image", got["AssetType"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"asset-abc123"}`))
	}))
	t.Cleanup(assetBase.Close)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v3/open/CreateAsset", strings.NewReader(`{"model":"doubao-seedance-2-0","url":"https://example.com/x.jpg","name":"avatar_front","AssetType":"Image"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	// 模拟 Distribute 选完渠道之后的上下文：主 base URL 与素材 base URL 不同域，
	// AssetBaseUrl 优先级应高于主 base URL。
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, mainBase.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "channel-key")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		AssetBaseUrl: assetBase.URL,
	})

	RelaySeedanceV3Asset(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, int32(1), callsToAssetBase.Load(), "asset base URL should receive the request")
	assert.Equal(t, int32(0), callsToMainBase.Load(), "main base URL must NOT be hit when AssetBaseUrl is set")

	var out map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &out))
	assert.Equal(t, "asset-abc123", out["id"])
}

func TestRelaySeedanceV3AssetFallbackToMainBaseUrl(t *testing.T) {
	service.InitHttpClient()

	var calls atomic.Int32
	mainBase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/v3/open/GetAsset", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"asset-abc","Status":"Active"}`))
	}))
	t.Cleanup(mainBase.Close)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v3/open/GetAsset", strings.NewReader(`{"model":"doubao-seedance-2-0","Id":"asset-abc"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	// 未配置 AssetBaseUrl —— 回落到主 base URL
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, mainBase.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "channel-key")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})

	RelaySeedanceV3Asset(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, int32(1), calls.Load())
	var out map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &out))
	assert.Equal(t, "Active", out["Status"])
}

func TestRelaySeedanceV3AssetConvertsHCAssetCreation(t *testing.T) {
	service.InitHttpClient()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/sd/assets", r.URL.Path)
		assert.Equal(t, "Bearer channel-key", r.Header.Get("Authorization"))

		var request map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &request))
		assert.Equal(t, "https://example.com/hc.jpg", request["URL"])
		assert.Equal(t, "hc-reference.jpg", request["Name"])
		assert.Equal(t, "Image", request["AssetType"])
		assert.NotContains(t, request, "model")
		assert.NotContains(t, request, "url")
		assert.NotContains(t, request, "name")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"Id":"asset-hc","base_resp":{"status_code":0,"status_msg":"success"}}}`))
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v3/open/CreateAsset", strings.NewReader(`{"model":"dreamina-seedance-2-0-hc","url":"https://example.com/hc.jpg","name":"hc-reference.jpg","AssetType":"Image"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "channel-key")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})

	RelaySeedanceV3Asset(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "asset-hc", response["id"])
}

func TestRelaySeedanceV3AssetConvertsHCAssetQuery(t *testing.T) {
	service.InitHttpClient()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/sd/assets/asset-hc", r.URL.Path)
		assert.Equal(t, "Bearer channel-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"Id":"asset-hc","AssetType":"Image","Name":"hc-reference.jpg","URL":"https://example.com/hc.jpg","base_resp":{"status_code":0,"status_msg":"success"}}}`))
	}))
	t.Cleanup(upstream.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v3/open/GetAsset", strings.NewReader(`{"model":"dreamina-seedance-2-0-hc","Id":"asset-hc"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "channel-key")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})

	RelaySeedanceV3Asset(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response dto.SeedanceV3AssetData
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "asset-hc", response.ID)
	assert.Equal(t, "Active", response.Status)
	assert.Equal(t, "https://example.com/hc.jpg", response.URL)
}
