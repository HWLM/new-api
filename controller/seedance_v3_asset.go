package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const seedanceV3AssetLogResultKey = "seedance_v3_asset_log_result"

// RelaySeedanceV3Asset 处理 SeedanceV3 素材接口：
//
//   - POST /api/v3/open/CreateAsset 上传素材，返回 {"id": "asset-xxxxxx"}
//   - POST /api/v3/open/GetAsset 查询素材，返回资产详情
//   - POST /v3/open/GetAsset     查询素材，兼容旧路径
//
// 该 controller 是一个简单透传：
//  1. 从渠道拿到素材上传专用的 base URL（OtherSettings.AssetBaseUrl，未配置时回落到主 base URL）
//  2. 用渠道 API Key 作为 Bearer 认证
//  3. body 由 SeedanceV3AssetRequestConvert 中间件保留在上下文里，原样 POST 上去
//  4. 上游响应状态码 + body 完整回给客户端
//
// 这条链路不参与计费：素材接口只是准备工作，费用在后续视频生成任务里结算。
func RelaySeedanceV3Asset(c *gin.Context) {
	startedAt := time.Now()
	var assetLogBody []byte
	var assetLogModel string
	if path.Base(c.Request.URL.Path) == "CreateAsset" {
		defer func() {
			recordSeedanceV3AssetUploadLog(c, assetLogModel, assetLogBody, startedAt)
		}()
	}
	baseURL := strings.TrimRight(common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl), "/")
	var customAssetCreateRoute *dto.SeedanceV3Route
	var customAssetGetRoute *dto.SeedanceV3Route
	// OtherSettings.AssetBaseUrl 优先
	if s, ok := common.GetContextKey(c, constant.ContextKeyChannelOtherSetting); ok {
		if os, ok := s.(dto.ChannelOtherSettings); ok {
			if strings.TrimSpace(os.AssetBaseUrl) != "" {
				baseURL = strings.TrimRight(strings.TrimSpace(os.AssetBaseUrl), "/")
			}
			if os.SeedanceV3Routes != nil && os.SeedanceV3Routes.AssetCreate.IsConfigured() {
				customAssetCreateRoute = os.SeedanceV3Routes.AssetCreate
			}
			if os.SeedanceV3Routes != nil && os.SeedanceV3Routes.AssetGet.IsConfigured() {
				customAssetGetRoute = os.SeedanceV3Routes.AssetGet
			}
		}
	}
	action := path.Base(c.Request.URL.Path)
	customRoute := customAssetCreateRoute
	if action == "GetAsset" {
		customRoute = customAssetGetRoute
	}
	customRouteNeedsBaseURL := customRoute == nil || strings.HasPrefix(strings.TrimSpace(customRoute.Target), "/")
	if baseURL == "" && customRouteNeedsBaseURL {
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
	assetLogBody = bodyBytes

	// URL 组装：路径 = 入口 path 的最后一段（CreateAsset / GetAsset），保持 base + /v3/open/<action> 的形状
	var modelRequest struct {
		Model string `json:"model"`
		ID    string `json:"Id"`
	}
	if err := common.Unmarshal(bodyBytes, &modelRequest); err != nil {
		respondAssetError(c, http.StatusBadRequest, "invalid asset request body: "+err.Error())
		return
	}
	assetLogModel = modelRequest.Model
	if customRoute != nil {
		var route *dto.SeedanceV3Route
		var err error
		if action == "GetAsset" {
			route, err = taskcommon.ResolveSeedanceV3AssetGetRoute(baseURL, customRoute, modelRequest.ID)
		} else {
			route, err = taskcommon.NormalizeSeedanceV3Route(baseURL, customRoute, http.MethodPost)
		}
		if err != nil {
			respondAssetError(c, http.StatusBadRequest, err.Error())
			return
		}
		if modelRequest.Model == dto.SeedanceV3ModelName {
			if action == "CreateAsset" {
				relaySDRealMaxCreateAssetCustom(c, route, apiKey, bodyBytes)
				return
			}
			relaySeedanceV3AssetCustom(c, route, apiKey, bodyBytes)
			return
		}
		relaySeedanceV3AssetCustom(c, route, apiKey, bodyBytes)
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
	setSeedanceV3AssetLogResult(c, respBody)

	// 尽量透传上游的 Content-Type，其他 header 按需过滤
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	} else {
		c.Header("Content-Type", "application/json")
	}
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(respBody)
}

