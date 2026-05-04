package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/kevinlin/realdeal-api/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockUploadService implements services.UploadServiceInterface for testing.
type mockUploadService struct {
	output services.PresignOutput
	err    error
}

func (m *mockUploadService) Presign(_ context.Context, _ services.PresignInput) (services.PresignOutput, error) {
	return m.output, m.err
}

func setupUploadRouter(svc services.UploadServiceInterface, userID string) *gin.Engine {
	r := gin.New()
	h := handlers.NewUploadHandler(svc)
	r.POST("/upload/presign", func(c *gin.Context) {
		if userID != "" {
			c.Set("userID", userID)
		}
		h.Presign(c)
	})
	return r
}

func TestPresign_Success(t *testing.T) {
	t.Parallel()
	svc := &mockUploadService{
		output: services.PresignOutput{
			UploadURL: "https://s3.amazonaws.com/bucket/property/user1/abc.jpg?X-Amz-Signature=sig",
			PublicURL: "https://cdn.example.com/property/user1/abc.jpg",
			Key:       "property/user1/abc.jpg",
		},
	}
	r := setupUploadRouter(svc, "user1")

	body, _ := json.Marshal(map[string]string{
		"filename":     "image.jpg",
		"content_type": "image/jpeg",
		"upload_type":  "property",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/upload/presign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["upload_url"])
	assert.NotEmpty(t, resp["public_url"])
	assert.NotEmpty(t, resp["key"])
}

func TestPresign_MissingFilename(t *testing.T) {
	t.Parallel()
	svc := &mockUploadService{}
	r := setupUploadRouter(svc, "user1")

	body, _ := json.Marshal(map[string]string{
		"content_type": "image/jpeg",
		"upload_type":  "property",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/upload/presign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "VALIDATION_ERROR", resp["code"])
}

func TestPresign_InvalidContentType(t *testing.T) {
	t.Parallel()
	svc := &mockUploadService{}
	r := setupUploadRouter(svc, "user1")

	body, _ := json.Marshal(map[string]string{
		"filename":     "image.gif",
		"content_type": "image/gif",
		"upload_type":  "property",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/upload/presign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "VALIDATION_ERROR", resp["code"])
}

func TestPresign_InvalidUploadType(t *testing.T) {
	t.Parallel()
	svc := &mockUploadService{}
	r := setupUploadRouter(svc, "user1")

	body, _ := json.Marshal(map[string]string{
		"filename":     "image.jpg",
		"content_type": "image/jpeg",
		"upload_type":  "invalid_type",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/upload/presign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "VALIDATION_ERROR", resp["code"])
}

func TestPresign_ServiceUnavailable_NilService(t *testing.T) {
	t.Parallel()
	r := setupUploadRouter(nil, "user1")

	body, _ := json.Marshal(map[string]string{
		"filename":     "image.jpg",
		"content_type": "image/jpeg",
		"upload_type":  "property",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/upload/presign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestPresign_ServiceError(t *testing.T) {
	t.Parallel()
	svc := &mockUploadService{err: errors.New("aws error")}
	r := setupUploadRouter(svc, "user1")

	body, _ := json.Marshal(map[string]string{
		"filename":     "image.jpg",
		"content_type": "image/jpeg",
		"upload_type":  "property",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/upload/presign", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
