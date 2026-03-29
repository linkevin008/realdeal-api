package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

type UserHandler struct {
	db *gorm.DB
}

func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{db: db}
}

type updateUserRequest struct {
	Name            *string          `json:"name"`
	PhoneNumber     *string          `json:"phone_number"`
	ProfilePhotoURL *string          `json:"profile_photo_url"`
	Role            *models.UserRole `json:"role"`
	ShowEmail       *bool            `json:"show_email"`
	ShowPhone       *bool            `json:"show_phone"`
	ShowListings    *bool            `json:"show_listings"`
}

// GET /api/v1/users/:id
func (h *UserHandler) GetUser(c *gin.Context) {
	id := c.Param("id")
	callerID, _ := c.Get("userID")

	var user models.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found", "code": "NOT_FOUND"})
		return
	}

	// If not requesting own profile, apply visibility rules
	if callerID != id {
		if !user.ShowEmail {
			user.Email = ""
		}
		if !user.ShowPhone {
			user.PhoneNumber = nil
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": user, "message": "user retrieved successfully"})
}

// PUT /api/v1/users/:id
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id := c.Param("id")
	callerID, _ := c.Get("userID")

	if callerID != id {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot update another user's profile", "code": "FORBIDDEN"})
		return
	}

	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	var user models.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found", "code": "NOT_FOUND"})
		return
	}

	updates := map[string]interface{}{}
	if req.Name != nil && *req.Name != "" {
		updates["name"] = *req.Name
	}
	if req.PhoneNumber != nil {
		updates["phone_number"] = req.PhoneNumber
	}
	if req.ProfilePhotoURL != nil {
		updates["profile_photo_url"] = req.ProfilePhotoURL
	}
	if req.Role != nil {
		updates["role"] = *req.Role
	}
	if req.ShowEmail != nil {
		updates["show_email"] = *req.ShowEmail
	}
	if req.ShowPhone != nil {
		updates["show_phone"] = *req.ShowPhone
	}
	if req.ShowListings != nil {
		updates["show_listings"] = *req.ShowListings
	}

	if len(updates) > 0 {
		if err := h.db.Model(&user).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user", "code": "INTERNAL_ERROR"})
			return
		}
	}

	// Reload updated user
	h.db.First(&user, "id = ?", id)

	c.JSON(http.StatusOK, gin.H{"data": user, "message": "user updated successfully"})
}

// DELETE /api/v1/users/:id
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id := c.Param("id")
	callerID, _ := c.Get("userID")

	if callerID != id {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot delete another user's account", "code": "FORBIDDEN"})
		return
	}

	var user models.User
	if err := h.db.First(&user, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found", "code": "NOT_FOUND"})
		return
	}

	if err := h.db.Delete(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user", "code": "INTERNAL_ERROR"})
		return
	}

	c.Status(http.StatusNoContent)
}
