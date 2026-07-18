package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// SeedanceV3RequestConvert 把 /api/v3/contents/generations/tasks 路径下的两类请求都归一成
// TaskSubmitReq，让下游 Distribute + adapter 走同一条链路：
//
//   - dreamina-seedance-2-0-hc：走原逻辑，另存强类型请求到 ContextKeySeedanceV3Request，
//     sdrealmax adapter 从上下文取回并转换为 /v1/video/generate 的上游请求（会做素材上传）。
//   - 其他模型（如 doubao-seedance-2-0-filter-off）：不做业务转换，把整个原始 body 挂到
//     TaskSubmitReq.Metadata；doubao adapter 通过 Metadata overlay 恢复完整原始字段，
//     URL 保持 /api/v3/contents/generations/tasks 透传给上游（不上传素材）。
func SeedanceV3RequestConvert() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		// 用 map 读取以保留所有客户端字段（tools/video_url/audio_url/resolution/ratio/... 都不丢）
		var rawMap map[string]any
		if err := common.UnmarshalBodyReusable(c, &rawMap); err != nil {
			c.Next()
			return
		}
		modelName, _ := rawMap["model"].(string)
		if !dto.IsSeedanceV3UnifiedModel(modelName) {
			abortWithOpenAiMessage(c, http.StatusBadRequest, "Seedance V3 does not support model "+modelName)
			return
		}
		promptParts, images := extractContentTextAndImages(rawMap)

		unifiedRequest := relaycommon.TaskSubmitReq{
			Model:  modelName,
			Prompt: strings.Join(promptParts, "\n"),
			Images: images,
		}
		if rawDuration, exists := rawMap["duration"]; exists {
			d, ok := extractIntField(rawDuration)
			if !ok {
				abortWithOpenAiMessage(c, http.StatusBadRequest, "duration must be an integer")
				return
			}
			// Doubao uses -1 as an upstream sentinel. Keep it only in Metadata so the
			// generic billing-duration validator never treats it as a multiplier.
			if d != -1 || modelName == dto.SeedanceV3ModelName {
				unifiedRequest.Duration = d
			}
		}

		if modelName == dto.SeedanceV3ModelName {
			// SeedanceV3：再解析一次成强类型，供 sdrealmax adapter 从 ContextKey 拿回
			var seedanceReq dto.SeedanceV3VideoRequest
			if err := common.UnmarshalBodyReusable(c, &seedanceReq); err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, "Invalid Seedance V3 request body")
				return
			}
			c.Set(string(constant.ContextKeySeedanceV3Request), seedanceReq)
		} else {
			// 非 SeedanceV3：完整原始 body 挂到 Metadata，doubao adapter 会用它覆盖 requestPayload 各字段
			unifiedRequest.Metadata = rawMap
		}

		body, err := common.Marshal(unifiedRequest)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "Failed to convert request body")
			return
		}

		common.CleanupBodyStorage(c)
		c.Set(common.KeyRequestBody, body)
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
		c.Next()
	}
}

// extractContentTextAndImages 从原始 doubao/seedance shape 的 content 数组里挑出
// 文本片段和 image_url 的 URL，用来填充 TaskSubmitReq 的 Prompt 与 Images 字段。
// video_url / audio_url / 其他自定义类型不在这里处理——它们随原始 body 一起进入 Metadata，
// 由下游 adapter（doubao）通过 metadata overlay 送回上游。
func extractContentTextAndImages(rawMap map[string]any) ([]string, []string) {
	contentAny, ok := rawMap["content"].([]any)
	if !ok {
		return nil, nil
	}
	texts := make([]string, 0, len(contentAny))
	images := make([]string, 0, len(contentAny))
	for _, item := range contentAny {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "text":
			if s, _ := m["text"].(string); strings.TrimSpace(s) != "" {
				texts = append(texts, strings.TrimSpace(s))
			}
		case "image_url":
			if iu, _ := m["image_url"].(map[string]any); iu != nil {
				if url, _ := iu["url"].(string); strings.TrimSpace(url) != "" {
					images = append(images, strings.TrimSpace(url))
				}
			}
		}
	}
	return texts, images
}

func extractIntField(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < -1 || n > relaycommon.MaxTaskDurationSeconds {
			return 0, false
		}
		return int(n), true
	case int:
		if n < -1 || n > relaycommon.MaxTaskDurationSeconds {
			return 0, false
		}
		return n, true
	case int64:
		if n < -1 || n > relaycommon.MaxTaskDurationSeconds {
			return 0, false
		}
		return int(n), true
	case json.Number:
		if i, err := strconv.ParseInt(string(n), 10, 64); err == nil && i >= -1 && i <= relaycommon.MaxTaskDurationSeconds {
			return int(i), true
		}
	case string:
		if out, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil && out >= -1 && out <= relaycommon.MaxTaskDurationSeconds {
			return int(out), true
		}
	}
	return 0, false
}
