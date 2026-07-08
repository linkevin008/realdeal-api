package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

// TrustHandler records hidden, bidirectional trustworthiness events. Trust
// data is never returned by any response in this handler — endpoints echo
// only the offer (or nothing at all), never the TrustEvent itself.
type TrustHandler struct {
	db *gorm.DB
}

func NewTrustHandler(db *gorm.DB) *TrustHandler {
	return &TrustHandler{db: db}
}

// hasConfirmedTrustEvent reports whether userID has at least one confirmed
// trust event of any of the given types. One confirmed violation is enough
// to enforce a block — there is no strike count.
func hasConfirmedTrustEvent(db *gorm.DB, userID string, types ...models.TrustEventType) (bool, error) {
	var count int64
	err := db.Model(&models.TrustEvent{}).
		Where("user_id = ? AND status = ? AND event_type IN ?", userID, models.TrustEventStatusConfirmed, types).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// isUniqueViolation is a best-effort check for a unique-constraint error
// surfaced by the driver, mirroring the string-matching convention already
// used in favorites.go (sqlmock in tests never returns a typed pg error).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique constraint") || strings.Contains(msg, "23505")
}

// POST /api/v1/properties/:id/offers/:offerId/report-nonpayment
// Seller-only. The accepted offer's payment deadline must have objectively
// passed — the server verifies this itself, so the event auto-confirms.
func (h *TrustHandler) ReportNonPayment(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the seller can report non-payment", "code": "FORBIDDEN"})
		return
	}

	var offer models.Offer
	if err := h.db.First(&offer, "id = ? AND property_id = ?", offerID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offer not found", "code": "NOT_FOUND"})
		return
	}

	if offer.Status != models.OfferStatusAccepted {
		c.JSON(http.StatusConflict, gin.H{"error": "offer is not in an accepted state", "code": "OFFER_NOT_ACCEPTED"})
		return
	}

	if offer.PaymentDeadline == nil || !time.Now().After(*offer.PaymentDeadline) {
		c.JSON(http.StatusConflict, gin.H{"error": "payment deadline has not passed", "code": "DEADLINE_NOT_PASSED"})
		return
	}

	sellerID := callerID
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&offer).Update("status", models.OfferStatusDefaulted).Error; err != nil {
			return err
		}

		event := models.TrustEvent{
			UserID:     offer.BuyerID,
			EventType:  models.TrustEventOfferDefault,
			Status:     models.TrustEventStatusConfirmed,
			OfferID:    &offer.ID,
			PropertyID: &property.ID,
			ReportedBy: &sellerID,
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}

		if property.Status == models.PropertyStatusPending {
			if err := tx.Model(&property).Update("status", models.PropertyStatusActive).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "non-payment already reported for this offer", "code": "ALREADY_REPORTED"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to report non-payment", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.First(&offer, "id = ?", offer.ID)

	c.JSON(http.StatusOK, gin.H{"data": offer, "message": "non-payment reported"})
}

type reportSellerRequest struct {
	Violation string `json:"violation" binding:"required,oneof=deed_default document_fraud"`
	Notes     string `json:"notes"`
}

const maxTrustNotesLength = 2000

// POST /api/v1/properties/:id/offers/:offerId/report-seller
// Buyer-only (the accepted buyer on the offer). Deed non-delivery and
// document fraud are accusations, not machine-verifiable facts — the event
// lands pending_review and enforces nothing until manually confirmed.
func (h *TrustHandler) ReportSeller(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	var offer models.Offer
	if err := h.db.First(&offer, "id = ? AND property_id = ?", offerID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offer not found", "code": "NOT_FOUND"})
		return
	}

	if offer.BuyerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the buyer on this offer can report the seller", "code": "FORBIDDEN"})
		return
	}

	if offer.Status != models.OfferStatusAccepted {
		c.JSON(http.StatusConflict, gin.H{"error": "offer is not in an accepted state", "code": "OFFER_NOT_ACCEPTED"})
		return
	}

	var req reportSellerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	if len(req.Notes) > maxTrustNotesLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "notes exceeds maximum length", "code": "VALIDATION_ERROR"})
		return
	}

	if property.SellerID == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "property has no seller on record", "code": "NO_SELLER"})
		return
	}

	buyerID := callerID
	event := models.TrustEvent{
		UserID:     *property.SellerID,
		EventType:  models.TrustEventType(req.Violation),
		Status:     models.TrustEventStatusPendingReview,
		OfferID:    &offer.ID,
		PropertyID: &property.ID,
		ReportedBy: &buyerID,
		Notes:      req.Notes,
	}

	if err := h.db.Create(&event).Error; err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "this violation has already been reported for this offer", "code": "ALREADY_REPORTED"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to report seller", "code": "INTERNAL_ERROR"})
		return
	}

	c.Status(http.StatusNoContent)
}

