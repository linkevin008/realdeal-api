package handlers_test

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/kevinlin/realdeal-api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPropertyRouter(h *handlers.PropertyHandler, callerID string) *gin.Engine {
	r := gin.New()
	r.GET("/properties", h.ListProperties)
	r.GET("/properties/:id", h.GetProperty)
	r.POST("/properties", func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		h.CreateProperty(c)
	})
	r.PUT("/properties/:id", func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		h.UpdateProperty(c)
	})
	r.DELETE("/properties/:id", func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		h.DeleteProperty(c)
	})
	return r
}

func propertyColumns() []string {
	return []string{
		"id", "street", "city", "state", "zip_code", "country",
		"price", "type", "description", "bedrooms", "bathrooms",
		"square_feet", "lot_size", "year_built", "latitude", "longitude",
		"source", "seller_id", "status", "created_at", "updated_at",
	}
}

func propertyRow(id, sellerID string) []driver.Value {
	now := time.Now()
	return []driver.Value{
		id, "123 Main St", "Springfield", "IL", "62701", "US",
		250000.0, "house", "Nice house", nil, nil,
		nil, nil, nil, 39.78, -89.65,
		"user_generated", sellerID, "active", now, now,
	}
}

func sellerRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "email", "password_hash", "phone_number", "profile_photo_url", "role", "show_email", "show_phone", "show_listings", "created_at", "updated_at"}).
		AddRow("seller-1", "Seller", "seller@example.com", "hash", nil, nil, "seller", true, true, true, time.Now(), time.Now())
}

func TestListProperties_NoFilters(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	// Count query
	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// Main query
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "seller-1")...))

	// Preload Images
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "property_id", "url", "order", "created_at"}))

	// Preload Seller
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["total"])
	assert.Equal(t, float64(1), resp["page"])
}

func TestListProperties_PriceFilter(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties?price_min=100000&price_max=500000", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListProperties_TypeFilter(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties?type=house", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListProperties_Pagination(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties?page=2&limit=10", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(2), resp["page"])
	assert.Equal(t, float64(10), resp["limit"])
}

func TestGetProperty_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "seller-1")...))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "property_id", "url", "order", "created_at"}))

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "prop-1", data["id"])
}

func TestGetProperty_NotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("no-such-prop", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/no-such-prop", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateProperty_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("new-prop-id", time.Now(), time.Now()))
	mock.ExpectCommit()

	// Reload with associations
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("new-prop-id", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("new-prop-id", "seller-1")...))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	body, _ := json.Marshal(map[string]interface{}{
		"street":        "123 Main St",
		"city":          "Springfield",
		"state":         "IL",
		"country":       "US",
		"price":         250000,
		"property_type": "house",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestCreateProperty_Unauthenticated(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	// Simulate the auth middleware blocking unauthenticated requests
	r := gin.New()
	cfg := &config.Config{JWTSecret: jwtTestSecret}
	r.POST("/properties",
		middleware.AuthMiddleware(cfg),
		h.CreateProperty,
	)

	body, _ := json.Marshal(map[string]interface{}{
		"street":        "123 Main St",
		"city":          "Springfield",
		"state":         "IL",
		"country":       "US",
		"price":         250000,
		"property_type": "house",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateProperty_InvalidPrice(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	body, _ := json.Marshal(map[string]interface{}{
		"street":        "123 Main St",
		"city":          "Springfield",
		"state":         "IL",
		"country":       "US",
		"price":         0,
		"property_type": "house",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateProperty_MissingRequired(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	// Missing "street"
	body, _ := json.Marshal(map[string]interface{}{
		"city":          "Springfield",
		"state":         "IL",
		"country":       "US",
		"price":         250000,
		"property_type": "house",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateProperty_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	sellerID := "seller-1"
	r := setupPropertyRouter(h, sellerID)

	// Fetch property
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))

	// Update
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "properties"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// Reload
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	body, _ := json.Marshal(map[string]interface{}{"city": "New City"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUpdateProperty_Forbidden(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	// callerID != sellerID
	r := setupPropertyRouter(h, "other-user")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "real-seller-id")...))

	body, _ := json.Marshal(map[string]interface{}{"city": "Hacked"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeleteProperty_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	sellerID := "seller-1"
	r := setupPropertyRouter(h, sellerID)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))

	// Soft delete: UPDATE status = 'deleted'
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "properties"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteProperty_Forbidden(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "other-user")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "real-seller-id")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeleteProperty_NotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("no-such-prop", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/no-such-prop", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteProperty_DBError(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	sellerID := "seller-1"
	r := setupPropertyRouter(h, sellerID)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "properties"`).
		WillReturnError(fmt.Errorf("db error"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Covers the remaining optional field assignments: street, state, country, zip_code,
// type, lot_size, year_built, latitude, longitude, source
func TestUpdateProperty_AllFields(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	sellerID := "seller-1"
	r := setupPropertyRouter(h, sellerID)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "properties"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	street := "456 Oak Ave"
	state := "CA"
	country := "CA"
	zip := "90210"
	propType := "condo"
	lotSize := 5000.0
	yearBuilt := 2005
	lat := 34.05
	lon := -118.24
	source := "mls"

	body, _ := json.Marshal(map[string]interface{}{
		"street":     street,
		"state":      state,
		"country":    country,
		"zip_code":   zip,
		"type":       propType,
		"lot_size":   lotSize,
		"year_built": yearBuilt,
		"latitude":   lat,
		"longitude":  lon,
		"source":     source,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUpdateProperty_MultipleFields(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	sellerID := "seller-1"
	r := setupPropertyRouter(h, sellerID)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "properties"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	price := 300000.0
	desc := "Updated description"
	bedrooms := 3
	bathrooms := 2.0
	sqft := 1800
	status := "active"
	body, _ := json.Marshal(map[string]interface{}{
		"price":       price,
		"description": desc,
		"bedrooms":    bedrooms,
		"bathrooms":   bathrooms,
		"square_feet": sqft,
		"status":      status,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUpdateProperty_NotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("no-such-prop", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	body, _ := json.Marshal(map[string]interface{}{"city": "Nowhere"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/no-such-prop", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListProperties_SourceFilter(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties?source=user_generated", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListProperties_BedroomsFilter(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties?bedrooms_min=3&bathrooms_min=2", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListProperties_SellerFilter(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties?seller_id=seller-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCreateProperty_WithImages(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("new-prop-id", time.Now(), time.Now()))
	mock.ExpectQuery(`INSERT INTO "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("img-1").AddRow("img-2"))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("new-prop-id", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("new-prop-id", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	body, _ := json.Marshal(map[string]interface{}{
		"street":        "123 Main St",
		"city":          "Springfield",
		"state":         "IL",
		"country":       "US",
		"price":         250000,
		"property_type": "house",
		"images": []map[string]interface{}{
			{"url": "https://example.com/photo1.jpg", "order": 0},
			{"url": "https://example.com/photo2.jpg", "order": 1},
			{"url": "", "order": 2}, // empty URL — should be skipped
		},
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestCreateProperty_InvalidCoordinates(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	body, _ := json.Marshal(map[string]interface{}{
		"street":        "123 Main St",
		"city":          "Springfield",
		"state":         "IL",
		"country":       "US",
		"price":         250000,
		"property_type": "house",
		"latitude":      200.0,
		"longitude":     -89.65,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateProperty_BadJSON(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListProperties_LocationFilter(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties?lat=39.78&lon=-89.65&radius_miles=10", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
