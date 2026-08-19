package services

import (
	"context"
	"time"
)

// MediaStorage is the object-store primitive behind the upload flow, and the
// only cloud-specific seam in it. Key layout, upload-type validation and
// public-URL construction all live in UploadService, so porting to another
// provider means writing one new implementation of this interface and nothing
// else.
//
// Implementations sign only — they never see image bytes. The client PUTs
// directly to the returned URL.
type MediaStorage interface {
	// PresignPut returns a URL granting a time-limited PUT of exactly this key
	// and content type. The URL must expire after ttl.
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
}
