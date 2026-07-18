package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

// SeedanceV3AssetRequestConvert 用于 /v3/open/CreateAsset 与 /v3/open/GetAsset 两个素材接口路由。
// 它只做一件事：从请求 body 里把 model 字段提出来（wetoken V3 文档要求 model 必填），
// 让下游 Distribute 能按 model 找到目标渠道。body 本身不做任何改写，随后完整透传给上游。
func SeedanceV3AssetRequestConvert() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		var raw map[string]any
		if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, "invalid asset request body: "+err.Error())
			return
		}
		modelName, _ := raw["model"].(string)
		if modelName == "" {
			abortWithOpenAiMessage(c, http.StatusBadRequest, "model is required for asset request")
			return
		}
		if !dto.IsSeedanceV3UnifiedModel(modelName) {
			abortWithOpenAiMessage(c, http.StatusBadRequest, "Seedance V3 assets do not support model "+modelName)
			return
		}

		// 把 body 原样保留在 c.Request.Body 里，让 controller 能拿到原始 bytes 直接透传。
		bodyBytes, err := common.Marshal(raw)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "failed to re-encode asset request body")
			return
		}
		c.Set(common.KeyRequestBody, bodyBytes)
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		c.Request.ContentLength = int64(len(bodyBytes))
		c.Next()
	}
}
