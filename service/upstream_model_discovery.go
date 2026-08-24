package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

type upstreamModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// DiscoverUpstreamModelsFromInput validates an unpersisted cooperation request
// and discovers models without storing or returning its credentials.
func DiscoverUpstreamModelsFromInput(ctx context.Context, input dto.UpstreamCandidateRequest) ([]string, error) {
	baseURL, err := NormalizeUpstreamBaseURL(input.BaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.APIKey) == "" {
		return nil, errors.New("api_key is required")
	}
	if !IsValidUpstreamChannelType(input.Type) {
		return nil, errors.New("invalid channel type")
	}
	if !SupportsUpstreamModelDiscovery(input.Type) {
		configured := configuredUpstreamModels(input.Models)
		if len(configured) > 0 {
			return configured, nil
		}
		return nil, errors.New("model discovery is not supported for this channel type")
	}
	candidate := &model.UpstreamCandidate{
		Source:            constant.UpstreamSourceSelf,
		Type:              input.Type,
		BaseURL:           strings.TrimSpace(input.BaseURL),
		NormalizedBaseURL: baseURL,
		APIKey:            input.APIKey,
	}
	return DiscoverUpstreamModels(ctx, candidate)
}

// SupportsUpstreamModelDiscovery reports the channel types that expose one of
// the model-list protocols implemented by the cooperation flow.
func SupportsUpstreamModelDiscovery(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeOpenAI,
		constant.ChannelTypeOllama,
		constant.ChannelTypeAnthropic,
		constant.ChannelTypeAli,
		constant.ChannelTypeOpenRouter,
		constant.ChannelTypeTencent,
		constant.ChannelTypeGemini,
		constant.ChannelTypeMoonshot,
		constant.ChannelTypeZhipu_v4,
		constant.ChannelTypePerplexity,
		constant.ChannelTypeLingYiWanWu,
		constant.ChannelTypeCohere,
		constant.ChannelTypeMiniMax,
		constant.ChannelTypeSiliconFlow,
		constant.ChannelTypeMistral,
		constant.ChannelTypeDeepSeek,
		constant.ChannelTypeXinference,
		constant.ChannelTypeXai,
		constant.ChannelTypeSub2API,
		constant.ChannelTypeNewAPI:
		return true
	default:
		return false
	}
}

// DiscoverUpstreamModels fetches only model identifiers and never returns credentials.
func DiscoverUpstreamModels(ctx context.Context, candidate *model.UpstreamCandidate) ([]string, error) {
	if candidate == nil {
		return nil, ErrUpstreamNotFound
	}
	if !IsValidUpstreamChannelType(candidate.Type) {
		return nil, errors.New("invalid channel type")
	}
	if !SupportsUpstreamModelDiscovery(candidate.Type) {
		configured := configuredUpstreamModels(candidate.Models)
		if len(configured) > 0 {
			return configured, nil
		}
		return nil, errors.New("model discovery is not supported for this channel type")
	}
	endpoint := upstreamModelListEndpoint(candidate)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setUpstreamAuthentication(request, candidate)
	client := GetHttpClient()
	if candidate.Source == constant.UpstreamSourceSelf {
		client = GetSSRFProtectedHTTPClient()
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upstream model discovery returned status %d", response.StatusCode)
	}
	var payload upstreamModelListResponse
	if err := common.DecodeJson(io.LimitReader(response.Body, 2<<20), &payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, item := range payload.Data {
		if name := strings.TrimSpace(item.ID); name != "" {
			models = append(models, name)
		}
	}
	for _, item := range payload.Models {
		name := strings.TrimSpace(strings.TrimPrefix(item.Name, "models/"))
		if name != "" {
			models = append(models, name)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("upstream returned no models")
	}
	sort.Strings(models)
	result := models[:0]
	seen := make(map[string]struct{}, len(models))
	for _, name := range models {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func configuredUpstreamModels(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	models := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		modelName := strings.TrimSpace(part)
		if modelName == "" {
			continue
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		models = append(models, modelName)
	}
	return models
}

func upstreamModelListEndpoint(candidate *model.UpstreamCandidate) string {
	baseURL := strings.TrimRight(candidate.NormalizedBaseURL, "/")
	endpoint := baseURL + "/v1/models"
	switch candidate.Type {
	case constant.ChannelTypeOllama:
		endpoint = baseURL + "/api/tags"
	case constant.ChannelTypeAli:
		endpoint = baseURL + "/compatible-mode/v1/models"
	case constant.ChannelTypeZhipu_v4:
		endpoint = baseURL + "/api/paas/v4/models"
	case constant.ChannelTypeGemini:
		endpoint = baseURL + "/v1beta/models"
		if strings.HasSuffix(baseURL, "/v1beta") {
			endpoint = baseURL + "/models"
		}
	default:
		if strings.HasSuffix(baseURL, "/v1") {
			endpoint = baseURL + "/models"
		}
	}
	return endpoint
}

func setUpstreamAuthentication(request *http.Request, candidate *model.UpstreamCandidate) {
	if candidate.Type == constant.ChannelTypeGemini {
		request.Header.Set("x-goog-api-key", candidate.APIKey)
	} else if candidate.Type == constant.ChannelTypeAnthropic {
		request.Header.Set("x-api-key", candidate.APIKey)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else if candidate.Type == constant.ChannelTypeAzure {
		request.Header.Set("api-key", candidate.APIKey)
	} else {
		request.Header.Set("Authorization", "Bearer "+candidate.APIKey)
	}
}
