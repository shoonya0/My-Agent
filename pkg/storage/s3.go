package storage

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const tracerName = "pkg/storage"

// Uploader uploads binary data to an object store and returns the object URL.
type Uploader interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) (url string, err error)
}

type s3Uploader struct {
	client   *s3.Client
	bucket   string
	endpoint string
	log      *zap.Logger
}

// NewS3Uploader creates an Uploader backed by S3 or an S3-compatible store
// (e.g. MinIO). When endpoint is non-empty it is used as a custom endpoint
// with path-style addressing enabled. Static credentials are used when both
// accessKey and secretKey are provided; otherwise the default credential
// chain is used (env vars, IAM role, etc.).
func NewS3Uploader(ctx context.Context, bucket, endpoint, region, accessKey, secretKey string, log *zap.Logger) (Uploader, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}

	if accessKey != "" && secretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(cfg, s3Opts...)

	log.Info("S3 uploader initialised",
		zap.String("bucket", bucket),
		zap.String("region", region),
		zap.String("endpoint", endpoint),
	)

	return &s3Uploader{
		client:   client,
		bucket:   bucket,
		endpoint: endpoint,
		log:      log,
	}, nil
}

// Upload stores data in the configured bucket at the given key and returns
// the public object URL.
func (u *s3Uploader) Upload(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "storage.Upload")
	defer span.End()
	span.SetAttributes(
		attribute.String("storage.bucket", u.bucket),
		attribute.String("storage.key", key),
		attribute.Int("storage.size_bytes", len(data)),
	)

	_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(u.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("storage: put object %s/%s: %w", u.bucket, key, err)
	}

	objURL := u.objectURL(key)
	u.log.Debug("Object uploaded",
		zap.String("bucket", u.bucket),
		zap.String("key", key),
		zap.Int("size_bytes", len(data)),
	)

	return objURL, nil
}

func (u *s3Uploader) objectURL(key string) string {
	if u.endpoint != "" {
		return fmt.Sprintf("%s/%s/%s", u.endpoint, u.bucket, key)
	}
	return fmt.Sprintf("https://%s.s3.amazonaws.com/%s", u.bucket, key)
}
