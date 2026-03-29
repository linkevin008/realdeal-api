package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

type PropertyHandler struct {
	db *gorm.DB
}

func NewPropertyHandler(db *gorm.DB) *PropertyHandler {
	return &PropertyHandler{db: db}
}

type createPropertyRequest struct {
	Street      string               `json:"street" binding:"required"`
	City        string               `json:"city" binding:"required"`
	State       string               `json:"state" binding:"required"`
	ZipCode     string               `json:"zip_code"`
	Country     string               `json:"country" binding:"required"`
	Price       float64              `json:"price" binding:"required,gt=0"`
	Type        models.PropertyType  `json:"property_type" binding:"required"`
	Description string               `json:"description"`
	Bedrooms    *int                 `json:"bedrooms"`
	Bathrooms   *float64             `json:"bathrooms"`
	SquareFeet  *int                 `json:"square_feet"`
	LotSize     *float64             `json:"lot_size"`
	YearBuilt   *int                 `json:"year_built"`
	Latitude    float64              `json:"latitude"`
	Longitude   float64              `json:"longitude"`
	Source      models.ListingSource `json:"source"`
	Images      []struct {
		URL   string `json:"url"`
		Order int    `json:"order"`
	} `json:"images"`
}

type updatePropertyRequest struct {
	Street      *string               `json:"street"`
	City        *string               `json:"city"`
	State       *string               `json:"state"`
	ZipCode     *string               `json:"zip_code"`
	Country     *string               `json:"country"`
	Price       *float64              `json:"price"`
	Type        *models.PropertyType  `json:"property_type"`
	Description *string               `json:"description"`
	Bedrooms    *int                  `json:"bedrooms"`
	Bathrooms   *float64              `json:"bathrooms"`
	SquareFeet  *int                  `json:"square_feet"`
	LotSize     *float64              `json:"lot_size"`
	YearBuilt   *int                  `json:"year_built"`
	Latitude    *float64              `json:"latitude"`
	Longitude   *float64              `json:"longitude"`
	Source      *models.ListingSource `json:"source"`
	Status      *models.PropertyStatus `json:"status"`
}

// GET /api/v1/properties
func (h *PropertyHandler) ListProperties(c *gin.Context) {
	query := h.db.Model(&models.Property{}).Preload("Images").Preload("Seller")

	// Status filter (default: active)
	status := c.DefaultQuery("status", string(models.PropertyStatusActive))
	query = query.Where("status = ?", status)

	// Price range
	if priceMin := c.Query("price_min"); priceMin != "" {
		if v, err := strconv.ParseFloat(priceMin, 64); err == nil {
			query = query.Where("price >= ?", v)
		}
	}
	if priceMax := c.Query("price_max"); priceMax != "" {
		if v, err := strconv.ParseFloat(priceMax, 64); err == nil {
			query = query.Where("price <= ?", v)
		}
	}

	// Property type (multi — comma-separated or repeated)
	if types := c.QueryArray("type"); len(types) > 0 {
		query = query.Where("type IN ?", types)
	} else if t := c.Query("type"); t != "" {
		parts := strings.Split(t, ",")
		query = query.Where("type IN ?", parts)
	}

	// Source (multi — comma-separated or repeated)
	if sources := c.QueryArray("source"); len(sources) > 0 {
		query = query.Where("source IN ?", sources)
	} else if s := c.Query("source"); s != "" {
		parts := strings.Split(s, ",")
		query = query.Where("source IN ?", parts)
	}

	// Bedrooms/Bathrooms minimums
	if bedroomsMin := c.Query("bedrooms_min"); bedroomsMin != "" {
		if v, err := strconv.Atoi(bedroomsMin); err == nil {
			query = query.Where("bedrooms >= ?", v)
		}
	}
	if bathroomsMin := c.Query("bathrooms_min"); bathroomsMin != "" {
		if v, err := strconv.ParseFloat(bathroomsMin, 64); err == nil {
			query = query.Where("bathrooms >= ?", v)
		}
	}

	// Seller ID
	if sellerID := c.Query("seller_id"); sellerID != "" {
		query = query.Where("seller_id = ?", sellerID)
	}

	// Location radius (Haversine)
	latStr := c.Query("lat")
	lonStr := c.Query("lon")
	radiusStr := c.Query("radius_miles")
	if latStr != "" && lonStr != "" && radiusStr != "" {
		lat, latErr := strconv.ParseFloat(latStr, 64)
		lon, lonErr := strconv.ParseFloat(lonStr, 64)
		radiusMiles, radErr := strconv.ParseFloat(radiusStr, 64)
		if latErr == nil && lonErr == nil && radErr == nil {
			radiusKm := radiusMiles * 1.60934
			haversine := `(6371 * acos(
				cos(radians(?)) * cos(radians(latitude)) *
				cos(radians(longitude) - radians(?)) +
				sin(radians(?)) * sin(radians(latitude))
			)) <= ?`
			query = query.Where(haversine, lat, lon, lat, radiusKm)
		}
	}

	// Count total before pagination
	var total int64
	query.Count(&total)

	// Pagination
	page := 1
	limit := 20
	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	offset := (page - 1) * limit

	var properties []models.Property
	if err := query.Offset(offset).Limit(limit).Find(&properties).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch properties", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  properties,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// GET /api/v1/properties/:id
func (h *PropertyHandler) GetProperty(c *gin.Context) {
	id := c.Param("id")

	var property models.Property
	if err := h.db.Preload("Images").Preload("Seller").First(&property, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": property, "message": "property retrieved successfully"})
}

// POST /api/v1/properties
func (h *PropertyHandler) CreateProperty(c *gin.Context) {
	callerID, _ := c.Get("userID")
	sellerID := callerID.(string)

	var req createPropertyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	// Validate coordinates
	if req.Latitude < -90 || req.Latitude > 90 || req.Longitude < -180 || req.Longitude > 180 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid coordinates", "code": "VALIDATION_ERROR"})
		return
	}

	source := req.Source
	if source == "" {
		source = models.ListingSourceUserGenerated
	}

	property := models.Property{
		Street:      req.Street,
		City:        req.City,
		State:       req.State,
		ZipCode:     req.ZipCode,
		Country:     req.Country,
		Price:       req.Price,
		Type:        req.Type,
		Description: req.Description,
		Bedrooms:    req.Bedrooms,
		Bathrooms:   req.Bathrooms,
		SquareFeet:  req.SquareFeet,
		LotSize:     req.LotSize,
		YearBuilt:   req.YearBuilt,
		Latitude:    req.Latitude,
		Longitude:   req.Longitude,
		Source:      source,
		SellerID:    &sellerID,
		Status:      models.PropertyStatusActive,
	}

	// Build images
	for _, img := range req.Images {
		if img.URL != "" {
			property.Images = append(property.Images, models.PropertyImage{
				URL:   img.URL,
				Order: img.Order,
			})
		}
	}

	if err := h.db.Create(&property).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create property", "code": "INTERNAL_ERROR"})
		return
	}

	// Reload with associations
	h.db.Preload("Images").Preload("Seller").First(&property, "id = ?", property.ID)

	c.JSON(http.StatusCreated, gin.H{"data": property, "message": "property created successfully"})
}

