package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

// ContractHandler drives the post-acceptance signing state machine (MVP step
// 5). Contracts are created automatically by OfferHandler.AcceptOffer — there
// is no create endpoint here. The underlying documents are stubbed for the
// MVP; this handler implements the real state machine around them: terms
// proposal/agreement, signing, cancellation, and lazy expiry.
type ContractHandler struct {
	db *gorm.DB
}

func NewContractHandler(db *gorm.DB) *ContractHandler {
	return &ContractHandler{db: db}
}

const maxConditionsLength = 5000

// contractTerminalStatuses are statuses expiry must never override.
func isContractTerminal(status models.ContractStatus) bool {
	switch status {
	case models.ContractStatusExecuted, models.ContractStatusCancelled, models.ContractStatusExpired:
		return true
	default:
		return false
	}
}

// expireContractIfPastDeadline lazily evaluates expiry: if the contract is
// non-terminal and its execution deadline has passed, it flips to expired
// and reverts the property pending -> active in one transaction (only if the
// property is currently pending). Returns true if the contract was expired
// by this call (the in-memory contract is updated to reflect it).
//
// FUTURE HOOK: expiry currently carries no trust consequence by product
// decision (penalties for a contract that expired without execution will be
// layered on later via the trust_events core, the same way offer
// non-payment is handled — see internal/handlers/trust.go).
func (h *ContractHandler) expireContractIfPastDeadline(contract *models.Contract) (bool, error) {
	if isContractTerminal(contract.Status) {
		return false, nil
	}
	if !time.Now().After(contract.ExecutionDeadline) {
		return false, nil
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(contract).Update("status", models.ContractStatusExpired).Error; err != nil {
			return err
		}
		var property models.Property
		if err := tx.First(&property, "id = ?", contract.PropertyID).Error; err != nil {
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
		return false, err
	}

	contract.Status = models.ContractStatusExpired
	return true, nil
}

// loadContractForParty fetches the property, offer, and contract for the
// given property/offer path params, verifies the caller is a party to the
// contract (403 otherwise), and evaluates lazy expiry. It writes the
// appropriate error response and returns ok=false if anything fails.
func (h *ContractHandler) loadContractForParty(c *gin.Context, propertyID, offerID, callerID string) (contract models.Contract, ok bool) {
	var property models.Property
	if err := h.db.First(&property, "id = ?", propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return contract, false
	}

	if err := h.db.First(&contract, "offer_id = ? AND property_id = ?", offerID, propertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "contract not found", "code": "NOT_FOUND"})
		return contract, false
	}

	if contract.BuyerID != callerID && contract.SellerID != callerID {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the buyer or seller on this contract may access it", "code": "FORBIDDEN"})
		return contract, false
	}

	if _, err := h.expireContractIfPastDeadline(&contract); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load contract", "code": "INTERNAL_ERROR"})
		return contract, false
	}

	return contract, true
}

// GET /api/v1/properties/:id/offers/:offerId/contract
func (h *ContractHandler) GetContract(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	contract, ok := h.loadContractForParty(c, propertyID, offerID, callerID)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": contract})
}

type contractTermsRequest struct {
	MoveInDate   *time.Time `json:"move_in_date"`
	TransferDate *time.Time `json:"transfer_date"`
	Conditions   string     `json:"conditions" binding:"required"`
}

