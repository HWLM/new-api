package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// RelaySeedanceV3Asset 处理 SeedanceV3 素材接口的两条路由：
//
//   - POST /v3/open/CreateAsset  上传素材，返回 {"id": "asset-xxxxxx"}
//   - POST /v3/open/GetAsset     查询素材，返回资产详情
//
// 该 controller 是一个简单透传：
//  1. 从渠道拿到素材上传专用的 base URL（OtherSettings.AssetBaseUrl，未配置时回落到主 base URL）
//  2. 用渠道 API Key 作为 Bearer 认证
//  3. body 由 SeedanceV3AssetRequestConvert 中间件保留在上下文里，原样 POST 上去
//  4. 上游响应状态码 + body 完整回给客户端
//
// 这条链路不参与计费：素材接口只是准备工作，费用在后续视频生成任务里结算。
func RelaySeedanceV3Asset(c *gin.Context) {
	baseURL := strings.TrimRight(common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl), "/")
	// OtherSettings.AssetBaseUrl 优先
	if s, ok := common.GetContextKey(c, constant.ContextKeyChannelOtherSetting); ok {
		if os, ok := s.(dto.ChannelOtherSettings); ok && strings.TrimSpace(os.AssetBaseUrl) != "" {
			baseURL = strings.TrimRight(strings.TrimSpace(os.AssetBaseUrl), "/")
		}
	}
	if baseURL == "" {
		respondAssetError(c, http.StatusBadGateway, "channel base URL is empty")
		return
	}

	apiKey := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if apiKey == "" {
		respondAssetError(c, http.StatusBadGateway, "channel API key is empty")
		return
	}

	// 从中间件保留的 KeyRequestBody 拿原始 body（middleware 已按 map 反解并重新 marshal 过）
	var bodyBytes []byte
	if v, exists := common.GetContextKey(c, common.KeyRequestBody); exists {
		if bs, ok := v.([]byte); ok {
			bodyBytes = bs
		}
	}
	if bodyBytes == nil {
		// 兜底：直接读 request body
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			respondAssetError(c, http.StatusBadRequest, "failed to read request body: "+err.Error())
			return
		}
		bodyBytes = raw
	}

	// URL 组装：路径 = 入口 path 的最后一段（CreateAsset / GetAsset），保持 base + /v3/open/<action> 的形状
	action := path.Base(c.Request.URL.Path)
	var modelRequest struct {
		Model string `json:"model"`
	}
	if err := common.Unmarshal(bodyBytes, &modelRequest); err != nil {
		respondAssetError(c, http.StatusBadRequest, "invalid asset request body: "+err.Error())
		return
	}
	if modelRequest.Model == dto.SeedanceV3ModelName {
		relaySDRealMaxAsset(c, baseURL, apiKey, action, bodyBytes)
		return
	}
	upstreamURL := fmt.Sprintf("%s/v3/open/%s", baseURL, action)

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		respondAssetError(c, http.StatusInternalServerError, "failed to build upstream request: "+err.Error())
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	proxy := ""
	if s, ok := common.GetContextKey(c, constant.ContextKeyChannelSetting); ok {
		if cs, ok := s.(dto.ChannelSettings); ok {
			proxy = cs.Proxy
		}
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		respondAssetError(c, http.StatusInternalServerError, "failed to build HTTP client: "+err.Error())
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		respondAssetError(c, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		respondAssetError(c, http.StatusBadGateway, "failed to read upstream response: "+err.Error())
		return
	}

	// 尽量透传上游的 Content-Type，其他 header 按需过滤
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	} else {
		c.Header("Content-Type", "application/json")
	}
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(respBody)
}

