package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
}

type Client struct {
	s3     *s3.Client
	bucket string
}

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

func ConfigFromEnv() Config {
	return Config{
		Endpoint:        env("S3_ENDPOINT", "http://localhost:9000"),
		Region:          env("S3_REGION", "us-east-1"),
		Bucket:          env("S3_BUCKET", "cineweave"),
		AccessKeyID:     env("S3_ACCESS_KEY_ID", "minio"),
		SecretAccessKey: env("S3_SECRET_ACCESS_KEY", "minio123"),
		UsePathStyle:    envBool("S3_USE_PATH_STYLE", true),
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
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		options.UsePathStyle = cfg.UsePathStyle
	})
	return &Client{s3: client, bucket: cfg.Bucket}, nil
}

func (c *Client) PutJSON(ctx context.Context, key string, value any) (PutResult, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return PutResult{}, err
	}
	return c.PutBytes(ctx, key, body, "application/json")
}

func (c *Client) PutBytes(ctx context.Context, key string, body []byte, contentType string) (PutResult, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	sum := sha256.Sum256(body)
	if _, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	}); err != nil {
		return PutResult{}, err
	}
	return PutResult{
		StorageKey:  key,
		ContentHash: "sha256:" + hex.EncodeToString(sum[:]),
		ByteSize:    int64(len(body)),
	}, nil
}

func (c *Client) PresignPutObject(ctx context.Context, key, contentType string, expires time.Duration) (PresignedPutResult, error) {
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	presignClient := s3.NewPresignClient(c.s3)
	result, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, func(options *s3.PresignOptions) {
		options.Expires = expires
	})
	if err != nil {
		return PresignedPutResult{}, err
	}
	return PresignedPutResult{
		StorageKey: key,
		URL:        result.URL,
		Method:     result.Method,
		Headers:    result.SignedHeader,
		ExpiresAt:  time.Now().Add(expires).UTC(),
	}, nil
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
