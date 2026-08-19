package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStorage records what the service asked it to sign, so the tests can
// assert on the key layout and TTL the service is responsible for.
type fakeStorage struct {
	gotKey         string
	gotContentType string
	gotTTL         time.Duration
	url            string
	err            error
}

func (f *fakeStorage) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	f.gotKey = key
	f.gotContentType = contentType
	f.gotTTL = ttl
	if f.err != nil {
		return "", f.err
	}
	return f.url, nil
}

func TestPresign_KeyLayoutAndURLs(t *testing.T) {
	t.Parallel()
	fake := &fakeStorage{url: "https://signed.example/put"}
	svc := services.NewUploadServiceWithStorage(fake, "https://cdn.example.com")

	out, err := svc.Presign(context.Background(), services.PresignInput{
		UserID:      "user1",
		UploadType:  "property",
		Filename:    "photo.png",
		ContentType: "image/png",
	})
	require.NoError(t, err)

	// {upload_type}/{user_id}/{uuid}.{ext}
	assert.True(t, strings.HasPrefix(out.Key, "property/user1/"), "key was %q", out.Key)
	assert.True(t, strings.HasSuffix(out.Key, ".png"), "key was %q", out.Key)
	assert.Equal(t, fake.gotKey, out.Key, "signed key must match the returned key")
	assert.Equal(t, "image/png", fake.gotContentType)
	assert.Equal(t, 15*time.Minute, fake.gotTTL)

	assert.Equal(t, "https://signed.example/put", out.UploadURL)
	assert.Equal(t, "https://cdn.example.com/"+out.Key, out.PublicURL)
}

func TestPresign_AllowedUploadTypes(t *testing.T) {
	t.Parallel()
	for _, ut := range []string{"property", "profile", "id_verification"} {
		fake := &fakeStorage{url: "https://signed.example/put"}
		svc := services.NewUploadServiceWithStorage(fake, "https://cdn.example.com")

		out, err := svc.Presign(context.Background(), services.PresignInput{
			UserID: "u", UploadType: ut, Filename: "a.jpg", ContentType: "image/jpeg",
		})
		require.NoError(t, err, "upload_type %q should be allowed", ut)
		assert.True(t, strings.HasPrefix(out.Key, ut+"/u/"), "key was %q", out.Key)
	}
}

func TestPresign_RejectsUnknownUploadType(t *testing.T) {
	t.Parallel()
	fake := &fakeStorage{url: "https://signed.example/put"}
	svc := services.NewUploadServiceWithStorage(fake, "https://cdn.example.com")

	_, err := svc.Presign(context.Background(), services.PresignInput{
		UserID: "u", UploadType: "passport", Filename: "a.jpg", ContentType: "image/jpeg",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid upload_type")
	assert.Empty(t, fake.gotKey, "must not sign anything for a rejected upload type")
}

func TestPresign_ExtensionHandling(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, filename, wantSuffix string
	}{
		{"missing extension defaults to jpg", "photo", ".jpg"},
		{"uppercase extension is lowercased", "PHOTO.JPEG", ".jpeg"},
		{"multi-dot keeps the last segment", "my.holiday.photo.PNG", ".png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeStorage{url: "https://signed.example/put"}
			svc := services.NewUploadServiceWithStorage(fake, "https://cdn.example.com")

			out, err := svc.Presign(context.Background(), services.PresignInput{
				UserID: "u", UploadType: "profile", Filename: tc.filename, ContentType: "image/jpeg",
			})
			require.NoError(t, err)
			assert.True(t, strings.HasSuffix(out.Key, tc.wantSuffix), "key was %q", out.Key)
		})
	}
}

func TestPresign_TrimsTrailingSlashOnCDNBase(t *testing.T) {
	t.Parallel()
	fake := &fakeStorage{url: "https://signed.example/put"}
	svc := services.NewUploadServiceWithStorage(fake, "https://cdn.example.com/")

	out, err := svc.Presign(context.Background(), services.PresignInput{
		UserID: "u", UploadType: "property", Filename: "a.jpg", ContentType: "image/jpeg",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com/"+out.Key, out.PublicURL)
}

func TestPresign_KeysAreUnique(t *testing.T) {
	t.Parallel()
	fake := &fakeStorage{url: "https://signed.example/put"}
	svc := services.NewUploadServiceWithStorage(fake, "https://cdn.example.com")

	in := services.PresignInput{
		UserID: "u", UploadType: "property", Filename: "a.jpg", ContentType: "image/jpeg",
	}
	first, err := svc.Presign(context.Background(), in)
	require.NoError(t, err)
	second, err := svc.Presign(context.Background(), in)
	require.NoError(t, err)

	assert.NotEqual(t, first.Key, second.Key, "identical input must not collide on one key")
}

func TestPresign_PropagatesStorageError(t *testing.T) {
	t.Parallel()
	fake := &fakeStorage{err: errors.New("signing backend down")}
	svc := services.NewUploadServiceWithStorage(fake, "https://cdn.example.com")

	_, err := svc.Presign(context.Background(), services.PresignInput{
		UserID: "u", UploadType: "property", Filename: "a.jpg", ContentType: "image/jpeg",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signing backend down")
}

func TestNewUploadService_RequiresStorageConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cfg     config.Config
		wantErr string
	}{
		{"missing bucket", config.Config{CloudFrontBaseURL: "https://cdn.example.com"}, "S3_BUCKET is not configured"},
		{"missing cdn base", config.Config{S3Bucket: "b"}, "CLOUDFRONT_BASE_URL is not configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := tc.cfg
			_, err := services.NewUploadService(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestNewS3Storage_RequiresBucket(t *testing.T) {
	t.Parallel()
	_, err := services.NewS3Storage(context.Background(), "us-west-2", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket is required")
}