// PUT /api/v1/properties/:id/offers/:offerId/contract/terms
// Either party may propose/replace terms. Proposing sets the proposer's
// agreement and resets the other party's agreement and both signatures —
// any terms change voids prior agreement/signing progress.
func (h *ContractHandler) ProposeTerms(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	contract, ok := h.loadContractForParty(c, propertyID, offerID, callerID)
	if !ok {
		return
	}

	if isContractTerminal(contract.Status) {
		c.JSON(http.StatusConflict, gin.H{"error": contractTerminalErrorMessage(contract.Status), "code": contractTerminalErrorCode(contract.Status)})
		return
	}

	var req contractTermsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	if len(req.Conditions) == 0 || len(req.Conditions) > maxConditionsLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conditions is required and must be at most 5000 characters", "code": "VALIDATION_ERROR"})
		return
	}

	now := time.Now()
	if req.MoveInDate != nil && !req.MoveInDate.After(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "move_in_date must be in the future", "code": "VALIDATION_ERROR"})
		return
	}
	if req.TransferDate != nil && !req.TransferDate.After(now) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transfer_date must be in the future", "code": "VALIDATION_ERROR"})
		return
	}
	if req.MoveInDate != nil && req.TransferDate != nil && req.TransferDate.Before(*req.MoveInDate) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transfer_date must not be before move_in_date", "code": "VALIDATION_ERROR"})
		return
	}

	proposer := callerID
	updates := map[string]interface{}{
		"move_in_date":      req.MoveInDate,
		"transfer_date":     req.TransferDate,
		"conditions":        req.Conditions,
		"terms_proposed_by": &proposer,
		"status":            models.ContractStatusDraft,
		"buyer_agreed_at":   nil,
		"seller_agreed_at":  nil,
		"buyer_signed_at":   nil,
		"seller_signed_at":  nil,
	}
	if callerID == contract.BuyerID {
		updates["buyer_agreed_at"] = now
	} else {
		updates["seller_agreed_at"] = now
	}

	// Updates() with a map on a model that carries associations (Offer,
	// Property, Seller, Buyer) makes GORM open its own transaction even for
	// a single-row update; wrap explicitly so behavior is the same whether
	// or not GORM would have done it implicitly, matching AcceptOffer's
	// pattern for map-based updates.
	err := h.db.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&contract).Updates(updates).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to propose terms", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.First(&contract, "id = ?", contract.ID)

	c.JSON(http.StatusOK, gin.H{"data": contract, "message": "terms proposed"})
}

// POST /api/v1/properties/:id/offers/:offerId/contract/agree-terms
// The non-proposing party (or either party whose agreement is currently
// nil) agrees to the current terms. When both have agreed, status advances
// to terms_agreed, unlocking signing.
func (h *ContractHandler) AgreeTerms(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	contract, ok := h.loadContractForParty(c, propertyID, offerID, callerID)
	if !ok {
		return
	}

	if isContractTerminal(contract.Status) {
		c.JSON(http.StatusConflict, gin.H{"error": contractTerminalErrorMessage(contract.Status), "code": contractTerminalErrorCode(contract.Status)})
		return
	}

	if contract.TermsProposedBy == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "no terms have been proposed yet", "code": "TERMS_NOT_PROPOSED"})
		return
	}

	isBuyer := callerID == contract.BuyerID
	alreadyAgreed := (isBuyer && contract.BuyerAgreedAt != nil) || (!isBuyer && contract.SellerAgreedAt != nil)
	if alreadyAgreed {
		c.JSON(http.StatusConflict, gin.H{"error": "you have already agreed to the current terms", "code": "ALREADY_AGREED"})
		return
	}

	now := time.Now()
	updates := map[string]interface{}{}
	if isBuyer {
		updates["buyer_agreed_at"] = now
	} else {
		updates["seller_agreed_at"] = now
	}

	bothAgreed := (isBuyer && contract.SellerAgreedAt != nil) || (!isBuyer && contract.BuyerAgreedAt != nil)
	if bothAgreed {
		updates["status"] = models.ContractStatusTermsAgreed
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&contract).Updates(updates).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to agree to terms", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.First(&contract, "id = ?", contract.ID)

	c.JSON(http.StatusOK, gin.H{"data": contract, "message": "terms agreed"})
}

