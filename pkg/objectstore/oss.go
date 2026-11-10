package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

// OSSConfig 描述阿里雲 OSS 上傳所需的配置。
type OSSConfig struct {
	Bucket          string
	Region          string
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	Endpoint        string
	PublicBaseURL   string
	UseCName        bool
	UsePathStyle    bool
}

// OSSUploader 通過阿里雲 OSS SDK 上傳對象。
type OSSUploader struct {
	config OSSConfig
	client *oss.Client
}

// NewOSSUploader 構造一個 OSS 上傳器。
func NewOSSUploader(config OSSConfig) (*OSSUploader, error) {
	if config.Bucket == "" {
		return nil, fmt.Errorf("oss bucket is required")
	}
	if config.AccessKeyID == "" || config.AccessKeySecret == "" {
		return nil, fmt.Errorf("oss access key id and access key secret are required")
	}
	if config.Region == "" && config.Endpoint == "" {
		return nil, fmt.Errorf("oss region or endpoint is required")
	}
	config.Endpoint = strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	config.PublicBaseURL = strings.TrimRight(strings.TrimSpace(config.PublicBaseURL), "/")

	cfg := oss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			config.AccessKeyID,
			config.AccessKeySecret,
			config.SecurityToken,
		))
	if config.Region != "" {
		cfg = cfg.WithRegion(config.Region)
	}
	if config.Endpoint != "" {
		cfg = cfg.WithEndpoint(config.Endpoint)
	}
	if config.UseCName {
		cfg = cfg.WithUseCName(true)
	}
	if config.UsePathStyle {
		cfg = cfg.WithUsePathStyle(true)
	}

	client := oss.NewClient(cfg)
	return &OSSUploader{
		config: config,
		client: client,
	}, nil
}

// Upload 把對象寫入 OSS 並返回可訪問的 URL。
func (u *OSSUploader) Upload(ctx context.Context, object Object) (string, error) {
	if object.Key == "" {
		return "", fmt.Errorf("object key is required")
	}
	if len(object.Content) == 0 {
		return "", fmt.Errorf("object content is empty")
	}
	if object.ContentType == "" {
		object.ContentType = "application/octet-stream"
	}

	req := &oss.PutObjectRequest{
		Bucket:      oss.Ptr(u.config.Bucket),
		Key:         oss.Ptr(object.Key),
		Body:        bytes.NewReader(object.Content),
		ContentType: oss.Ptr(object.ContentType),
	}
	if _, err := u.client.PutObject(ctx, req); err != nil {
		return "", fmt.Errorf("upload object to oss failed: %w", err)
	}

	return u.buildPublicURL(object.Key), nil
}

func (u *OSSUploader) buildPublicURL(key string) string {
	escaped := PathEscapeSegments(key)
	if u.config.PublicBaseURL != "" {
		return u.config.PublicBaseURL + "/" + escaped
	}
	if u.config.Endpoint != "" {
		parsed, err := url.Parse(u.config.Endpoint)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			if u.config.UseCName || u.config.UsePathStyle {
				return strings.TrimRight(parsed.String(), "/") + "/" + escaped
			}
			parsed.Host = u.config.Bucket + "." + parsed.Host
			return strings.TrimRight(parsed.String(), "/") + "/" + escaped
		}
	}
	if u.config.Region != "" {
		return fmt.Sprintf("https://%s.oss-%s.aliyuncs.com/%s", u.config.Bucket, u.config.Region, escaped)
	}
	return escaped
}

// PathEscapeSegments 對對象 Key 按分段進行 URL 轉義。
func PathEscapeSegments(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
