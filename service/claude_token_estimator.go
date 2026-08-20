package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const claudeImageEstimateTokens = 1600

// EstimateClaudeInputTokens returns a local estimate for an Anthropic count_tokens request.
func EstimateClaudeInputTokens(request *dto.ClaudeRequest) int {
	if request == nil {
		return 0
	}
	normalizeClaudeRequestTools(request)
	meta := request.GetTokenCountMeta()
	if meta == nil {
		return 0
	}
	return EstimateTokenByModel(request.Model, meta.CombineText) + len(meta.Files)*claudeImageEstimateTokens
}

// normalizeClaudeRequestTools converts JSON-decoded tool maps into the DTO types used by GetTokenCountMeta.
func normalizeClaudeRequestTools(request *dto.ClaudeRequest) {
	if request == nil || request.Tools == nil {
		return
	}
	rawTools, ok := request.Tools.([]any)
	if !ok {
		return
	}

	normalized := make([]any, 0, len(rawTools))
	for _, rawTool := range rawTools {
		switch rawTool.(type) {
		case *dto.Tool, dto.Tool, *dto.ClaudeWebSearchTool, dto.ClaudeWebSearchTool:
			normalized = append(normalized, rawTool)
			continue
		}

		toolMap, ok := rawTool.(map[string]any)
		if !ok {
			continue
		}
		data, err := common.Marshal(toolMap)
		if err != nil {
			continue
		}
		if toolType, ok := toolMap["type"].(string); ok && strings.HasPrefix(toolType, "web_search") {
			var webSearchTool dto.ClaudeWebSearchTool
			if err := common.Unmarshal(data, &webSearchTool); err == nil && webSearchTool.Type != "" {
				normalized = append(normalized, &webSearchTool)
			}
			continue
		}

		var tool dto.Tool
		if err := common.Unmarshal(data, &tool); err == nil && tool.Name != "" {
			normalized = append(normalized, &tool)
		}
	}
	request.Tools = normalized
}
