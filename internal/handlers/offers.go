package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

type OfferHandler struct {
	db                   *gorm.DB
	paymentDeadlineHours int
}

func NewOfferHandler(db *gorm.DB, paymentDeadlineHours int) *OfferHandler {
	return &OfferHandler{db: db, paymentDeadlineHours: paymentDeadlineHours}
}

type submitOfferRequest struct {
	Amount  float64 `json:"amount" binding:"required,gt=0"`
	Message string  `json:"message"`
}

// POST /api/v1/properties/:id/offers
func (h *OfferHandler) SubmitOffer(c *gin.Context) {
	propertyID := c.Param("id")
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
		c.JSON(http.StatusForbidden, gin.H{"error": "seller cannot submit an offer on their own listing", "code": "FORBIDDEN"})
		return
	}

	if flagged, err := hasConfirmedTrustEvent(h.db, callerID, models.TrustEventOfferDefault); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to submit offer", "code": "INTERNAL_ERROR"})
		return
	} else if flagged {
		c.JSON(http.StatusForbidden, gin.H{"error": "offers cannot be submitted from this account", "code": "FORBIDDEN"})
		return
	}

	var req submitOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	offer := models.Offer{
		PropertyID: propertyID,
		BuyerID:    callerID,
		Amount:     req.Amount,
		Message:    req.Message,
		Status:     models.OfferStatusPending,
	}

	if err := h.db.Create(&offer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to submit offer", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.Preload("Buyer").First(&offer, "id = ?", offer.ID)

	c.JSON(http.StatusCreated, gin.H{"data": offer, "message": "offer submitted successfully"})
}

// GET /api/v1/properties/:id/offers
func (h *OfferHandler) ListOffers(c *gin.Context) {
	propertyID := c.Param("id")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can view offers", "code": "FORBIDDEN"})
		return
	}

	var offers []models.Offer
	if err := h.db.Preload("Buyer").
		Where("property_id = ?", propertyID).
		Order("created_at DESC").
		Find(&offers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch offers", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": offers})
}

// PUT /api/v1/properties/:id/offers/:offerId/accept
func (h *OfferHandler) AcceptOffer(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can accept offers", "code": "FORBIDDEN"})
		return
	}

	if flagged, err := hasConfirmedTrustEvent(h.db, callerID, models.TrustEventDeedDefault, models.TrustEventDocumentFraud); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to accept offer", "code": "INTERNAL_ERROR"})
		return
	} else if flagged {
		c.JSON(http.StatusForbidden, gin.H{"error": "offers cannot be accepted from this account", "code": "FORBIDDEN"})
		return
	}

	if property.Status != models.PropertyStatusActive {
		c.JSON(http.StatusConflict, gin.H{"error": "property is not available", "code": "PROPERTY_NOT_AVAILABLE"})
		return
	}

	var offer models.Offer
	if err := h.db.First(&offer, "id = ? AND property_id = ?", offerID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offer not found", "code": "NOT_FOUND"})
		return
	}

	if offer.Status != models.OfferStatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "offer is no longer pending", "code": "OFFER_NOT_PENDING"})
		return
	}

	deadline := time.Now().Add(time.Duration(h.paymentDeadlineHours) * time.Hour)

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&offer).Updates(map[string]interface{}{
			"status":           models.OfferStatusAccepted,
			"payment_deadline": deadline,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Offer{}).
			Where("property_id = ? AND id != ? AND status = ?", propertyID, offerID, models.OfferStatusPending).
			Update("status", models.OfferStatusRejected).Error; err != nil {
			return err
		}
		return tx.Model(&property).Update("status", models.PropertyStatusPending).Error
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to accept offer", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.Preload("Buyer").First(&offer, "id = ?", offer.ID)

	c.JSON(http.StatusOK, gin.H{"data": offer, "message": "offer accepted"})
}

// PUT /api/v1/properties/:id/offers/:offerId/reject
func (h *OfferHandler) RejectOffer(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can reject offers", "code": "FORBIDDEN"})
		return
	}

	var offer models.Offer
	if err := h.db.First(&offer, "id = ? AND property_id = ?", offerID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offer not found", "code": "NOT_FOUND"})
		return
	}

	if offer.Status != models.OfferStatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "offer is no longer pending", "code": "OFFER_NOT_PENDING"})
		return
	}

	if err := h.db.Model(&offer).Update("status", models.OfferStatusRejected).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reject offer", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.Preload("Buyer").First(&offer, "id = ?", offer.ID)

	c.JSON(http.StatusOK, gin.H{"data": offer, "message": "offer rejected"})
}

// DELETE /api/v1/properties/:id/offers/:offerId
func (h *OfferHandler) WithdrawOffer(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	var offer models.Offer
	if err := h.db.First(&offer, "id = ? AND property_id = ?", offerID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offer not found", "code": "NOT_FOUND"})
		return
	}

	if offer.BuyerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the buyer can withdraw this offer", "code": "FORBIDDEN"})
		return
	}

	if offer.Status != models.OfferStatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "offer is no longer pending", "code": "OFFER_NOT_PENDING"})
		return
	}

	if err := h.db.Model(&offer).Update("status", models.OfferStatusWithdrawn).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to withdraw offer", "code": "INTERNAL_ERROR"})
		return
	}

	c.Status(http.StatusNoContent)
}

// GET /api/v1/users/me/offers
func (h *OfferHandler) ListMyOffers(c *gin.Context) {
	callerID := c.MustGet("userID").(string)

	var offers []models.Offer
	if err := h.db.Preload("Property").Preload("Property.Images").
		Where("buyer_id = ?", callerID).
		Order("created_at DESC").
		Find(&offers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch offers", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": offers})
}
