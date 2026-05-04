package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/services"
)

// UploadHandler handles presigned URL generation for direct-to-S3 uploads.
type UploadHandler struct {
	uploadService services.UploadServiceInterface
}

// NewUploadHandler creates an UploadHandler.
// uploadService may be nil when S3 is not configured; the handler returns 503 in that case.
func NewUploadHandler(uploadService services.UploadServiceInterface) *UploadHandler {
	return &UploadHandler{uploadService: uploadService}
}

type presignRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	UploadType  string `json:"upload_type"`
}

var allowedContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
}

var allowedUploadTypesHandler = map[string]bool{
	"property":        true,
	"profile":         true,
	"id_verification": true,
}

// Presign handles POST /api/v1/upload/presign.
// Requires auth — userID is read from the gin context key "userID" (set by auth middleware).
func (h *UploadHandler) Presign(c *gin.Context) {
	if h.uploadService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "upload service is not configured",
			"code":  "SERVICE_UNAVAILABLE",
		})
		return
	}

	var req presignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	if req.Filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename is required", "code": "VALIDATION_ERROR"})
		return
	}

	if !allowedContentTypes[req.ContentType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "content_type must be image/jpeg or image/png",
			"code":  "VALIDATION_ERROR",
		})
		return
	}

	if !allowedUploadTypesHandler[req.UploadType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "upload_type must be property, profile, or id_verification",
			"code":  "VALIDATION_ERROR",
		})
		return
	}

	userIDVal, _ := c.Get("userID")
	userID, _ := userIDVal.(string)

	out, err := h.uploadService.Presign(c.Request.Context(), services.PresignInput{
		UserID:      userID,
		UploadType:  req.UploadType,
		Filename:    req.Filename,
		ContentType: req.ContentType,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"upload_url": out.UploadURL,
		"public_url": out.PublicURL,
		"key":        out.Key,
	})
}
