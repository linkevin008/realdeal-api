package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	PostalCode  string               `json:"postal_code" binding:"required"`
	Country     string               `json:"country" binding:"required"`
	Price       float64              `json:"price" binding:"required,gt=0"`
	Type        models.PropertyType  `json:"property_type" binding:"required"`
	Description string               `json:"description"`
	// Specifications are required on a listing; pointers so an explicit 0
	// (e.g. land with no bedrooms) still passes "required"
	Bedrooms    *int                 `json:"bedrooms" binding:"required,gte=0"`
	Bathrooms   *float64             `json:"bathrooms" binding:"required,gte=0"`
	SquareFeet  *int                 `json:"square_feet" binding:"required,gt=0"`
	YearBuilt   *int                 `json:"year_built" binding:"required"`
	Latitude    float64              `json:"latitude"`
	Longitude   float64              `json:"longitude"`
	Source      models.ListingSource `json:"source"`
	Images      []struct {
		URL   string `json:"url"`
		Order int    `json:"order"`
	} `json:"images"`
}

// Postal formats per supported country (see SupportedCountries in countries.go).
var postalPatterns = map[string]*regexp.Regexp{
	"US": regexp.MustCompile(`^\d{5}(-\d{4})?$`),
	"CA": regexp.MustCompile(`^[A-Za-z]\d[A-Za-z][ -]?\d[A-Za-z]\d$`),
}

// validYearBuilt sanity-checks a construction year (no listings from before
// 1800 or from the future; +1 allows new construction completing next year).
func validYearBuilt(year int) bool {
	return year >= 1800 && year <= time.Now().Year()+1
}

// validateAddressFields returns a human-readable error for invalid
// country/state/postal combinations, or "" when valid. Listings may only be
// created in supported countries, and where a country defines subdivisions the
// state must be one of their codes — the same lists the client's dropdowns use.
func validateAddressFields(country, state, postalCode string) string {
	if !supportedCountrySet[country] {
		return fmt.Sprintf("country %q is not supported yet — supported: %s", country, strings.Join(supportedCountryCodes(), ", "))
	}
	if subdivisions, ok := subdivisionSets[country]; ok && !subdivisions[state] {
		return fmt.Sprintf("invalid state/province code %q for country %s", state, country)
	}
	if pattern, ok := postalPatterns[country]; ok && !pattern.MatchString(postalCode) {
		return fmt.Sprintf("invalid postal code for country %s", country)
	}
	return ""
}

type updatePropertyRequest struct {
	Street      *string               `json:"street"`
	City        *string               `json:"city"`
	State       *string               `json:"state"`
	PostalCode  *string               `json:"postal_code"`
	Country     *string               `json:"country"`
	Price       *float64              `json:"price"`
	Type        *models.PropertyType  `json:"property_type"`
	Description *string               `json:"description"`
	Bedrooms    *int                  `json:"bedrooms"`
	Bathrooms   *float64              `json:"bathrooms"`
	SquareFeet  *int                  `json:"square_feet"`
	YearBuilt   *int                  `json:"year_built"`
	Latitude    *float64              `json:"latitude"`
	Longitude   *float64              `json:"longitude"`
	Source      *models.ListingSource `json:"source"`
	Status      *models.PropertyStatus `json:"status"`
	// When present, replaces the property's full image set (nil = leave unchanged)
	Images      *[]struct {
		URL   string `json:"url"`
		Order int    `json:"order"`
	} `json:"images"`
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

	if msg := validateAddressFields(req.Country, req.State, req.PostalCode); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "VALIDATION_ERROR"})
		return
	}

	if !validYearBuilt(*req.YearBuilt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "year_built must be a plausible construction year", "code": "VALIDATION_ERROR"})
		return
	}

	if flagged, err := hasConfirmedTrustEvent(h.db, sellerID, models.TrustEventDeedDefault, models.TrustEventDocumentFraud); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create property", "code": "INTERNAL_ERROR"})
		return
	} else if flagged {
		c.JSON(http.StatusForbidden, gin.H{"error": "listings cannot be created from this account", "code": "FORBIDDEN"})
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
		PostalCode:  req.PostalCode,
		Country:     req.Country,
		Price:       req.Price,
		Type:        req.Type,
		Description: req.Description,
		Bedrooms:    req.Bedrooms,
		Bathrooms:   req.Bathrooms,
		SquareFeet:  req.SquareFeet,
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
	if req.PostalCode != nil {
		updates["postal_code"] = *req.PostalCode
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
	if req.YearBuilt != nil {
		if !validYearBuilt(*req.YearBuilt) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "year_built must be a plausible construction year", "code": "VALIDATION_ERROR"})
			return
		}
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

	// Replace the image set when provided (delete-then-insert in one transaction)
	if req.Images != nil {
		newImages := make([]models.PropertyImage, 0, len(*req.Images))
		for _, img := range *req.Images {
			if img.URL != "" {
				newImages = append(newImages, models.PropertyImage{PropertyID: id, URL: img.URL, Order: img.Order})
			}
		}
		err := h.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("property_id = ?", id).Delete(&models.PropertyImage{}).Error; err != nil {
				return err
			}
			if len(newImages) == 0 {
				return nil
			}
			return tx.Create(&newImages).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update property images", "code": "INTERNAL_ERROR"})
			return
		}
	}

	h.db.Preload("Images").Preload("Seller").First(&property, "id = ?", id)

	c.JSON(http.StatusOK, gin.H{"data": property, "message": "property updated successfully"})
}

// GET /api/v1/users/me/listings
//
// Returns the caller's own properties across active, pending, and sold
// statuses (deleted excluded) — unlike ListProperties (default status=active,
// used by the public search feed), a seller needs visibility into a listing
// that just went pending (offer accepted, contract path opening) or sold.
func (h *PropertyHandler) ListMyListings(c *gin.Context) {
	callerID := c.MustGet("userID").(string)

	var properties []models.Property
	if err := h.db.Preload("Images").Preload("Seller").
		Where("seller_id = ?", callerID).
		Where("status IN ?", []models.PropertyStatus{
			models.PropertyStatusActive,
			models.PropertyStatusPending,
			models.PropertyStatusSold,
		}).
		Order("created_at DESC").
		Find(&properties).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch listings", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": properties})
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
