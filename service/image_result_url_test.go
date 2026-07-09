package service

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureImageResponseURLsSkipsUnsupportedImageModel(t *testing.T) {
	response := &dto.ImageResponse{
		Data: []dto.ImageData{
			{B64Json: "not-used"},
		},
	}
	request := &dto.ImageRequest{
		Model:          "dall-e-3",
		ResponseFormat: "url",
	}

	changed, err := EnsureImageResponseURLs(nil, response, request)

	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, "not-used", response.Data[0].B64Json)
	assert.Empty(t, response.Data[0].Url)
}

func TestShouldUploadImageResultAllowsGPTImage2Aliases(t *testing.T) {
	restore := setImageResultObjectStoreEnabledForTest("true")
	defer restore()

	request := &dto.ImageRequest{
		Model:          "gpt-image-2-codex",
		ResponseFormat: "url",
	}

	assert.True(t, shouldUploadImageResult(request))
}

func TestShouldUploadImageResultSkipsWhenObjectStoreDisabled(t *testing.T) {
	restore := setImageResultObjectStoreEnabledForTest("false")
	defer restore()

	request := &dto.ImageRequest{
		Model:          "gpt-image-2-codex",
		ResponseFormat: "url",
	}

	assert.False(t, shouldUploadImageResult(request))
}

func TestImageResultUploadSourceUsesBase64FromURLField(t *testing.T) {
	source, value, ok, reason := imageResultUploadSource(dto.ImageData{
		Url: "data:image/png;base64,aW1hZ2U=",
	})

	require.True(t, ok)
	assert.Equal(t, "url_base64", source)
	assert.Equal(t, "data:image/png;base64,aW1hZ2U=", value)
	assert.Empty(t, reason)
}

func TestImageResultUploadSourceSkipsRemoteURL(t *testing.T) {
	source, value, ok, reason := imageResultUploadSource(dto.ImageData{
		Url:     "https://cdn.example.com/image.png",
		B64Json: "not-used",
	})

	assert.False(t, ok)
	assert.Empty(t, source)
	assert.Empty(t, value)
	assert.Equal(t, "already_has_remote_url", reason)
}

func TestBuildImageResultObjectKeyUsesPrefixAndDateFolder(t *testing.T) {
	key := buildImageResultObjectKeyWithTime("generated-images", "image/png", time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC))

	assert.True(t, strings.HasPrefix(key, "generated-images/2026-07-09/"))
	assert.True(t, strings.HasSuffix(key, ".png"))
}

func setImageResultObjectStoreEnabledForTest(value string) func() {
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{
		"ImageResultObjectStoreEnabled": value,
	}
	common.OptionMapRWMutex.Unlock()

	return func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	}
}
