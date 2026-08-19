package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kevinlin/realdeal-api/internal/config"
)

// presignTTL is how long a generated upload URL stays valid.
const presignTTL = 15 * time.Minute

// allowedUploadTypes lists the valid values for UploadType. This is the single
// source of truth — the handler validates through IsAllowedUploadType rather
// than keeping its own copy.
var allowedUploadTypes = map[string]bool{
	"property":        true,
	"profile":         true,
	"id_verification": true,
}

// IsAllowedUploadType reports whether t is a valid upload_type. Exported so the
// handler can reject early with a 400 without duplicating the set.
func IsAllowedUploadType(t string) bool {
	return allowedUploadTypes[t]
}

// UploadServiceInterface allows the handler to use a mock in tests.
type UploadServiceInterface interface {
	Presign(ctx context.Context, input PresignInput) (PresignOutput, error)
}

// UploadService turns an upload request into a presigned URL. It owns the key
// layout, upload-type validation and public-URL construction; the provider-
// specific signing is delegated to MediaStorage.
// It never touches image bytes — signing only.
type UploadService struct {
	storage MediaStorage
	cdnBase string
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

// NewUploadService creates an UploadService backed by S3.
// Returns an error if S3Bucket or CloudFrontBaseURL are empty.
func NewUploadService(cfg *config.Config) (*UploadService, error) {
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is not configured")
	}
	if cfg.CloudFrontBaseURL == "" {
		return nil, fmt.Errorf("CLOUDFRONT_BASE_URL is not configured")
	}

	storage, err := NewS3Storage(context.Background(), cfg.AWSRegion, cfg.S3Bucket)
	if err != nil {
		return nil, err
	}

	return NewUploadServiceWithStorage(storage, cfg.CloudFrontBaseURL), nil
}

// NewUploadServiceWithStorage builds an UploadService over any MediaStorage.
// This is the seam for tests and for wiring a non-S3 provider.
func NewUploadServiceWithStorage(storage MediaStorage, cdnBase string) *UploadService {
	return &UploadService{
		storage: storage,
		cdnBase: strings.TrimRight(cdnBase, "/"),
	}
}

// Presign generates a presigned PUT URL for the given input.
// The object key format is: {upload_type}/{user_id}/{uuid}.{ext}
func (s *UploadService) Presign(ctx context.Context, input PresignInput) (PresignOutput, error) {
	if !allowedUploadTypes[input.UploadType] {
		return PresignOutput{}, fmt.Errorf("invalid upload_type %q: must be one of property, profile, id_verification", input.UploadType)
	}

	ext := strings.ToLower(filepath.Ext(input.Filename))
	if ext == "" {
		ext = ".jpg"
	}

	key := fmt.Sprintf("%s/%s/%s%s", input.UploadType, input.UserID, uuid.New().String(), ext)

	uploadURL, err := s.storage.PresignPut(ctx, key, input.ContentType, presignTTL)
	if err != nil {
		return PresignOutput{}, err
	}

	return PresignOutput{
		UploadURL: uploadURL,
		PublicURL: fmt.Sprintf("%s/%s", s.cdnBase, key),
		Key:       key,
	}, nil
}
