package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

// SearchHandler serves the lookup service's read-only listing search.
// It only ever reads — the lookup service connects as a SELECT-only DB user,
// so any write would fail at the database level.
type SearchHandler struct {
	db *gorm.DB
}

func NewSearchHandler(db *gorm.DB) *SearchHandler {
	return &SearchHandler{db: db}
}

const (
	searchDefaultLimit = 20
	searchMaxLimit     = 100
)

// validSearchSorts maps the sort query param to an ORDER BY clause.
var validSearchSorts = map[string]string{
	"price_asc":  "price ASC",
	"price_desc": "price DESC",
	"newest":     "created_at DESC",
}

// SearchProperties godoc
// GET /api/v1/search/properties
// Query params: q, min_price, max_price, beds, baths, property_type, city,
// state, sort (price_asc|price_desc|newest), page, limit.
// Only active listings are returned.
func (h *SearchHandler) SearchProperties(c *gin.Context) {
	query := h.db.Model(&models.Property{}).
		Preload("Images").
		Preload("Seller").
		Where("status = ?", models.PropertyStatusActive)

	// Free-text match across address and description
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		pattern := "%" + q + "%"
		query = query.Where(
			"street ILIKE ? OR city ILIKE ? OR description ILIKE ?",
			pattern, pattern, pattern,
		)
	}

	if minPrice := c.Query("min_price"); minPrice != "" {
		if v, err := strconv.ParseFloat(minPrice, 64); err == nil {
			query = query.Where("price >= ?", v)
		}
	}
	if maxPrice := c.Query("max_price"); maxPrice != "" {
		if v, err := strconv.ParseFloat(maxPrice, 64); err == nil {
			query = query.Where("price <= ?", v)
		}
	}

	if beds := c.Query("beds"); beds != "" {
		if v, err := strconv.Atoi(beds); err == nil {
			query = query.Where("bedrooms >= ?", v)
		}
	}
	if baths := c.Query("baths"); baths != "" {
		if v, err := strconv.ParseFloat(baths, 64); err == nil {
			query = query.Where("bathrooms >= ?", v)
		}
	}

	if t := c.Query("property_type"); t != "" {
		query = query.Where("type IN ?", strings.Split(t, ","))
	}

	if city := c.Query("city"); city != "" {
		query = query.Where("city ILIKE ?", city)
	}
	if state := c.Query("state"); state != "" {
		query = query.Where("state ILIKE ?", state)
	}

	order, ok := validSearchSorts[c.DefaultQuery("sort", "newest")]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sort: must be one of price_asc, price_desc, newest", "code": "VALIDATION_ERROR"})
		return
	}
	query = query.Order(order)

	var total int64
	query.Count(&total)

	page := 1
	limit := searchDefaultLimit
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
	if limit > searchMaxLimit {
		limit = searchMaxLimit
	}
	offset := (page - 1) * limit

	var properties []models.Property
	if err := query.Offset(offset).Limit(limit).Find(&properties).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search properties", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  properties,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
