//go:build integration

package integration

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestUploadPresignFlow exercises the real S3 path: presign via core, then PUT
// the bytes directly to object storage (LocalStack locally, real S3 when run
// against AWS). This is the flow the iOS app uses.
func TestUploadPresignFlow(t *testing.T) {
	user, _ := signup(t, "seller")

	resp := user.do("POST", "/api/v1/upload/presign", map[string]interface{}{
		"filename":     "photo.jpg",
		"content_type": "image/jpeg",
		"upload_type":  "property",
	}).mustStatus(t, http.StatusOK)

	uploadURL, _ := resp.body["upload_url"].(string)
	publicURL, _ := resp.body["public_url"].(string)
	if uploadURL == "" || publicURL == "" {
		t.Fatalf("presign response missing URLs: %v", resp.body)
	}

	// PUT directly to storage — no Authorization header, the presigned URL
	// carries the signature (same as the iOS client)
	payload := []byte("fake-jpeg-bytes")
	req, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "image/jpeg")

	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT to presigned URL: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT to presigned URL returned %d", putResp.StatusCode)
	}

	if !strings.Contains(publicURL, "property/") {
		t.Fatalf("public URL missing upload-type prefix: %s", publicURL)
	}

	// Read the object back through the public URL. Locally that is LocalStack's
	// S3 path URL; against AWS it is CloudFront in front of a PRIVATE bucket, so
	// this is the only assertion that exercises the OAI and the bucket policy.
	// Without it the write half of the media path is proven and the read half is
	// not — a broken distribution would still leave this test green.
	var (
		fetched  []byte
		lastErr  error
		lastCode int
	)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		getResp, err := http.Get(publicURL)
		if err != nil {
			lastErr = err
			continue
		}
		lastCode = getResp.StatusCode
		if getResp.StatusCode == http.StatusOK {
			fetched, lastErr = io.ReadAll(getResp.Body)
			getResp.Body.Close()
			break
		}
		getResp.Body.Close()
	}
	if fetched == nil {
		t.Fatalf("GET %s did not return the object: status=%d err=%v", publicURL, lastCode, lastErr)
	}
	if !bytes.Equal(fetched, payload) {
		t.Fatalf("object fetched back differs: got %q, want %q", fetched, payload)
	}
}
