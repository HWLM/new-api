package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/assert"
)

func TestShouldEnsureImageResultURLFallbackAllowsGPTImage2Aliases(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-2-codex",
		RelayMode:       relayconstant.RelayModeImagesGenerations,
	}
	request := &dto.ImageRequest{ResponseFormat: "url"}

	assert.True(t, shouldEnsureImageResultURLFallback(info, request))
}
