package dto

import (
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeMessagePreservesOutputConfig(t *testing.T) {
	raw := []byte(`{"role":"system","content":[],"output_config":{"effort":"low"}}`)

	var msg ClaudeMessage
	require.NoError(t, kitutil.Unmarshal(raw, &msg))

	encoded, err := kitutil.Marshal(msg)
	require.NoError(t, err)

	assert.Equal(t, "system", gjson.GetBytes(encoded, "role").String())
	assert.Equal(t, "low", gjson.GetBytes(encoded, "output_config.effort").String())
}

func TestClaudeMessageOmitsEmptyOutputConfig(t *testing.T) {
	encoded, err := kitutil.Marshal(ClaudeMessage{Role: "user", Content: "hi"})
	require.NoError(t, err)

	assert.False(t, gjson.GetBytes(encoded, "output_config").Exists())
}

func TestClaudeRequestPreservesMessageOutputConfigRoundTrip(t *testing.T) {
	raw := []byte(`{
		"model":"claude-opus-4-8",
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"system","content":[],"output_config":{"effort":"low"}}
		]
	}`)

	var req ClaudeRequest
	require.NoError(t, kitutil.Unmarshal(raw, &req))

	encoded, err := kitutil.Marshal(req)
	require.NoError(t, err)

	assert.False(t, gjson.GetBytes(encoded, "messages.0.output_config").Exists())
	assert.Equal(t, "low", gjson.GetBytes(encoded, "messages.1.output_config.effort").String())
}