// respondAssetError 用与 wetoken 文档"错误响应格式"一致的 shape 返回错误，
// 便于客户端做统一错误处理。
func relaySDRealMaxAsset(c *gin.Context, baseURL, apiKey, action string, bodyBytes []byte) {
	var clientRequest struct {
		Model     string `json:"model"`
		URL       string `json:"url"`
		Name      string `json:"name"`
		AssetType string `json:"AssetType"`
		ID        string `json:"Id"`
	}
	if err := common.Unmarshal(bodyBytes, &clientRequest); err != nil {
		respondAssetError(c, http.StatusBadRequest, "invalid SD Real Max asset request body: "+err.Error())
		return
	}

	proxy := ""
	if s, ok := common.GetContextKey(c, constant.ContextKeyChannelSetting); ok {
		if cs, ok := s.(dto.ChannelSettings); ok {
			proxy = cs.Proxy
		}
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		respondAssetError(c, http.StatusInternalServerError, "failed to build HTTP client: "+err.Error())
		return
	}

	switch action {
	case "CreateAsset":
		if strings.TrimSpace(clientRequest.URL) == "" || strings.TrimSpace(clientRequest.Name) == "" {
			respondAssetError(c, http.StatusBadRequest, "url and name are required")
			return
		}
		assetType := strings.TrimSpace(clientRequest.AssetType)
		if assetType == "" {
			assetType = "Image"
		}
		payload, err := common.Marshal(dto.SeedanceV3AssetRequest{
			URL:       strings.TrimSpace(clientRequest.URL),
			Name:      strings.TrimSpace(clientRequest.Name),
			AssetType: assetType,
		})
		if err != nil {
			respondAssetError(c, http.StatusInternalServerError, "failed to encode SD Real Max asset request: "+err.Error())
			return
		}
		request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, baseURL+"/v1/sd/assets", bytes.NewReader(payload))
		if err != nil {
			respondAssetError(c, http.StatusInternalServerError, "failed to build SD Real Max asset request: "+err.Error())
			return
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+apiKey)
		response, err := client.Do(request)
		if err != nil {
			respondAssetError(c, http.StatusBadGateway, "SD Real Max asset request failed: "+err.Error())
			return
		}
		defer response.Body.Close()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			respondAssetError(c, http.StatusBadGateway, "failed to read SD Real Max asset response: "+err.Error())
			return
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			respondAssetError(c, response.StatusCode, string(responseBody))
			return
		}
		var assetResponse dto.SeedanceV3AssetResponse
		if err := common.Unmarshal(responseBody, &assetResponse); err != nil {
			respondAssetError(c, http.StatusBadGateway, "invalid SD Real Max asset response: "+err.Error())
			return
		}
		if !assetResponse.Success || assetResponse.Data.BaseResp == nil || assetResponse.Data.BaseResp.StatusCode != 0 || strings.TrimSpace(assetResponse.Data.ID) == "" {
			respondAssetError(c, http.StatusBadGateway, "SD Real Max asset creation failed")
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": strings.TrimSpace(assetResponse.Data.ID)})
	case "GetAsset":
		assetID := strings.TrimSpace(clientRequest.ID)
		if assetID == "" {
			respondAssetError(c, http.StatusBadRequest, "Id is required")
			return
		}
		request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, baseURL+"/v1/sd/assets/"+url.PathEscape(assetID), nil)
		if err != nil {
			respondAssetError(c, http.StatusInternalServerError, "failed to build SD Real Max asset query: "+err.Error())
			return
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+apiKey)
		response, err := client.Do(request)
		if err != nil {
			respondAssetError(c, http.StatusBadGateway, "SD Real Max asset query failed: "+err.Error())
			return
		}
		defer response.Body.Close()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			respondAssetError(c, http.StatusBadGateway, "failed to read SD Real Max asset detail: "+err.Error())
			return
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			respondAssetError(c, response.StatusCode, string(responseBody))
			return
		}
		var assetResponse dto.SeedanceV3AssetResponse
		if err := common.Unmarshal(responseBody, &assetResponse); err != nil {
			respondAssetError(c, http.StatusBadGateway, "invalid SD Real Max asset detail: "+err.Error())
			return
		}
		if !assetResponse.Success || assetResponse.Data.BaseResp == nil || assetResponse.Data.BaseResp.StatusCode != 0 {
			respondAssetError(c, http.StatusBadGateway, "SD Real Max asset query failed")
			return
		}
		if assetResponse.Data.Status == "" {
			assetResponse.Data.Status = "Active"
		}
		c.JSON(http.StatusOK, assetResponse.Data)
	default:
		respondAssetError(c, http.StatusNotFound, "unsupported asset action")
	}
}

func respondAssetError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"code":    status,
		"message": message,
	})
}
