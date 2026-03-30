package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// Presigner generates time-limited signed URLs for private S3 objects.
type Presigner interface {
	PresignGetObject(ctx context.Context, key string, expiry time.Duration) (string, error)
}

type s3Presigner struct {
	client *s3.PresignClient
	bucket string
	log    *zap.Logger
}

// NewS3Presigner creates a Presigner backed by S3 or an S3-compatible store.
// Configuration mirrors NewS3Uploader — custom endpoints enable path-style
// addressing, and static credentials are used when both keys are provided.
func NewS3Presigner(ctx context.Context, bucket, endpoint, region, accessKey, secretKey string, log *zap.Logger) (Presigner, error) {
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
		return nil, fmt.Errorf("storage: load aws config for presigner: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})
	}

	s3Client := s3.NewFromConfig(cfg, s3Opts...)
	presignClient := s3.NewPresignClient(s3Client)

	log.Info("S3 presigner initialised",
		zap.String("bucket", bucket),
		zap.String("region", region),
	)

	return &s3Presigner{
		client: presignClient,
		bucket: bucket,
		log:    log,
	}, nil
}

// PresignGetObject returns a pre-authenticated GET URL for the given key
// that expires after the specified duration.
func (p *s3Presigner) PresignGetObject(ctx context.Context, key string, expiry time.Duration) (string, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "storage.PresignGetObject")
	defer span.End()
	span.SetAttributes(
		attribute.String("storage.bucket", p.bucket),
		attribute.String("storage.key", key),
	)

	result, err := p.client.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("storage: presign get %s/%s: %w", p.bucket, key, err)
	}

	return result.URL, nil
}

// ExtractKeyFromURL derives the S3 object key from a full object URL
// produced by the Uploader. It handles both custom-endpoint (path-style)
// and standard virtual-hosted-style AWS URLs.
func ExtractKeyFromURL(objectURL, bucket, endpoint string) string {
	if endpoint != "" {
		prefix := endpoint + "/" + bucket + "/"
		return strings.TrimPrefix(objectURL, prefix)
	}
	prefix := "https://" + bucket + ".s3.amazonaws.com/"
	return strings.TrimPrefix(objectURL, prefix)
}
