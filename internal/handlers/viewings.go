package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

type ViewingHandler struct {
	db *gorm.DB
}

func NewViewingHandler(db *gorm.DB) *ViewingHandler {
	return &ViewingHandler{db: db}
}

type createSlotRequest struct {
	StartTime time.Time `json:"start_time" binding:"required"`
	EndTime   time.Time `json:"end_time" binding:"required"`
}

type requestViewingRequest struct {
	Message string `json:"message"`
}

type viewingSlotResponse struct {
	models.ViewingSlot
	Booked bool `json:"booked"`
}

// POST /api/v1/properties/:id/viewing-slots
func (h *ViewingHandler) CreateSlot(c *gin.Context) {
	propertyID := c.Param("id")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can create viewing slots", "code": "FORBIDDEN"})
		return
	}

	if property.Status != models.PropertyStatusActive {
		c.JSON(http.StatusConflict, gin.H{"error": "property is not available", "code": "PROPERTY_NOT_AVAILABLE"})
		return
	}

	var req createSlotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	if !req.EndTime.After(req.StartTime) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end_time must be after start_time", "code": "VALIDATION_ERROR"})
		return
	}

	if !req.StartTime.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_time must be in the future", "code": "VALIDATION_ERROR"})
		return
	}

	var overlapCount int64
	if err := h.db.Model(&models.ViewingSlot{}).
		Where("property_id = ? AND start_time < ? AND end_time > ?", propertyID, req.EndTime, req.StartTime).
		Count(&overlapCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate slot", "code": "INTERNAL_ERROR"})
		return
	}

	if overlapCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "slot overlaps an existing slot", "code": "SLOT_OVERLAP"})
		return
	}

	slot := models.ViewingSlot{
		PropertyID: propertyID,
		StartTime:  req.StartTime,
		EndTime:    req.EndTime,
	}

	if err := h.db.Create(&slot).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create viewing slot", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": slot, "message": "viewing slot created"})
}

// GET /api/v1/properties/:id/viewing-slots
func (h *ViewingHandler) ListSlots(c *gin.Context) {
	propertyID := c.Param("id")

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	var slots []models.ViewingSlot
	if err := h.db.Where("property_id = ?", propertyID).
		Order("start_time ASC").
		Find(&slots).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch viewing slots", "code": "INTERNAL_ERROR"})
		return
	}

	response := make([]viewingSlotResponse, 0, len(slots))
	for _, slot := range slots {
		var acceptedCount int64
		h.db.Model(&models.ViewingRequest{}).
			Where("slot_id = ? AND status = ?", slot.ID, models.ViewingRequestStatusAccepted).
			Count(&acceptedCount)
		response = append(response, viewingSlotResponse{ViewingSlot: slot, Booked: acceptedCount > 0})
	}

	c.JSON(http.StatusOK, gin.H{"data": response})
}

// DELETE /api/v1/properties/:id/viewing-slots/:slotId
func (h *ViewingHandler) DeleteSlot(c *gin.Context) {
	propertyID := c.Param("id")
	slotID := c.Param("slotId")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can delete viewing slots", "code": "FORBIDDEN"})
		return
	}

	var slot models.ViewingSlot
	if err := h.db.First(&slot, "id = ? AND property_id = ?", slotID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "viewing slot not found", "code": "NOT_FOUND"})
		return
	}

	var acceptedCount int64
	if err := h.db.Model(&models.ViewingRequest{}).
		Where("slot_id = ? AND status = ?", slotID, models.ViewingRequestStatusAccepted).
		Count(&acceptedCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check viewing requests", "code": "INTERNAL_ERROR"})
		return
	}

	if acceptedCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "slot has a confirmed viewing; handle it before deleting", "code": "SLOT_HAS_ACCEPTED_REQUEST"})
		return
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ViewingRequest{}).
			Where("slot_id = ? AND status = ?", slotID, models.ViewingRequestStatusPending).
			Update("status", models.ViewingRequestStatusDeclined).Error; err != nil {
			return err
		}
		return tx.Delete(&slot).Error
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete viewing slot", "code": "INTERNAL_ERROR"})
		return
	}

	c.Status(http.StatusNoContent)
}

// POST /api/v1/properties/:id/viewing-slots/:slotId/requests
func (h *ViewingHandler) RequestViewing(c *gin.Context) {
	propertyID := c.Param("id")
	slotID := c.Param("slotId")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.Status != models.PropertyStatusActive {
		c.JSON(http.StatusConflict, gin.H{"error": "property is not available", "code": "PROPERTY_NOT_AVAILABLE"})
		return
	}

	if property.SellerID != nil && *property.SellerID == callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "seller cannot request a viewing on their own listing", "code": "FORBIDDEN"})
		return
	}

	var slot models.ViewingSlot
	if err := h.db.First(&slot, "id = ? AND property_id = ?", slotID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "viewing slot not found", "code": "NOT_FOUND"})
		return
	}

	if !slot.StartTime.After(time.Now()) {
		c.JSON(http.StatusConflict, gin.H{"error": "viewing slot has already passed", "code": "SLOT_PAST"})
		return
	}

	var acceptedCount int64
	if err := h.db.Model(&models.ViewingRequest{}).
		Where("slot_id = ? AND status = ?", slotID, models.ViewingRequestStatusAccepted).
		Count(&acceptedCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate slot", "code": "INTERNAL_ERROR"})
		return
	}

	if acceptedCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "viewing slot is already booked", "code": "SLOT_BOOKED"})
		return
	}

	var existingCount int64
	if err := h.db.Model(&models.ViewingRequest{}).
		Where("slot_id = ? AND buyer_id = ? AND status = ?", slotID, callerID, models.ViewingRequestStatusPending).
		Count(&existingCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate request", "code": "INTERNAL_ERROR"})
		return
	}

	if existingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "you already have a pending request for this slot", "code": "REQUEST_ALREADY_EXISTS"})
		return
	}

	var req requestViewingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	viewingRequest := models.ViewingRequest{
		SlotID:     slotID,
		PropertyID: propertyID,
		BuyerID:    callerID,
		Message:    req.Message,
		Status:     models.ViewingRequestStatusPending,
	}

	if err := h.db.Create(&viewingRequest).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to submit viewing request", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.Preload("Buyer").Preload("Slot").First(&viewingRequest, "id = ?", viewingRequest.ID)

	c.JSON(http.StatusCreated, gin.H{"data": viewingRequest, "message": "viewing request submitted"})
}

