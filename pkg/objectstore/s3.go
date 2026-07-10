package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type S3Config struct {
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Endpoint        string
	PublicBaseURL   string
	ForcePathStyle  bool
}

type S3Uploader struct {
	config S3Config
	client *http.Client
}

func NewS3Uploader(config S3Config, client *http.Client) (*S3Uploader, error) {
	if config.Bucket == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}
	if config.Region == "" {
		return nil, fmt.Errorf("s3 region is required")
	}
	if config.AccessKeyID == "" || config.SecretAccessKey == "" {
		return nil, fmt.Errorf("s3 access key and secret key are required")
	}
	config.Endpoint = strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	config.PublicBaseURL = strings.TrimRight(strings.TrimSpace(config.PublicBaseURL), "/")
	if client == nil {
		client = http.DefaultClient
	}
	return &S3Uploader{
		config: config,
		client: client,
	}, nil
}

func (u *S3Uploader) Upload(ctx context.Context, object Object) (string, error) {
	if object.Key == "" {
		return "", fmt.Errorf("object key is required")
	}
	if len(object.Content) == 0 {
		return "", fmt.Errorf("object content is empty")
	}
	if object.ContentType == "" {
		object.ContentType = "application/octet-stream"
	}

	targetURL, host, canonicalURI, err := u.buildURL(object.Key)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256Hex(object.Content)

	headers := map[string]string{
		"content-type":         object.ContentType,
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	if u.config.SessionToken != "" {
		headers["x-amz-security-token"] = u.config.SessionToken
	}

	signedHeaders := sortedHeaderNames(headers)
	canonicalHeaders := buildCanonicalHeaders(headers, signedHeaders)
	canonicalRequest := strings.Join([]string{
		http.MethodPut,
		canonicalURI,
		"",
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n")

	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, u.config.Region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(awsSigningKey(u.config.SecretAccessKey, dateStamp, u.config.Region), stringToSign))
	authorization := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", u.config.AccessKeyID, scope, strings.Join(signedHeaders, ";"), signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, targetURL, bytes.NewReader(object.Content))
	if err != nil {
		return "", err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Authorization", authorization)

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload object to s3 failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("upload object to s3 failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if u.config.PublicBaseURL != "" {
		return u.config.PublicBaseURL + "/" + PathEscapeSegments(object.Key), nil
	}
	return targetURL, nil
}

func (u *S3Uploader) buildURL(key string) (string, string, string, error) {
	escapedKey := PathEscapeSegments(key)
	if u.config.Endpoint == "" {
		host := fmt.Sprintf("%s.s3.%s.amazonaws.com", u.config.Bucket, u.config.Region)
		canonicalURI := "/" + escapedKey
		return "https://" + host + canonicalURI, host, canonicalURI, nil
	}

	parsed, err := url.Parse(u.config.Endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", "", fmt.Errorf("invalid s3 endpoint")
	}

	if u.config.ForcePathStyle {
		canonicalURI := "/" + PathEscapeSegments(u.config.Bucket) + "/" + escapedKey
		parsed.Path = strings.TrimRight(parsed.Path, "/") + canonicalURI
		return parsed.String(), parsed.Host, canonicalURI, nil
	}

	host := u.config.Bucket + "." + parsed.Host
	canonicalURI := "/" + escapedKey
	parsed.Host = host
	parsed.Path = strings.TrimRight(parsed.Path, "/") + canonicalURI
	return parsed.String(), host, canonicalURI, nil
}

func PathEscapeSegments(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func sortedHeaderNames(headers map[string]string) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func buildCanonicalHeaders(headers map[string]string, names []string) string {
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(strings.TrimSpace(headers[name]))
		b.WriteByte('\n')
	}
	return b.String()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func awsSigningKey(secret, date, region string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secret), date)
	regionKey := hmacSHA256(dateKey, region)
	serviceKey := hmacSHA256(regionKey, "s3")
	return hmacSHA256(serviceKey, "aws4_request")
}