// PUT /api/v1/properties/:id
func (h *PropertyHandler) UpdateProperty(c *gin.Context) {
	id := c.Param("id")
	callerID, _ := c.Get("userID")

	var property models.Property
	if err := h.db.First(&property, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not authorized to update this property", "code": "FORBIDDEN"})
		return
	}

	var req updatePropertyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	updates := map[string]interface{}{}
	if req.Street != nil && *req.Street != "" {
		updates["street"] = *req.Street
	}
	if req.City != nil && *req.City != "" {
		updates["city"] = *req.City
	}
	if req.State != nil && *req.State != "" {
		updates["state"] = *req.State
	}
	if req.ZipCode != nil {
		updates["zip_code"] = *req.ZipCode
	}
	if req.Country != nil && *req.Country != "" {
		updates["country"] = *req.Country
	}
	if req.Price != nil && *req.Price > 0 {
		updates["price"] = *req.Price
	}
	if req.Type != nil {
		updates["type"] = *req.Type
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Bedrooms != nil {
		updates["bedrooms"] = *req.Bedrooms
	}
	if req.Bathrooms != nil {
		updates["bathrooms"] = *req.Bathrooms
	}
	if req.SquareFeet != nil {
		updates["square_feet"] = *req.SquareFeet
	}
	if req.LotSize != nil {
		updates["lot_size"] = *req.LotSize
	}
	if req.YearBuilt != nil {
		updates["year_built"] = *req.YearBuilt
	}
	if req.Latitude != nil {
		updates["latitude"] = *req.Latitude
	}
	if req.Longitude != nil {
		updates["longitude"] = *req.Longitude
	}
	if req.Source != nil {
		updates["source"] = *req.Source
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	if len(updates) > 0 {
		if err := h.db.Model(&property).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update property", "code": "INTERNAL_ERROR"})
			return
		}
	}

	h.db.Preload("Images").Preload("Seller").First(&property, "id = ?", id)

	c.JSON(http.StatusOK, gin.H{"data": property, "message": "property updated successfully"})
}

// DELETE /api/v1/properties/:id
func (h *PropertyHandler) DeleteProperty(c *gin.Context) {
	id := c.Param("id")
	callerID, _ := c.Get("userID")

	var property models.Property
	if err := h.db.First(&property, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "property not found", "code": "NOT_FOUND"})
		return
	}

	if property.SellerID == nil || *property.SellerID != callerID.(string) {
		c.JSON(http.StatusForbidden, gin.H{"error": "not authorized to delete this property", "code": "FORBIDDEN"})
		return
	}

	if err := h.db.Model(&property).Update("status", models.PropertyStatusDeleted).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete property", "code": "INTERNAL_ERROR"})
		return
	}

	c.Status(http.StatusNoContent)
}