// GET /api/v1/properties/:id/viewing-requests
func (h *ViewingHandler) ListRequests(c *gin.Context) {
	propertyID := c.Param("id")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can view viewing requests", "code": "FORBIDDEN"})
		return
	}

	var requests []models.ViewingRequest
	if err := h.db.Preload("Buyer").Preload("Slot").
		Where("property_id = ?", propertyID).
		Order("created_at DESC").
		Find(&requests).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch viewing requests", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": requests})
}

// PUT /api/v1/properties/:id/viewing-requests/:requestId/accept
func (h *ViewingHandler) AcceptRequest(c *gin.Context) {
	propertyID := c.Param("id")
	requestID := c.Param("requestId")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can accept viewing requests", "code": "FORBIDDEN"})
		return
	}

	var viewingRequest models.ViewingRequest
	if err := h.db.First(&viewingRequest, "id = ? AND property_id = ?", requestID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "viewing request not found", "code": "NOT_FOUND"})
		return
	}

	if viewingRequest.Status != models.ViewingRequestStatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "viewing request is no longer pending", "code": "REQUEST_NOT_PENDING"})
		return
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&viewingRequest).Update("status", models.ViewingRequestStatusAccepted).Error; err != nil {
			return err
		}
		return tx.Model(&models.ViewingRequest{}).
			Where("slot_id = ? AND id != ? AND status = ?", viewingRequest.SlotID, requestID, models.ViewingRequestStatusPending).
			Update("status", models.ViewingRequestStatusDeclined).Error
	})

	if err != nil {
		// The pre-check above handles the common case, but two concurrent
		// accepts on the same slot can both pass it before either commits.
		// The partial unique index on viewing_requests(slot_id) WHERE
		// status='accepted' is the hard backstop for that race; map its
		// violation to the same 409 the pre-check would have produced.
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "viewing slot is already booked", "code": "SLOT_BOOKED"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to accept viewing request", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.Preload("Buyer").First(&viewingRequest, "id = ?", viewingRequest.ID)

	c.JSON(http.StatusOK, gin.H{"data": viewingRequest, "message": "viewing request accepted"})
}

// PUT /api/v1/properties/:id/viewing-requests/:requestId/decline
func (h *ViewingHandler) DeclineRequest(c *gin.Context) {
	propertyID := c.Param("id")
	requestID := c.Param("requestId")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can decline viewing requests", "code": "FORBIDDEN"})
		return
	}

	var viewingRequest models.ViewingRequest
	if err := h.db.First(&viewingRequest, "id = ? AND property_id = ?", requestID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "viewing request not found", "code": "NOT_FOUND"})
		return
	}

	if viewingRequest.Status != models.ViewingRequestStatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "viewing request is no longer pending", "code": "REQUEST_NOT_PENDING"})
		return
	}

	if err := h.db.Model(&viewingRequest).Update("status", models.ViewingRequestStatusDeclined).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to decline viewing request", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.Preload("Buyer").First(&viewingRequest, "id = ?", viewingRequest.ID)

	c.JSON(http.StatusOK, gin.H{"data": viewingRequest, "message": "viewing request declined"})
}

// DELETE /api/v1/viewing-requests/:requestId
func (h *ViewingHandler) CancelRequest(c *gin.Context) {
	requestID := c.Param("requestId")
	callerID := c.MustGet("userID").(string)

	var viewingRequest models.ViewingRequest
	if err := h.db.First(&viewingRequest, "id = ?", requestID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "viewing request not found", "code": "NOT_FOUND"})
		return
	}

	if viewingRequest.BuyerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the buyer can cancel this viewing request", "code": "FORBIDDEN"})
		return
	}

	if viewingRequest.Status != models.ViewingRequestStatusPending && viewingRequest.Status != models.ViewingRequestStatusAccepted {
		c.JSON(http.StatusConflict, gin.H{"error": "viewing request can no longer be cancelled", "code": "REQUEST_NOT_CANCELLABLE"})
		return
	}

	if err := h.db.Model(&viewingRequest).Update("status", models.ViewingRequestStatusCancelled).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel viewing request", "code": "INTERNAL_ERROR"})
		return
	}

	c.Status(http.StatusNoContent)
}

// GET /api/v1/users/me/viewing-requests
func (h *ViewingHandler) ListMyRequests(c *gin.Context) {
	callerID := c.MustGet("userID").(string)

	var requests []models.ViewingRequest
	if err := h.db.Preload("Property").Preload("Property.Images").Preload("Slot").
		Where("buyer_id = ?", callerID).
		Order("created_at DESC").
		Find(&requests).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch viewing requests", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": requests})
}
