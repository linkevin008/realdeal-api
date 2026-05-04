package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/kevinlin/realdeal-api/internal/config"
)

// allowedUploadTypes lists the valid values for UploadType.
var allowedUploadTypes = map[string]bool{
	"property":        true,
	"profile":         true,
	"id_verification": true,
}

// UploadServiceInterface allows the handler to use a mock in tests.
type UploadServiceInterface interface {
	Presign(ctx context.Context, input PresignInput) (PresignOutput, error)
}

// UploadService generates presigned S3 PUT URLs.
// It never touches image bytes — signing only.
type UploadService struct {
	presignClient *s3.PresignClient
	bucket        string
	cdnBase       string
}

// PresignInput contains the parameters needed to generate a presigned URL.
type PresignInput struct {
	UserID      string
	UploadType  string // "property" | "profile" | "id_verification"
	Filename    string
	ContentType string
}

// PresignOutput holds the presigned upload URL and the resulting public URL.
type PresignOutput struct {
	UploadURL string
	PublicURL string
	Key       string
}

// NewUploadService creates an UploadService using the AWS SDK default credential chain.
// Returns an error if S3Bucket or CloudFrontBaseURL are empty.
func NewUploadService(cfg *config.Config) (*UploadService, error) {
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is not configured")
	}
	if cfg.CloudFrontBaseURL == "" {
		return nil, fmt.Errorf("CLOUDFRONT_BASE_URL is not configured")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		context.Background(),
		awsconfig.WithRegion(cfg.AWSRegion),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg)
	presignClient := s3.NewPresignClient(s3Client)

	return &UploadService{
		presignClient: presignClient,
		bucket:        cfg.S3Bucket,
		cdnBase:       strings.TrimRight(cfg.CloudFrontBaseURL, "/"),
	}, nil
}

// Presign generates a presigned S3 PUT URL for the given input.
// The S3 key format is: {upload_type}/{user_id}/{uuid}.{ext}
func (s *UploadService) Presign(ctx context.Context, input PresignInput) (PresignOutput, error) {
	if !allowedUploadTypes[input.UploadType] {
		return PresignOutput{}, fmt.Errorf("invalid upload_type %q: must be one of property, profile, id_verification", input.UploadType)
	}

	ext := strings.ToLower(filepath.Ext(input.Filename))
	if ext == "" {
		ext = ".jpg"
	}

	key := fmt.Sprintf("%s/%s/%s%s", input.UploadType, input.UserID, uuid.New().String(), ext)

	req, err := s.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(input.ContentType),
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return PresignOutput{}, fmt.Errorf("failed to presign S3 PUT: %w", err)
	}

	return PresignOutput{
		UploadURL: req.URL,
		PublicURL: fmt.Sprintf("%s/%s", s.cdnBase, key),
		Key:       key,
	}, nil
}
