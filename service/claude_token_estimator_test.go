package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateClaudeInputTokens(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "single short user message",
			body: `{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"count"}]}`,
			want: 4,
		},
		{
			name: "system and user message",
			body: `{"model":"claude-haiku-4-5","system":"You are helpful.","messages":[{"role":"user","content":"hello"}]}`,
			want: 9,
		},
		{
			name: "tool schema",
			body: `{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"count"}],"tools":[{"name":"Bash","description":"Execute a shell command on the host","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}]}`,
			want: 43,
		},
		{
			name: "web search tool",
			body: `{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"news"}],"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":3}]}`,
			want: 7,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var request dto.ClaudeRequest
			require.NoError(t, common.Unmarshal([]byte(test.body), &request))

			assert.Equal(t, test.want, EstimateClaudeInputTokens(&request))
		})
	}
}

func TestEstimateClaudeInputTokensHandlesNil(t *testing.T) {
	assert.Zero(t, EstimateClaudeInputTokens(nil))
	assert.Zero(t, EstimateClaudeInputTokens(&dto.ClaudeRequest{}))
}

func TestNormalizeClaudeRequestToolsPreservesNonWebSearchServerTool(t *testing.T) {
	request := &dto.ClaudeRequest{Tools: []any{
		map[string]any{
			"type":         "computer_20250124",
			"name":         "computer",
			"description":  "Operate a virtual desktop",
			"input_schema": map[string]any{"type": "object"},
		},
	}}

	normalizeClaudeRequestTools(request)

	tools, ok := request.Tools.([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	assert.IsType(t, &dto.Tool{}, tools[0])
}
