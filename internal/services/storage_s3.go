package services

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Storage is the S3 implementation of MediaStorage. It is the only file in
// the service layer that imports the AWS SDK.
type S3Storage struct {
	presignClient *s3.PresignClient
	bucket        string
}

// NewS3Storage builds an S3Storage using the AWS SDK default credential chain,
// so the same code path serves ECS task roles in prod and local credentials or
// LocalStack in development.
func NewS3Storage(ctx context.Context, region, bucket string) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		// Custom endpoints (AWS_ENDPOINT_URL, e.g. LocalStack) need path-style
		// addressing — virtual-host style would put the bucket in the hostname,
		// which custom endpoints can't resolve. Unset in prod → virtual-host style.
		if awsCfg.BaseEndpoint != nil {
			o.UsePathStyle = true
		}
	})

	return &S3Storage{
		presignClient: s3.NewPresignClient(client),
		bucket:        bucket,
	}, nil
}

// PresignPut implements MediaStorage.
func (s *S3Storage) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	req, err := s.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("failed to presign S3 PUT: %w", err)
	}
	return req.URL, nil
}
