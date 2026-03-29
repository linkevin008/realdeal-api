package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

type FavoriteHandler struct {
	db *gorm.DB
}

func NewFavoriteHandler(db *gorm.DB) *FavoriteHandler {
	return &FavoriteHandler{db: db}
}

type addFavoriteRequest struct {
	PropertyID string `json:"property_id" binding:"required"`
}

// GET /api/v1/users/:id/favorites
func (h *FavoriteHandler) ListFavorites(c *gin.Context) {
	id := c.Param("id")
	callerID, _ := c.Get("userID")

	if callerID != id {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot view another user's favorites", "code": "FORBIDDEN"})
		return
	}

	var favorites []models.Favorite
	if err := h.db.Where("user_id = ?", id).Preload("Property.Images").Find(&favorites).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch favorites", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  favorites,
		"total": len(favorites),
	})
}

// POST /api/v1/users/:id/favorites
func (h *FavoriteHandler) AddFavorite(c *gin.Context) {
	id := c.Param("id")
	callerID, _ := c.Get("userID")

	if callerID != id {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot add favorites for another user", "code": "FORBIDDEN"})
		return
	}

	var req addFavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	// Verify property exists
	var property models.Property
	if err := h.db.First(&property, "id = ?", req.PropertyID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	favorite := models.Favorite{
		UserID:     id,
		PropertyID: req.PropertyID,
		SavedAt:    time.Now(),
	}

	if err := h.db.Create(&favorite).Error; err != nil {
		// Likely duplicate — unique constraint violation
		c.JSON(http.StatusConflict, gin.H{"error": "property already in favorites", "code": "ALREADY_EXISTS"})
		return
	}

	// Reload with property
	h.db.Preload("Property.Images").First(&favorite, "id = ?", favorite.ID)

	c.JSON(http.StatusCreated, gin.H{"data": favorite, "message": "added to favorites"})
}

// DELETE /api/v1/users/:id/favorites/:propertyId
func (h *FavoriteHandler) RemoveFavorite(c *gin.Context) {
	id := c.Param("id")
	propertyID := c.Param("propertyId")
	callerID, _ := c.Get("userID")

	if callerID != id {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot remove favorites for another user", "code": "FORBIDDEN"})
		return
	}

	result := h.db.Where("user_id = ? AND property_id = ?", id, propertyID).Delete(&models.Favorite{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove favorite", "code": "INTERNAL_ERROR"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "favorite not found", "code": "NOT_FOUND"})
		return
	}

	c.Status(http.StatusNoContent)
}
