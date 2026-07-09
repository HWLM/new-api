package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// OpenaiImageHandler handles non-streaming OpenAI image responses
// (generations/edits), returning the parsed usage for billing.
func OpenaiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var usageResp dto.SimpleResponse
	err = common.Unmarshal(responseBody, &usageResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := usageResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	// 写入新的 response body
	if info != nil {
		if imageReq, ok := info.Request.(*dto.ImageRequest); ok && shouldEnsureImageResultURLFallback(info, imageReq) {
			fallbackReq := imageResultURLFallbackRequest(info, imageReq)
			updatedBody, _, err := service.EnsureImageResponseBodyURLs(c, responseBody, fallbackReq)
			if err != nil {
				return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
			}
			responseBody = updatedBody
		}
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	normalizeOpenAIUsage(&usageResp.Usage)
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)
	return &usageResp.Usage, nil
}

func imageResultURLFallbackRequest(info *relaycommon.RelayInfo, request *dto.ImageRequest) *dto.ImageRequest {
	fallbackReq := *request
	fallbackReq.Model = info.OriginModelName
	return &fallbackReq
}

func shouldEnsureImageResultURLFallback(info *relaycommon.RelayInfo, request *dto.ImageRequest) bool {
	return info != nil &&
		request != nil &&
		info.RelayMode == relayconstant.RelayModeImagesGenerations &&
		isGPTImage2Model(info.OriginModelName) &&
		strings.EqualFold(strings.TrimSpace(request.ResponseFormat), "url")
}

func isGPTImage2Model(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return normalized == "gpt-image-2" || strings.HasPrefix(normalized, "gpt-image-2-")
}

// normalizeOpenAIUsage maps the OpenAI Images usage shape (input_tokens /
// output_tokens / input_tokens_details) onto the canonical prompt/completion
// fields. It is used only on the OpenAI image relay paths (generations/edits,
// streaming and non-streaming): the image API never returns prompt_tokens /
// completion_tokens, so the overwrite (=) semantics here are equivalent to the
// previous additive (+=) behavior while avoiding any future double-counting if
// both field sets are ever populated. Do not reuse this on chat/embedding paths
// without revisiting the overwrite semantics.
func normalizeOpenAIUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.InputTokens != 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.CachedCreationTokens = usage.InputTokensDetails.CachedCreationTokens
		usage.PromptTokensDetails.ImageTokens = usage.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.TextTokens = usage.InputTokensDetails.TextTokens
		usage.PromptTokensDetails.AudioTokens = usage.InputTokensDetails.AudioTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}

func OpenaiImageStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid image stream response")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OpenaiImageHandler(c, info, resp)
	}
	if !strings.Contains(contentType, "text/event-stream") {
		return OpenaiImageJSONAsStreamHandler(c, info, resp)
	}
	// Reuse the shared streaming engine (helper.StreamScannerHandler) so the
	// image streaming path gets the same ping keepalive, streaming-timeout
	// watchdog, client-disconnect detection, panic recovery and goroutine
	// cleanup as every other relay stream. The scanner delivers only the
	// "data:" payload, so the SSE "event:" line is rebuilt from the JSON "type"
	// field (real OpenAI image events keep event == type).
	usage := &dto.Usage{}
	var lastStreamData []byte

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		raw := common.StringToByteSlice(data)
		lastStreamData = raw
		if isOpenAIImageStreamErrorEvent(raw) {
			// Record the error as a soft error; the scanner drives the final
			// EndReason. HasErrors() flags the failure for logging/handling.
			sr.Error(fmt.Errorf("%s", extractOpenAIImageStreamErrorMessage(raw)))
		}
		var usageResp dto.SimpleResponse
		if err := common.Unmarshal(raw, &usageResp); err == nil {
			normalizeOpenAIUsage(&usageResp.Usage)
			if service.ValidUsage(&usageResp.Usage) {
				usage = &usageResp.Usage
			}
		}
		if info != nil {
			if imageReq, ok := info.Request.(*dto.ImageRequest); ok && shouldEnsureImageResultURLFallback(info, imageReq) {
				var err error
				raw, err = ensureOpenAIImageStreamChunkURLFallback(c, info, imageReq, raw)
				if err != nil {
					sr.Stop(err)
					return
				}
			}
		}
		writeOpenaiImageStreamChunk(c, raw)
	})

	// StreamScannerHandler consumes the upstream [DONE]; re-emit it so the
	// client still receives a terminal data: [DONE].
	if info != nil && info.StreamStatus != nil && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone {
		helper.Done(c)
	}

	applyUsagePostProcessing(info, usage, lastStreamData)
	return usage, nil
}

// writeOpenaiImageStreamChunk rebuilds the SSE frame for an image stream chunk:
// it emits an "event:" line derived from the JSON "type" field (when present)
// followed by the verbatim "data:" payload, mirroring helper.ResponseChunkData.
func writeOpenaiImageStreamChunk(c *gin.Context, data []byte) {
	var payload struct {
		Type string `json:"type"`
	}
	_ = common.Unmarshal(data, &payload)
	if eventName := strings.TrimSpace(payload.Type); eventName != "" {
		_ = helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventName}, string(data))
		return
	}
	_ = helper.StringData(c, string(data))
}