// POST /api/v1/properties/:id/offers/:offerId/contract/sign
// Only reachable once both parties have agreed to the current terms
// (status terms_agreed, buyer_signed, or seller_signed). Executed once both
// signatures are present.
func (h *ContractHandler) Sign(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	contract, ok := h.loadContractForParty(c, propertyID, offerID, callerID)
	if !ok {
		return
	}

	if isContractTerminal(contract.Status) {
		c.JSON(http.StatusConflict, gin.H{"error": contractTerminalErrorMessage(contract.Status), "code": contractTerminalErrorCode(contract.Status)})
		return
	}

	switch contract.Status {
	case models.ContractStatusTermsAgreed, models.ContractStatusBuyerSigned, models.ContractStatusSellerSigned:
		// signing unlocked
	default:
		c.JSON(http.StatusConflict, gin.H{"error": "terms have not been agreed to yet", "code": "TERMS_NOT_AGREED"})
		return
	}

	isBuyer := callerID == contract.BuyerID
	if isBuyer && contract.BuyerSignedAt != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "you have already signed this contract", "code": "ALREADY_SIGNED"})
		return
	}
	if !isBuyer && contract.SellerSignedAt != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "you have already signed this contract", "code": "ALREADY_SIGNED"})
		return
	}

	now := time.Now()
	updates := map[string]interface{}{}
	var newStatus models.ContractStatus
	if isBuyer {
		updates["buyer_signed_at"] = now
		if contract.SellerSignedAt != nil {
			newStatus = models.ContractStatusExecuted
		} else {
			newStatus = models.ContractStatusBuyerSigned
		}
	} else {
		updates["seller_signed_at"] = now
		if contract.BuyerSignedAt != nil {
			newStatus = models.ContractStatusExecuted
		} else {
			newStatus = models.ContractStatusSellerSigned
		}
	}
	updates["status"] = newStatus

	err := h.db.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&contract).Updates(updates).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sign contract", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.First(&contract, "id = ?", contract.ID)

	c.JSON(http.StatusOK, gin.H{"data": contract, "message": "contract signed"})
}

// POST /api/v1/properties/:id/offers/:offerId/contract/cancel
// Either party may cancel any time before executed. Reverts the property
// pending -> active in the same transaction (only if currently pending),
// mirroring AcceptOffer's transaction pattern.
func (h *ContractHandler) Cancel(c *gin.Context) {
	propertyID := c.Param("id")
	offerID := c.Param("offerId")
	callerID := c.MustGet("userID").(string)

	contract, ok := h.loadContractForParty(c, propertyID, offerID, callerID)
	if !ok {
		return
	}

	if isContractTerminal(contract.Status) {
		c.JSON(http.StatusConflict, gin.H{"error": contractTerminalErrorMessage(contract.Status), "code": contractTerminalErrorCode(contract.Status)})
		return
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&contract).Update("status", models.ContractStatusCancelled).Error; err != nil {
			return err
		}
		var property models.Property
		if err := tx.First(&property, "id = ?", propertyID).Error; err != nil {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel contract", "code": "INTERNAL_ERROR"})
		return
	}

	h.db.First(&contract, "id = ?", contract.ID)

	c.JSON(http.StatusOK, gin.H{"data": contract, "message": "contract cancelled"})
}

// GET /api/v1/users/me/contracts
func (h *ContractHandler) ListMyContracts(c *gin.Context) {
	callerID := c.MustGet("userID").(string)

	var contracts []models.Contract
	if err := h.db.
		Where("buyer_id = ? OR seller_id = ?", callerID, callerID).
		Order("created_at DESC").
		Find(&contracts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch contracts", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": contracts})
}

// contractTerminalErrorMessage/Code produce the mutation-path 409 for a
// terminal contract. GET (loadContractForParty callers that don't further
// gate on terminal status, i.e. GetContract) instead returns the expired
// contract itself per spec.
func contractTerminalErrorMessage(status models.ContractStatus) string {
	if status == models.ContractStatusExpired {
		return "contract has expired"
	}
	return "contract is no longer active"
}

func contractTerminalErrorCode(status models.ContractStatus) string {
	if status == models.ContractStatusExpired {
		return "CONTRACT_EXPIRED"
	}
	return "CONTRACT_NOT_ACTIVE"
}
