package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertImageGenerationRequestOmitsResponseFormatForUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
	}
	request := dto.ImageRequest{
		Model:          "gpt-image-2",
		Prompt:         "draw a cat",
		ResponseFormat: "url",
	}

	converted, err := (&Adaptor{}).ConvertImageRequest(c, info, request)
	require.NoError(t, err)
	convertedRequest, ok := converted.(dto.ImageRequest)
	require.True(t, ok)
	require.Empty(t, convertedRequest.ResponseFormat)
	require.Equal(t, "url", request.ResponseFormat)

	jsonData, err := common.Marshal(converted)
	require.NoError(t, err)
	require.NotContains(t, string(jsonData), "response_format")
}