// isOpenAIImageStreamErrorEvent detects upstream error chunks by JSON content
// only ("type" of error/upstream_error, or a non-empty "error" field). The SSE
// "event:" line is not available here: StreamScannerHandler delivers only the
// "data:" payload. A payload carrying just a "message" key is deliberately NOT
// treated as an error to avoid false positives.
func isOpenAIImageStreamErrorEvent(data []byte) bool {
	if !json.Valid(data) {
		return false
	}
	var payload struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return false
	}
	payloadType := strings.ToLower(strings.TrimSpace(payload.Type))
	return payloadType == "error" || payloadType == "upstream_error" || len(payload.Error) > 0
}

func extractOpenAIImageStreamErrorMessage(data []byte) string {
	if len(data) == 0 || !json.Valid(data) {
		return "upstream image stream returned error event"
	}
	var payload struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return "upstream image stream returned error event"
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return msg
	}
	if len(payload.Error) > 0 {
		var nested struct {
			Message string `json:"message"`
		}
		if err := common.Unmarshal(payload.Error, &nested); err == nil {
			if msg := strings.TrimSpace(nested.Message); msg != "" {
				return msg
			}
		}
		if msg := strings.TrimSpace(common.JsonRawMessageToString(payload.Error)); msg != "" {
			return msg
		}
	}
	return "upstream image stream returned error event"
}

func OpenaiImageJSONAsStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var imageResp dto.ImageResponse
	if err := common.Unmarshal(responseBody, &imageResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	var usageResp dto.SimpleResponse
	_ = common.Unmarshal(responseBody, &usageResp)
	if oaiError := usageResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if info != nil {
		if imageReq, ok := info.Request.(*dto.ImageRequest); ok && shouldEnsureImageResultURLFallback(info, imageReq) {
			fallbackReq := imageResultURLFallbackRequest(info, imageReq)
			updatedBody, changed, err := service.EnsureImageResponseBodyURLs(c, responseBody, fallbackReq)
			if err != nil {
				return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
			}
			if changed {
				responseBody = updatedBody
				if err := common.Unmarshal(responseBody, &imageResp); err != nil {
					return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
				}
			}
		}
	}

	normalizeOpenAIUsage(&usageResp.Usage)
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)

	helper.SetEventStreamHeaders(c)
	c.Status(http.StatusOK)

	created := imageResp.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	if info != nil {
		info.SetFirstResponseTime()
	}
	for _, image := range imageResp.Data {
		payload := map[string]any{
			"type":       "image_generation.completed",
			"created_at": created,
		}
		if image.Url != "" {
			payload["url"] = image.Url
		}
		if image.B64Json != "" {
			payload["b64_json"] = image.B64Json
		}
		if image.RevisedPrompt != "" {
			payload["revised_prompt"] = image.RevisedPrompt
		}
		if service.ValidUsage(&usageResp.Usage) {
			payload["usage"] = usageResp.Usage
		}
		if err := writeOpenaiImageStreamPayload(c, "image_generation.completed", payload); err != nil {
			if info != nil && info.StreamStatus != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			}
			return &usageResp.Usage, nil
		}
	}
	if err := writeOpenaiImageStreamDone(c); err != nil {
		if info != nil && info.StreamStatus != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		}
		return &usageResp.Usage, nil
	}
	if info != nil {
		info.ReceivedResponseCount += len(imageResp.Data)
		if info.StreamStatus == nil {
			info.StreamStatus = relaycommon.NewStreamStatus()
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	}
	return &usageResp.Usage, nil
}

func writeOpenaiImageStreamPayload(c *gin.Context, eventName string, payload any) error {
	data, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	if eventName != "" {
		return helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventName}, string(data))
	}
	return helper.StringData(c, string(data))
}

func ensureOpenAIImageStreamChunkURLFallback(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest, data []byte) ([]byte, error) {
	if !json.Valid(data) {
		return data, nil
	}
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(data, &payload); err != nil {
		return data, nil
	}
	if len(payload["url"]) > 0 {
		return data, nil
	}
	rawB64, ok := payload["b64_json"]
	if !ok || len(rawB64) == 0 {
		return data, nil
	}
	var b64 string
	if err := common.Unmarshal(rawB64, &b64); err != nil || strings.TrimSpace(b64) == "" {
		return data, nil
	}
	fallbackReq := imageResultURLFallbackRequest(info, request)
	imageResp := &dto.ImageResponse{
		Data: []dto.ImageData{{B64Json: b64}},
	}
	changed, err := service.EnsureImageResponseURLs(c, imageResp, fallbackReq)
	if err != nil || !changed || len(imageResp.Data) == 0 || imageResp.Data[0].Url == "" {
		return data, err
	}
	urlData, err := common.Marshal(imageResp.Data[0].Url)
	if err != nil {
		return data, err
	}
	payload["url"] = urlData
	delete(payload, "b64_json")
	return common.Marshal(payload)
}

func writeOpenaiImageStreamDone(c *gin.Context) error {
	return helper.StringData(c, "[DONE]")
}
