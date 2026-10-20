package service

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/assert"
)

func TestIsTextClassRelayFormat(t *testing.T) {
	cases := []struct {
		format types.RelayFormat
		want   bool
	}{
		{types.RelayFormatOpenAI, true},
		{types.RelayFormatClaude, true},
		{types.RelayFormatGemini, true},
		{types.RelayFormatOpenAIResponses, true},
		{types.RelayFormatOpenAIResponsesCompaction, true},
		{types.RelayFormat("image"), false},
		{types.RelayFormat("audio"), false},
		{types.RelayFormat("task"), false},
		{types.RelayFormat(""), false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, IsTextClassRelayFormat(tc.format), "format=%s", tc.format)
	}
}

func TestIsRefusalAdminRejectReason(t *testing.T) {
	assert.True(t, IsRefusalAdminRejectReason("claude_stop_reason=refusal"),
		"canonical Claude refusal reason must count")
	assert.False(t, IsRefusalAdminRejectReason(""),
		"empty admin reject reason is not a refusal")
	assert.False(t, IsRefusalAdminRejectReason("openai_finish_reason=content_filter"),
		"other provider reasons must NOT count as refusal")
	assert.False(t, IsRefusalAdminRejectReason("gemini_block_reason=SAFETY"),
		"gemini block is not a refusal under this feature")
	assert.False(t, IsRefusalAdminRejectReason("claude_refusal_no_output"),
		"the billing-exempt reason string is unrelated and must not count")
}
