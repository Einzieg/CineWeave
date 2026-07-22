package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Endpoint            string
	PublicEndpoint      string
	Region              string
	Bucket              string
	AccessKeyID         string
	SecretAccessKey     string
	UsePathStyle        bool
	ProxyURL            string
	DownloadPartSize    int64
	DownloadConcurrency int
}

type Client struct {
	s3                  *s3.Client
	presignS3           *s3.Client
	bucket              string
	downloadPartSize    int64
	downloadConcurrency int
}

const (
	defaultDownloadPartSize    = int64(1 << 20)
	defaultDownloadConcurrency = 8
	maxDownloadConcurrency     = 32
)

type PutResult struct {
	StorageKey  string
	ContentHash string
	ByteSize    int64
}

type PresignedPutResult struct {
	StorageKey string
	URL        string
	Method     string
	Headers    http.Header
	ExpiresAt  time.Time
}

type PresignedGetResult struct {
	StorageKey string    `json:"storageKey"`
	URL        string    `json:"url"`
	Method     string    `json:"method"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

func ConfigFromEnv() Config {
	return Config{
		Endpoint:        env("S3_ENDPOINT", "http://localhost:9000"),
		PublicEndpoint:  env("S3_PUBLIC_ENDPOINT", ""),
		Region:          env("S3_REGION", "us-east-1"),
		Bucket:          env("S3_BUCKET", "cineweave"),
		AccessKeyID:     env("S3_ACCESS_KEY_ID", "minio"),
		SecretAccessKey: env("S3_SECRET_ACCESS_KEY", "minio123"),
		UsePathStyle:    envBool("S3_USE_PATH_STYLE", true),
		ProxyURL:        env("S3_HTTP_PROXY", ""),
		DownloadPartSize: envInt64(
			"S3_DOWNLOAD_PART_SIZE_BYTES",
			defaultDownloadPartSize,
		),
		DownloadConcurrency: envInt(
			"S3_DOWNLOAD_CONCURRENCY",
			defaultDownloadConcurrency,
		),
	}
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}
	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, err
	}
	storageHTTPClient, err := newStorageHTTPClient(cfg.ProxyURL)
	if err != nil {
		return nil, err
	}
	awsCfg.HTTPClient = storageHTTPClient
	client := newS3Client(awsCfg, cfg.Endpoint, cfg.UsePathStyle)
	presignEndpoint := cfg.Endpoint
	if strings.TrimSpace(cfg.PublicEndpoint) != "" {
		presignEndpoint = cfg.PublicEndpoint
	}
	presignClient := newS3Client(awsCfg, presignEndpoint, cfg.UsePathStyle)
	partSize := cfg.DownloadPartSize
	if partSize <= 0 {
		partSize = defaultDownloadPartSize
	}
	concurrency := cfg.DownloadConcurrency
	if concurrency <= 0 {
		concurrency = defaultDownloadConcurrency
	}
	if concurrency > maxDownloadConcurrency {
		concurrency = maxDownloadConcurrency
	}
	return &Client{
		s3: client, presignS3: presignClient, bucket: cfg.Bucket,
		downloadPartSize: partSize, downloadConcurrency: concurrency,
	}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.s3 == nil || c.bucket == "" {
		return fmt.Errorf("storage client is not initialized")
	}
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	return err
}

func (c *Client) PutJSON(ctx context.Context, key string, value any) (PutResult, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return PutResult{}, err
	}
	return c.PutBytes(ctx, key, body, "application/json")
}

func (c *Client) PutBytes(ctx context.Context, key string, body []byte, contentType string) (PutResult, error) {
	return c.PutObject(ctx, key, body, contentType)
}

func (c *Client) PutObject(ctx context.Context, key string, body []byte, contentType string) (PutResult, error) {
	normalizedKey, err := validateObjectKey(key)
	if err != nil {
		return PutResult{}, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	sum := sha256.Sum256(body)
	if _, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(normalizedKey),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	}); err != nil {
		return PutResult{}, err
	}
	return PutResult{
		StorageKey:  normalizedKey,
		ContentHash: "sha256:" + hex.EncodeToString(sum[:]),
		ByteSize:    int64(len(body)),
	}, nil
}

func (c *Client) PutFile(ctx context.Context, key, filePath, contentType string) (PutResult, error) {
	normalizedKey, err := validateObjectKey(key)
	if err != nil {
		return PutResult{}, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	file, err := os.Open(filePath)
	if err != nil {
		return PutResult{}, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return PutResult{}, err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return PutResult{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return PutResult{}, err
	}
	if _, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(normalizedKey),
		Body:        file,
		ContentType: aws.String(contentType),
	}); err != nil {
		return PutResult{}, err
	}
	return PutResult{
		StorageKey:  normalizedKey,
		ContentHash: "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
		ByteSize:    stat.Size(),
	}, nil
}

func (c *Client) GetObject(ctx context.Context, key string, maxBytes int64) ([]byte, string, error) {
	normalizedKey, err := validateObjectKey(key)
	if err != nil {
		return nil, "", err
	}
	head, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(normalizedKey),
	})
	if err != nil {
		return nil, "", err
	}
	if head.ContentLength == nil || *head.ContentLength < 0 {
		return nil, "", fmt.Errorf("object %s did not report a valid content length", normalizedKey)
	}
	size := *head.ContentLength
	if maxBytes > 0 && size > maxBytes {
		return nil, "", fmt.Errorf("object %s exceeds maxBytes limit", normalizedKey)
	}
	contentType := ""
	if head.ContentType != nil {
		contentType = *head.ContentType
	}
	if size == 0 {
		return []byte{}, contentType, nil
	}
	body, err := c.downloadObjectRanges(ctx, normalizedKey, size, head.ETag)
	if err != nil {
		return nil, "", err
	}
	return body, contentType, nil
}

type objectByteRange struct {
	start int64
	end   int64
}

func (c *Client) downloadObjectRanges(ctx context.Context, key string, size int64, etag *string) ([]byte, error) {
	partSize := c.downloadPartSize
	if partSize <= 0 {
		partSize = defaultDownloadPartSize
	}
	partCount := int((size + partSize - 1) / partSize)
	concurrency := c.downloadConcurrency
	if concurrency <= 0 {
		concurrency = defaultDownloadConcurrency
	}
	if concurrency > partCount {
		concurrency = partCount
	}
	body := make([]byte, size)
	jobs := make(chan objectByteRange, partCount)
	for start := int64(0); start < size; start += partSize {
		end := start + partSize - 1
		if end >= size {
			end = size - 1
		}
		jobs <- objectByteRange{start: start, end: end}
	}
	close(jobs)

	downloadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wait sync.WaitGroup
	var firstErr error
	var firstErrOnce sync.Once
	for workerIndex := 0; workerIndex < concurrency; workerIndex++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for part := range jobs {
				if downloadCtx.Err() != nil {
					return
				}
				if err := c.downloadObjectRange(downloadCtx, key, etag, part, body[part.start:part.end+1]); err != nil {
					firstErrOnce.Do(func() {
						firstErr = err
						cancel()
					})
					return
				}
			}
		}()
	}
	wait.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) downloadObjectRange(ctx context.Context, key string, etag *string, part objectByteRange, destination []byte) error {
	rangeValue := fmt.Sprintf("bytes=%d-%d", part.start, part.end)
	result, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket:  aws.String(c.bucket),
		Key:     aws.String(key),
		Range:   aws.String(rangeValue),
		IfMatch: etag,
	})
	if err != nil {
		return fmt.Errorf("download object %s range %s: %w", key, rangeValue, err)
	}
	defer result.Body.Close()
	expected := int64(len(destination))
	partBody, err := io.ReadAll(io.LimitReader(result.Body, expected+1))
	if err != nil {
		return fmt.Errorf("read object %s range %s: %w", key, rangeValue, err)
	}
	if int64(len(partBody)) != expected {
		return fmt.Errorf("object %s range %s returned %d bytes, expected %d", key, rangeValue, len(partBody), expected)
	}
	copy(destination, partBody)
	return nil
}

func (c *Client) PresignPutObject(ctx context.Context, key, contentType string, expires time.Duration) (PresignedPutResult, error) {
	normalizedKey, err := validateObjectKey(key)
	if err != nil {
		return PresignedPutResult{}, err
	}
	expires = normalizePresignExpiry(expires)
	presignClient := s3.NewPresignClient(c.presignS3)
	result, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(normalizedKey),
		ContentType: aws.String(contentType),
	}, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return PresignedPutResult{}, err
	}
	return PresignedPutResult{
		StorageKey: normalizedKey,
		URL:        result.URL,
		Method:     result.Method,
		Headers:    result.SignedHeader,
		ExpiresAt:  time.Now().Add(expires).UTC(),
	}, nil
}

func (c *Client) PresignGetObject(ctx context.Context, key string, expires time.Duration) (PresignedGetResult, error) {
	normalizedKey, err := validateObjectKey(key)
	if err != nil {
		return PresignedGetResult{}, err
	}
	expires = normalizePresignExpiry(expires)
	presignClient := s3.NewPresignClient(c.presignS3)
	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(normalizedKey),
	}, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return PresignedGetResult{}, err
	}
	return PresignedGetResult{
		StorageKey: normalizedKey,
		URL:        result.URL,
		Method:     result.Method,
		ExpiresAt:  time.Now().Add(expires).UTC(),
	}, nil
}

func newS3Client(cfg aws.Config, endpoint string, usePathStyle bool) *s3.Client {
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		if strings.TrimSpace(endpoint) != "" {
			options.BaseEndpoint = aws.String(strings.TrimSpace(endpoint))
		}
		options.UsePathStyle = usePathStyle
	})
}

func newStorageHTTPClient(rawProxyURL string) (*http.Client, error) {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport has unexpected type %T", http.DefaultTransport)
	}
	transport := baseTransport.Clone()
	transport.Proxy = nil
	if rawProxyURL = strings.TrimSpace(rawProxyURL); rawProxyURL != "" {
		proxyURL, err := url.Parse(rawProxyURL)
		if err != nil || proxyURL.Host == "" || proxyURL.Scheme != "http" && proxyURL.Scheme != "https" {
			return nil, fmt.Errorf("S3 HTTP proxy URL is invalid")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: transport}, nil
}

func normalizePresignExpiry(expires time.Duration) time.Duration {
	if expires <= 0 {
		return 15 * time.Minute
	}
	if expires > time.Hour {
		return time.Hour
	}
	return expires
}

func validateObjectKey(key string) (string, error) {
	normalized := strings.TrimSpace(key)
	if normalized == "" {
		return "", fmt.Errorf("storage key is required")
	}
	if strings.HasPrefix(normalized, "/") || path.IsAbs(normalized) || strings.Contains(normalized, "\\") {
		return "", fmt.Errorf("storage key must be a relative object key")
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.Contains(normalized, "../") || strings.Contains(normalized, "/..") || strings.HasPrefix(normalized, "./") || strings.Contains(normalized, "/./") {
		return "", fmt.Errorf("storage key must not contain path traversal")
	}
	return normalized, nil
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