func relaySeedanceV3AssetCustom(c *gin.Context, route *dto.SeedanceV3Route, apiKey string, bodyBytes []byte) {
	status, responseBody, contentType, err := executeSeedanceV3AssetRoute(c, route, apiKey, bodyBytes)
	if err != nil {
		respondAssetError(c, http.StatusBadGateway, "upstream request failed: "+err.Error())
		return
	}
	if contentType == "" {
		contentType = "application/json"
	}
	setSeedanceV3AssetLogResult(c, responseBody)
	c.Header("Content-Type", contentType)
	c.Writer.WriteHeader(status)
	_, _ = c.Writer.Write(responseBody)
}

func relaySDRealMaxCreateAssetCustom(c *gin.Context, route *dto.SeedanceV3Route, apiKey string, bodyBytes []byte) {
	var clientRequest struct {
		URL       string `json:"url"`
		Name      string `json:"name"`
		AssetType string `json:"AssetType"`
	}
	if err := common.Unmarshal(bodyBytes, &clientRequest); err != nil {
		respondAssetError(c, http.StatusBadRequest, "invalid Byteplus asset request body: "+err.Error())
		return
	}
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
		respondAssetError(c, http.StatusInternalServerError, "failed to encode Byteplus asset request: "+err.Error())
		return
	}
	status, responseBody, _, err := executeSeedanceV3AssetRoute(c, route, apiKey, payload)
	if err != nil {
		respondAssetError(c, http.StatusBadGateway, "Byteplus asset request failed: "+err.Error())
		return
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		respondAssetError(c, status, string(responseBody))
		return
	}
	var assetResponse dto.SeedanceV3AssetResponse
	if err := common.Unmarshal(responseBody, &assetResponse); err != nil {
		respondAssetError(c, http.StatusBadGateway, "invalid Byteplus asset response: "+err.Error())
		return
	}
	if !assetResponse.Success || assetResponse.Data.BaseResp == nil || assetResponse.Data.BaseResp.StatusCode != 0 || strings.TrimSpace(assetResponse.Data.ID) == "" {
		respondAssetError(c, http.StatusBadGateway, "Byteplus asset creation failed")
		return
	}
	result := gin.H{"id": strings.TrimSpace(assetResponse.Data.ID)}
	setSeedanceV3AssetLogResult(c, result)
	c.JSON(http.StatusOK, result)
}

func executeSeedanceV3AssetRoute(c *gin.Context, route *dto.SeedanceV3Route, apiKey string, bodyBytes []byte) (int, []byte, string, error) {
	target := route.Target
	var err error
	var requestBody io.Reader
	if route.Method == http.MethodGet {
		target, err = taskcommon.ApplySeedanceV3RouteQueryParameters(target, bytes.NewReader(bodyBytes), route)
		if err != nil {
			return 0, nil, "", err
		}
	} else {
		requestBody, err = taskcommon.ApplySeedanceV3RouteParameters(bytes.NewReader(bodyBytes), route)
		if err != nil {
			return 0, nil, "", err
		}
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), route.Method, target, requestBody)
	if err != nil {
		return 0, nil, "", err
	}
	request.Header.Set("Accept", "application/json")
	if route.Method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	proxy := ""
	if s, ok := common.GetContextKey(c, constant.ContextKeyChannelSetting); ok {
		if cs, ok := s.(dto.ChannelSettings); ok {
			proxy = cs.Proxy
		}
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return 0, nil, "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, "", err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, "", err
	}
	responseBody, err = taskcommon.ApplySeedanceV3RouteResponseMapping(responseBody, route)
	if err != nil {
		return 0, nil, "", err
	}
	return response.StatusCode, responseBody, response.Header.Get("Content-Type"), nil
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
		respondAssetError(c, http.StatusBadRequest, "invalid Byteplus asset request body: "+err.Error())
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
			respondAssetError(c, http.StatusInternalServerError, "failed to encode Byteplus asset request: "+err.Error())
			return
		}
		request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, baseURL+"/v1/sd/assets", bytes.NewReader(payload))
		if err != nil {
			respondAssetError(c, http.StatusInternalServerError, "failed to build Byteplus asset request: "+err.Error())
			return
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+apiKey)
		response, err := client.Do(request)
		if err != nil {
			respondAssetError(c, http.StatusBadGateway, "Byteplus asset request failed: "+err.Error())
			return
		}
		defer response.Body.Close()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			respondAssetError(c, http.StatusBadGateway, "failed to read Byteplus asset response: "+err.Error())
			return
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			respondAssetError(c, response.StatusCode, string(responseBody))
			return
		}
		var assetResponse dto.SeedanceV3AssetResponse
		if err := common.Unmarshal(responseBody, &assetResponse); err != nil {
			respondAssetError(c, http.StatusBadGateway, "invalid Byteplus asset response: "+err.Error())
			return
		}
		if !assetResponse.Success || assetResponse.Data.BaseResp == nil || assetResponse.Data.BaseResp.StatusCode != 0 || strings.TrimSpace(assetResponse.Data.ID) == "" {
			respondAssetError(c, http.StatusBadGateway, "Byteplus asset creation failed")
			return
		}
		result := gin.H{"id": strings.TrimSpace(assetResponse.Data.ID)}
		setSeedanceV3AssetLogResult(c, result)
		c.JSON(http.StatusOK, result)
	case "GetAsset":
		assetID := strings.TrimSpace(clientRequest.ID)
		if assetID == "" {
			respondAssetError(c, http.StatusBadRequest, "Id is required")
			return
		}
		request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, baseURL+"/v1/sd/assets/"+url.PathEscape(assetID), nil)
		if err != nil {
			respondAssetError(c, http.StatusInternalServerError, "failed to build Byteplus asset query: "+err.Error())
			return
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+apiKey)
		response, err := client.Do(request)
		if err != nil {
			respondAssetError(c, http.StatusBadGateway, "Byteplus asset query failed: "+err.Error())
			return
		}
		defer response.Body.Close()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			respondAssetError(c, http.StatusBadGateway, "failed to read Byteplus asset detail: "+err.Error())
			return
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			respondAssetError(c, response.StatusCode, string(responseBody))
			return
		}
		var assetResponse dto.SeedanceV3AssetResponse
		if err := common.Unmarshal(responseBody, &assetResponse); err != nil {
			respondAssetError(c, http.StatusBadGateway, "invalid Byteplus asset detail: "+err.Error())
			return
		}
		if !assetResponse.Success || assetResponse.Data.BaseResp == nil || assetResponse.Data.BaseResp.StatusCode != 0 {
			respondAssetError(c, http.StatusBadGateway, "Byteplus asset query failed")
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
	result := gin.H{
		"code":    status,
		"message": message,
	}
	setSeedanceV3AssetLogResult(c, result)
	c.JSON(status, result)
}

