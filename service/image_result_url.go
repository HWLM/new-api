package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/objectstore"

	"github.com/gin-gonic/gin"
)

const imageResultDefaultPrefix = "ai-images"
const imageResultSupportedModel = "gpt-image-2"

type imageResultObjectStoreConfig struct {
	Provider       string `json:"provider,omitempty"`
	AccessKey      string `json:"accessKey,omitempty"`
	SecretKey      string `json:"secretKey,omitempty"`
	BucketName     string `json:"bucketName,omitempty"`
	Region         string `json:"region,omitempty"`
	PublicURL      string `json:"publicUrl,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	Prefix         string `json:"prefix,omitempty"`
	SessionToken   string `json:"sessionToken,omitempty"`
	ForcePathStyle bool   `json:"forcePathStyle,omitempty"`
}

// EnsureImageResponseBodyURLs rewrites OpenAI image response data entries from
// b64_json to url when the client explicitly asked for URL output.
func EnsureImageResponseBodyURLs(c *gin.Context, body []byte, request *dto.ImageRequest) ([]byte, bool, error) {
	if !shouldUploadImageResult(request) || len(body) == 0 {
		return body, false, nil
	}

	var imageResp dto.ImageResponse
	if err := common.Unmarshal(body, &imageResp); err != nil {
		return body, false, nil
	}
	if len(imageResp.Data) == 0 {
		return body, false, nil
	}

	changed, err := EnsureImageResponseURLs(c, &imageResp, request)
	if err != nil || !changed {
		return body, false, nil
	}

	var raw map[string]json.RawMessage
	if err := common.Unmarshal(body, &raw); err != nil {
		return nil, false, err
	}
	data, err := common.Marshal(imageResp.Data)
	if err != nil {
		return nil, false, err
	}
	raw["data"] = data
	updated, err := common.Marshal(raw)
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
}

func EnsureImageResponseURLs(c *gin.Context, response *dto.ImageResponse, request *dto.ImageRequest) (bool, error) {
	if !shouldUploadImageResult(request) || response == nil {
		return false, nil
	}
	uploader, prefix, err := getImageResultUploader()
	if err != nil {
		logImageResultUploadError(c, fmt.Sprintf("image result upload failed: err=%v", err))
		return false, nil
	}

	changed := false
	for i := range response.Data {
		sourceField, imageData, ok, _ := imageResultUploadSource(response.Data[i])
		if !ok {
			continue
		}
		imageBytes, contentType, err := decodeImageResultBase64(imageData)
		if err != nil {
			logImageResultUploadError(c, fmt.Sprintf("image result upload failed: source=%s err=%v", sourceField, err))
			continue
		}
		contentType = imageResultContentTypeForRequest(request, contentType)
		key := buildImageResultObjectKey(prefix, contentType)
		ctx := context.Background()
		if c != nil && c.Request != nil {
			ctx = c.Request.Context()
		}
		resultURL, err := uploader.Upload(ctx, objectstore.Object{
			Key:         key,
			Content:     imageBytes,
			ContentType: contentType,
		})
		if err != nil {
			logImageResultUploadError(c, fmt.Sprintf("image result upload failed: key=%q content_type=%q bytes=%d err=%v", key, contentType, len(imageBytes), err))
			continue
		}
		response.Data[i].Url = resultURL
		response.Data[i].B64Json = ""
		changed = true
	}
	return changed, nil
}

func logImageResultUploadError(c *gin.Context, msg string) {
	if c == nil {
		logger.LogError(nil, msg)
		return
	}
	logger.LogError(c, msg)
}

func imageResultUploadSource(data dto.ImageData) (string, string, bool, string) {
	existingURL := strings.TrimSpace(data.Url)
	if isRemoteImageResultURL(existingURL) {
		return "", "", false, "already_has_remote_url"
	}
	if existingURL != "" {
		return "url_base64", existingURL, true, ""
	}
	b64 := strings.TrimSpace(data.B64Json)
	if b64 == "" {
		return "", "", false, "empty_image_data"
	}
	return "b64_json", b64, true, ""
}

func isRemoteImageResultURL(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://")
}

func shouldUploadImageResult(request *dto.ImageRequest) bool {
	return request != nil &&
		isImageResultObjectStoreEnabled() &&
		isImageResultSupportedModel(request.Model) &&
		strings.EqualFold(strings.TrimSpace(request.ResponseFormat), "url")
}

func isImageResultObjectStoreEnabled() bool {
	return strings.EqualFold(firstNonEmptyOption("ImageResultObjectStoreEnabled"), "true")
}

func isImageResultSupportedModel(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return normalized == imageResultSupportedModel || strings.HasPrefix(normalized, imageResultSupportedModel+"-")
}

func imageResultContentTypeForRequest(request *dto.ImageRequest, fallback string) string {
	format := imageResultOutputFormat(request)
	switch format {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		if strings.TrimSpace(fallback) == "" {
			return "image/png"
		}
		return fallback
	}
}

func imageResultOutputFormat(request *dto.ImageRequest) string {
	if request == nil {
		return ""
	}
	if format := imageResultFormatFromRaw(request.OutputFormat); format != "" {
		return format
	}
	for _, key := range []string{"format", "output_format"} {
		if raw, ok := request.Extra[key]; ok {
			if format := imageResultFormatFromRaw(raw); format != "" {
				return format
			}
		}
	}
	return ""
}

func imageResultFormatFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var format string
	if err := common.Unmarshal(raw, &format); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(format))
}

func getImageResultUploader() (objectstore.Uploader, string, error) {
	settings := getImageResultObjectStoreConfig()
	provider := strings.ToLower(strings.TrimSpace(settings.Provider))
	if provider == "" {
		provider = "s3"
	}
	var (
		uploader objectstore.Uploader
		err      error
	)
	switch provider {
	case "", "s3", "aws_s3":
		uploader, err = getImageResultS3Uploader(settings)
	default:
		err = fmt.Errorf("unsupported image result object store provider: %s", provider)
	}
	if err != nil {
		return nil, "", err
	}
	prefix := strings.Trim(strings.TrimSpace(settings.Prefix), "/")
	if prefix == "" {
		prefix = imageResultDefaultPrefix
	}
	return uploader, prefix, nil
}

func getImageResultS3Uploader(settings imageResultObjectStoreConfig) (objectstore.Uploader, error) {
	cfg := objectstore.S3Config{
		Bucket:          strings.TrimSpace(settings.BucketName),
		Region:          strings.TrimSpace(settings.Region),
		AccessKeyID:     strings.TrimSpace(settings.AccessKey),
		SecretAccessKey: strings.TrimSpace(settings.SecretKey),
		SessionToken:    strings.TrimSpace(settings.SessionToken),
		Endpoint:        strings.TrimSpace(settings.Endpoint),
		PublicBaseURL:   strings.TrimSpace(settings.PublicURL),
		ForcePathStyle:  settings.ForcePathStyle,
	}
	if cfg.AccessKeyID == "" {
		cfg.AccessKeyID = strings.TrimSpace(common.GetEnvOrDefaultString("AWS_ACCESS_KEY_ID", ""))
	}
	if cfg.SecretAccessKey == "" {
		cfg.SecretAccessKey = strings.TrimSpace(common.GetEnvOrDefaultString("AWS_SECRET_ACCESS_KEY", ""))
	}
	if cfg.SessionToken == "" {
		cfg.SessionToken = strings.TrimSpace(common.GetEnvOrDefaultString("AWS_SESSION_TOKEN", ""))
	}
	if cfg.Bucket == "" || cfg.Region == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("image result url fallback requires IMAGE_RESULT_AWS_S3_BUCKET, IMAGE_RESULT_AWS_S3_REGION, IMAGE_RESULT_AWS_S3_ACCESS_KEY_ID, and IMAGE_RESULT_AWS_S3_SECRET_ACCESS_KEY")
	}
	return objectstore.NewS3Uploader(cfg, nil)
}

func getImageResultObjectStoreConfig() imageResultObjectStoreConfig {
	cfg := imageResultObjectStoreConfig{
		Provider:       firstNonEmptyOption("ImageResultObjectStore.provider", "image_result.object_store.provider"),
		AccessKey:      firstNonEmptyOption("ImageResultObjectStore.accessKey", "image_result.object_store.accessKey"),
		SecretKey:      firstNonEmptyOption("ImageResultObjectStore.secretKey", "image_result.object_store.secretKey"),
		BucketName:     firstNonEmptyOption("ImageResultObjectStore.bucketName", "image_result.object_store.bucketName"),
		Region:         firstNonEmptyOption("ImageResultObjectStore.region", "image_result.object_store.region"),
		PublicURL:      firstNonEmptyOption("ImageResultObjectStore.publicUrl", "image_result.object_store.publicUrl"),
		Endpoint:       firstNonEmptyOption("ImageResultObjectStore.endpoint", "image_result.object_store.endpoint"),
		Prefix:         firstNonEmptyOption("ImageResultObjectStore.prefix", "image_result.object_store.prefix"),
		SessionToken:   firstNonEmptyOption("ImageResultObjectStore.sessionToken", "image_result.object_store.sessionToken"),
		ForcePathStyle: firstNonEmptyOption("ImageResultObjectStore.forcePathStyle", "image_result.object_store.forcePathStyle") == "true",
	}

	if raw := firstNonEmptyOption("ImageResultObjectStore", "image_result.object_store"); strings.TrimSpace(raw) != "" {
		var jsonCfg imageResultObjectStoreConfig
		if err := common.Unmarshal([]byte(raw), &jsonCfg); err == nil {
			mergeImageResultObjectStoreConfig(&cfg, jsonCfg)
		}
	}

	mergeImageResultObjectStoreConfig(&cfg, imageResultObjectStoreConfig{
		Provider:       common.GetEnvOrDefaultString("IMAGE_RESULT_OBJECT_STORE_PROVIDER", ""),
		AccessKey:      common.GetEnvOrDefaultString("IMAGE_RESULT_AWS_S3_ACCESS_KEY_ID", ""),
		SecretKey:      common.GetEnvOrDefaultString("IMAGE_RESULT_AWS_S3_SECRET_ACCESS_KEY", ""),
		BucketName:     common.GetEnvOrDefaultString("IMAGE_RESULT_AWS_S3_BUCKET", ""),
		Region:         common.GetEnvOrDefaultString("IMAGE_RESULT_AWS_S3_REGION", ""),
		PublicURL:      common.GetEnvOrDefaultString("IMAGE_RESULT_AWS_S3_PUBLIC_BASE_URL", ""),
		Endpoint:       common.GetEnvOrDefaultString("IMAGE_RESULT_AWS_S3_ENDPOINT", ""),
		Prefix:         firstNonEmptyEnv("IMAGE_RESULT_OBJECT_PREFIX", "IMAGE_RESULT_AWS_S3_PREFIX"),
		SessionToken:   common.GetEnvOrDefaultString("IMAGE_RESULT_AWS_S3_SESSION_TOKEN", ""),
		ForcePathStyle: common.GetEnvOrDefaultBool("IMAGE_RESULT_AWS_S3_FORCE_PATH_STYLE", false),
	})

	return cfg
}

func mergeImageResultObjectStoreConfig(dst *imageResultObjectStoreConfig, src imageResultObjectStoreConfig) {
	if strings.TrimSpace(dst.Provider) == "" {
		dst.Provider = src.Provider
	}
	if strings.TrimSpace(dst.AccessKey) == "" {
		dst.AccessKey = src.AccessKey
	}
	if strings.TrimSpace(dst.SecretKey) == "" {
		dst.SecretKey = src.SecretKey
	}
	if strings.TrimSpace(dst.BucketName) == "" {
		dst.BucketName = src.BucketName
	}
	if strings.TrimSpace(dst.Region) == "" {
		dst.Region = src.Region
	}
	if strings.TrimSpace(dst.PublicURL) == "" {
		dst.PublicURL = src.PublicURL
	}
	if strings.TrimSpace(dst.Endpoint) == "" {
		dst.Endpoint = src.Endpoint
	}
	if strings.TrimSpace(dst.Prefix) == "" {
		dst.Prefix = src.Prefix
	}
	if strings.TrimSpace(dst.SessionToken) == "" {
		dst.SessionToken = src.SessionToken
	}
	dst.ForcePathStyle = dst.ForcePathStyle || src.ForcePathStyle
}

func firstNonEmptyOption(keys ...string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	for _, key := range keys {
		if value := strings.TrimSpace(common.OptionMap[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(common.GetEnvOrDefaultString(key, "")); value != "" {
			return value
		}
	}
	return ""
}

func decodeImageResultBase64(value string) ([]byte, string, error) {
	data := strings.TrimSpace(value)
	contentType := ""
	if strings.HasPrefix(data, "data:") {
		parts := strings.SplitN(data, ",", 2)
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("invalid image data URI")
		}
		meta := strings.TrimPrefix(parts[0], "data:")
		if semi := strings.Index(meta, ";"); semi >= 0 {
			contentType = meta[:semi]
		} else {
			contentType = meta
		}
		data = parts[1]
	}
	imageBytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, "", fmt.Errorf("decode image b64_json failed: %w", err)
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(imageBytes)
	}
	if !strings.HasPrefix(contentType, "image/") {
		contentType = "image/png"
	}
	return imageBytes, contentType, nil
}

func buildImageResultObjectKey(prefix string, contentType string) string {
	return buildImageResultObjectKeyWithTime(prefix, contentType, time.Now())
}

func buildImageResultObjectKeyWithTime(prefix string, contentType string, now time.Time) string {
	ext := ".png"
	switch strings.ToLower(contentType) {
	case "image/jpeg", "image/jpg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	name := fmt.Sprintf("%d-%s%s", now.UnixNano(), common.GetUUID(), ext)
	dateFolder := now.Format("2006-01-02")
	if prefix == "" {
		return dateFolder + "/" + name
	}
	return strings.Trim(prefix, "/") + "/" + dateFolder + "/" + name
}
