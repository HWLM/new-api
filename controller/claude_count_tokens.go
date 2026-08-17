package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ClaudeCountTokens estimates Anthropic input tokens locally without selecting a channel or consuming quota.
func ClaudeCountTokens(c *gin.Context) {
	// Anthropic-compatible clients may omit Content-Type on probe requests.
	// This endpoint only accepts JSON, so parse the body as JSON by default.
	c.Request.Header.Set("Content-Type", "application/json")
	var request dto.ClaudeRequest
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		logger.LogWarn(c.Request.Context(), "Claude count_tokens received an invalid body; returning zero estimate: "+err.Error())
		c.JSON(http.StatusOK, gin.H{"input_tokens": 0})
		return
	}

	inputTokens := service.EstimateClaudeInputTokens(&request)
	logger.LogInfo(c.Request.Context(), fmt.Sprintf(
		"Claude count_tokens estimated locally: model=%s input_tokens=%d",
		request.Model,
		inputTokens,
	))
	c.JSON(http.StatusOK, gin.H{"input_tokens": inputTokens})
}