func setSeedanceV3AssetLogResult(c *gin.Context, result any) {
	c.Set(seedanceV3AssetLogResultKey, result)
}

func recordSeedanceV3AssetUploadLog(c *gin.Context, modelName string, requestBody []byte, startedAt time.Time) {
	if len(requestBody) == 0 {
		if value, exists := common.GetContextKey(c, common.KeyRequestBody); exists {
			requestBody, _ = value.([]byte)
		}
	}
	var request struct {
		Model     string `json:"model"`
		Name      string `json:"name"`
		AssetType string `json:"AssetType"`
	}
	_ = common.Unmarshal(requestBody, &request)
	if request.AssetType == "" && len(requestBody) > 0 {
		request.AssetType = "Image"
	}
	if modelName == "" {
		modelName = request.Model
	}

	result, _ := c.Get(seedanceV3AssetLogResultKey)
	normalizedResult := normalizeSeedanceV3AssetLogResult(result)
	resultPreview := common.LocalLogPreview(common.GetJsonString(normalizedResult))
	status := c.Writer.Status()
	success := status >= http.StatusOK && status < http.StatusMultipleChoices
	content := "Seedance asset upload succeeded"
	if !success {
		content = "Seedance asset upload failed"
	}
	if resultPreview != "" && resultPreview != "null" {
		content += ": " + resultPreview
	}

	other := map[string]interface{}{
		"request_path":      c.Request.URL.Path,
		"asset_operation":   "create",
		"asset_status_code": status,
		"asset_success":     success,
		"asset_result":      resultPreview,
	}
	if request.Name != "" {
		other["asset_name"] = request.Name
	}
	if request.AssetType != "" {
		other["asset_type"] = request.AssetType
	}
	if assetID := seedanceV3AssetResultID(normalizedResult); assetID != "" {
		other["asset_id"] = assetID
	}

	model.RecordAssetLog(c, common.GetContextKeyInt(c, constant.ContextKeyUserId), model.RecordAssetLogParams{
		ChannelId:      common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		ModelName:      modelName,
		TokenName:      c.GetString("token_name"),
		Content:        content,
		TokenId:        common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		UseTimeSeconds: int(time.Since(startedAt).Seconds()),
		Group:          common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		Other:          other,
	})
}

func normalizeSeedanceV3AssetLogResult(result any) any {
	if result == nil {
		return nil
	}
	var encoded []byte
	switch typed := result.(type) {
	case []byte:
		encoded = typed
	default:
		var err error
		encoded, err = common.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
	}
	var normalized any
	if err := common.Unmarshal(encoded, &normalized); err != nil {
		return common.LocalLogPreview(string(encoded))
	}
	return normalized
}

func seedanceV3AssetResultID(result any) string {
	object, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"id", "Id", "ID"} {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if data, ok := object["data"]; ok {
		return seedanceV3AssetResultID(data)
	}
	return ""
}