type fileAppealRequest struct {
	Statement string `json:"statement" binding:"required"`
}

const maxAppealStatementLength = 2000

// noAppealAvailableResponse is returned in every case where the caller has
// nothing appealable right now: no confirmed trust event at all, or every
// confirmed event already has an appeal attached. Using one status/body for
// both cases (rather than, say, 404 for "never blocked" vs 409 for "already
// appealed") is the minimal-disclosure choice — a caller (or anyone who
// compromises a caller's session) cannot use this endpoint to probe whether
// an account carries a confirmed trust event or not; every non-actionable
// outcome looks identical.
func noAppealAvailableResponse(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{"error": "no appeal is available for this account", "code": "NO_APPEAL_AVAILABLE"})
}

// POST /api/v1/users/me/trust-appeal
// Files an appeal against one of the caller's confirmed trust events.
// MINIMAL DISCLOSURE: the response never reveals which (or how many) events
// exist — success returns only a bare status, and every non-actionable
// outcome (nothing to appeal, or already fully appealed) returns the same
// neutral conflict via noAppealAvailableResponse. Resolution is manual ops
// only (see TrustAppeal's doc comment for the SQL) — there is no
// adjudication endpoint here.
func (h *TrustHandler) FileAppeal(c *gin.Context) {
	callerID := c.MustGet("userID").(string)

	var req fileAppealRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "statement is required", "code": "VALIDATION_ERROR"})
		return
	}

	statement := strings.TrimSpace(req.Statement)
	if statement == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "statement is required", "code": "VALIDATION_ERROR"})
		return
	}
	if len(statement) > maxAppealStatementLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "statement exceeds maximum length", "code": "VALIDATION_ERROR"})
		return
	}

	// One open appeal per user at a time, regardless of which event it's
	// attached to.
	var pendingCount int64
	if err := h.db.Model(&models.TrustAppeal{}).
		Where("user_id = ? AND status = ?", callerID, models.TrustAppealStatusPending).
		Count(&pendingCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to file appeal", "code": "INTERNAL_ERROR"})
		return
	}
	if pendingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "an appeal is already pending for this account", "code": "APPEAL_PENDING"})
		return
	}

	// Find the caller's confirmed trust events, oldest first, and pick the
	// first one that has no appeal attached yet.
	var confirmedEvents []models.TrustEvent
	if err := h.db.
		Where("user_id = ? AND status = ?", callerID, models.TrustEventStatusConfirmed).
		Order("created_at ASC").
		Find(&confirmedEvents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to file appeal", "code": "INTERNAL_ERROR"})
		return
	}
	if len(confirmedEvents) == 0 {
		noAppealAvailableResponse(c)
		return
	}

	var appealedEventIDs []string
	if err := h.db.Model(&models.TrustAppeal{}).
		Where("user_id = ?", callerID).
		Pluck("trust_event_id", &appealedEventIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to file appeal", "code": "INTERNAL_ERROR"})
		return
	}
	appealed := make(map[string]bool, len(appealedEventIDs))
	for _, id := range appealedEventIDs {
		appealed[id] = true
	}

	var targetEventID string
	for _, ev := range confirmedEvents {
		if !appealed[ev.ID] {
			targetEventID = ev.ID
			break
		}
	}
	if targetEventID == "" {
		// Every confirmed event already has an appeal (necessarily
		// resolved, since we already checked for a pending one above).
		// Same neutral response as "nothing to appeal" — do not disclose
		// which case this is.
		noAppealAvailableResponse(c)
		return
	}

	appeal := models.TrustAppeal{
		TrustEventID: targetEventID,
		UserID:       callerID,
		Statement:    statement,
		Status:       models.TrustAppealStatusPending,
	}
	if err := h.db.Create(&appeal).Error; err != nil {
		if isUniqueViolation(err) {
			// Lost a race against another appeal on the same event.
			noAppealAvailableResponse(c)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to file appeal", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": gin.H{"status": "pending"}})
}
