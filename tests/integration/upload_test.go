//go:build integration

package integration

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
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
	body := bytes.NewReader([]byte("fake-jpeg-bytes"))
	req, err := http.NewRequest(http.MethodPut, uploadURL, body)
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
}
