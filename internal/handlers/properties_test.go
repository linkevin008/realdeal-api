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
	r.GET("/users/me/listings", func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		h.ListMyListings(c)
	})
	return r
}

func propertyColumns() []string {
	return []string{
		"id", "street", "city", "state", "postal_code", "country",
		"price", "type", "description", "bedrooms", "bathrooms",
		"square_feet", "lot_size", "year_built", "latitude", "longitude",
		"source", "seller_id", "status", "created_at", "updated_at",
	}
}

func propertyRowWithStatus(id, sellerID, status string) []driver.Value {
	now := time.Now()
	return []driver.Value{
		id, "123 Main St", "Springfield", "IL", "62701", "US",
		250000.0, "house", "Nice house", nil, nil,
		nil, nil, nil, 39.78, -89.65,
		"user_generated", sellerID, status, now, now,
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
	t.Parallel()
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

// TestListProperties_Filters consolidates all query-parameter filter cases.
// Each subtest verifies the handler returns 200 with a specific filter applied.
func TestListProperties_Filters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
		count int
		check func(t *testing.T, resp map[string]interface{})
	}{
		{name: "price range", query: "?price_min=100000&price_max=500000"},
		{name: "type", query: "?type=house"},
		{
			name:  "pagination",
			query: "?page=2&limit=10",
			count: 50,
			check: func(t *testing.T, resp map[string]interface{}) {
				assert.Equal(t, float64(2), resp["page"])
				assert.Equal(t, float64(10), resp["limit"])
			},
		},
		{name: "source", query: "?source=user_generated"},
		{name: "bedrooms and bathrooms", query: "?bedrooms_min=3&bathrooms_min=2"},
		{name: "seller", query: "?seller_id=seller-1", count: 1},
		{name: "location radius", query: "?lat=39.78&lon=-89.65&radius_miles=10"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gormDB, mock := newTestDB(t)
			h := handlers.NewPropertyHandler(gormDB)
			r := setupPropertyRouter(h, "")

			mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tt.count))
			mock.ExpectQuery(`SELECT .* FROM "properties"`).
				WillReturnRows(sqlmock.NewRows(propertyColumns()))
			mock.ExpectQuery(`SELECT .* FROM "property_images"`).
				WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectQuery(`SELECT .* FROM "users"`).
				WillReturnRows(sqlmock.NewRows([]string{"id"}))

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/properties"+tt.query, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			if tt.check != nil {
				var resp map[string]interface{}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				tt.check(t, resp)
			}
		})
	}
}

func TestGetProperty_Success(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	expectNoConfirmedTrustEvent(mock)
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
		"postal_code":   "62701",
		"bedrooms":      3,
		"bathrooms":     2.0,
		"square_feet":   1800,
		"year_built":    1998,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestCreateProperty_Unauthenticated(t *testing.T) {
	t.Parallel()
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
		"postal_code":   "62701",
		"bedrooms":      3,
		"bathrooms":     2.0,
		"square_feet":   1800,
		"year_built":    1998,
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestCreateProperty_InvalidInput consolidates all creation-time validation failures.
func TestCreateProperty_InvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body map[string]interface{}
	}{
		{
			name: "zero price",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"country": "US", "price": 0, "property_type": "house",
			},
		},
		{
			name: "missing required field (street)",
			body: map[string]interface{}{
				"city": "Springfield", "state": "IL", "country": "US",
				"price": 250000, "property_type": "house",
			},
		},
		{
			name: "invalid coordinates (latitude out of range)",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"postal_code": "62701", "country": "US", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
				"latitude": 200.0, "longitude": -89.65,
			},
		},
		{
			name: "full country name rejected",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"postal_code": "62701", "country": "United States", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
			},
		},
		{
			name: "alpha-3 country code rejected",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"postal_code": "62701", "country": "USA", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
			},
		},
		{
			name: "US postal must be ZIP format",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"postal_code": "A1A 1A1", "country": "US", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
			},
		},
		{
			name: "CA postal must be postal format",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Toronto", "state": "ON",
				"postal_code": "90210", "country": "CA", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
			},
		},
		{
			name: "invalid state code for country",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "Illinois",
				"postal_code": "62701", "country": "US", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
			},
		},
		{
			name: "state code from wrong country",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Toronto", "state": "ON",
				"postal_code": "62701", "country": "US", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
			},
		},
		{
			name: "missing postal code",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"country": "US", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
			},
		},
		{
			name: "missing specifications (bedrooms)",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"postal_code": "62701", "country": "US", "price": 250000, "property_type": "house",
				"bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
			},
		},
		{
			name: "zero square feet rejected",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"postal_code": "62701", "country": "US", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 0, "year_built": 1998,
			},
		},
		{
			name: "missing year built",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"postal_code": "62701", "country": "US", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800,
			},
		},
		{
			name: "implausible year built",
			body: map[string]interface{}{
				"street": "123 Main St", "city": "Springfield", "state": "IL",
				"postal_code": "62701", "country": "US", "price": 250000, "property_type": "house",
				"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1500,
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gormDB, _ := newTestDB(t)
			h := handlers.NewPropertyHandler(gormDB)
			r := setupPropertyRouter(h, "seller-1")

			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

// Explicit zero bedrooms/bathrooms (e.g. land) are valid as long as the
// fields are sent, and CA postal codes pass their country's format.
func TestCreateProperty_ZeroBedroomsCA(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	expectNoConfirmedTrustEvent(mock)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("new-prop-id", time.Now(), time.Now()))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("new-prop-id", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	body, _ := json.Marshal(map[string]interface{}{
		"street": "88 Scott St", "city": "Toronto", "state": "ON",
		"postal_code": "M5E 0A9", "country": "CA",
		"price": 900000.0, "property_type": "land",
		"bedrooms": 0, "bathrooms": 0.0, "square_feet": 5000, "year_built": 1990,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

// The country dropdown's source of truth: a public endpoint listing the
// countries listings may be created in.
func TestSupportedCountries(t *testing.T) {
	t.Parallel()
	r := gin.New()
	r.GET("/config/countries", handlers.SupportedCountriesHandler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/config/countries", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Data []struct {
			Code         string `json:"code"`
			Subdivisions []struct {
				Code string `json:"code"`
				Name string `json:"name"`
			} `json:"subdivisions"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "US", resp.Data[0].Code)
	assert.Len(t, resp.Data[0].Subdivisions, 51) // 50 states + DC
	assert.Equal(t, "CA", resp.Data[1].Code)
	assert.Len(t, resp.Data[1].Subdivisions, 13) // 10 provinces + 3 territories

	// An unsupported (but real) country is rejected at create time
	gormDB, _ := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	pr := setupPropertyRouter(h, "seller-1")
	body, _ := json.Marshal(map[string]interface{}{
		"street": "12 Rue de Rivoli", "city": "Paris", "state": "Ile-de-France",
		"postal_code": "75004", "country": "FR",
		"price": 900000.0, "property_type": "house",
		"bedrooms": 2, "bathrooms": 1.0, "square_feet": 900, "year_built": 2005,
	})
	cw := httptest.NewRecorder()
	creq, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	creq.Header.Set("Content-Type", "application/json")
	pr.ServeHTTP(cw, creq)
	assert.Equal(t, http.StatusBadRequest, cw.Code)
}

func TestCreateProperty_WithImages(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	expectNoConfirmedTrustEvent(mock)
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
		"postal_code":   "62701",
		"bedrooms":      3,
		"bathrooms":     2.0,
		"square_feet":   1800,
		"year_built":    1998,
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

func TestUpdateProperty_Success(t *testing.T) {
	t.Parallel()
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

func TestUpdateProperty_ReplacesImages(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	sellerID := "seller-1"
	r := setupPropertyRouter(h, sellerID)

	// Fetch property
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))

	// Image replacement: delete existing, insert new (one transaction)
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "property_images"`).
		WithArgs("prop-1").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectQuery(`INSERT INTO "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("img-1"))
	mock.ExpectCommit()

	// Reload with associations (GORM adds the populated PK as an extra condition)
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", "prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", sellerID)...))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "property_id", "url", "order", "created_at"}).
			AddRow("img-1", "prop-1", "http://cdn/new.jpg", 0, time.Now()))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	// images only — no scalar field updates, so no UPDATE "properties" statement
	body, _ := json.Marshal(map[string]interface{}{
		"images": []map[string]interface{}{{"url": "http://cdn/new.jpg", "order": 0}},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	images := data["images"].([]interface{})
	require.Len(t, images, 1)
	assert.Equal(t, "http://cdn/new.jpg", images[0].(map[string]interface{})["url"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateProperty_Forbidden(t *testing.T) {
	t.Parallel()
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

func TestUpdateProperty_NotFound(t *testing.T) {
	t.Parallel()
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

func TestUpdateProperty_BadJSON(t *testing.T) {
	t.Parallel()
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

// TestUpdateProperty_AllFields covers all optional field assignments.
func TestUpdateProperty_AllFields(t *testing.T) {
	t.Parallel()
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

	body, _ := json.Marshal(map[string]interface{}{
		"street":     "456 Oak Ave",
		"state":      "CA",
		"country":    "CA",
		"postal_code": "90210",
		"type":       "condo",
		"year_built": 2005,
		"latitude":   34.05,
		"longitude":  -118.24,
		"source":     "mls",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUpdateProperty_MultipleFields(t *testing.T) {
	t.Parallel()
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

	body, _ := json.Marshal(map[string]interface{}{
		"price":       300000.0,
		"description": "Updated description",
		"bedrooms":    3,
		"bathrooms":   2.0,
		"square_feet": 1800, "year_built": 1998,
		"status":      "active",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDeleteProperty_Success(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// ListMyListings powers the iOS "My Listings" screen — unlike the public
// ListProperties feed (default status=active), the caller must see their own
// listing across active/pending/sold (a listing goes pending the moment an
// offer is accepted, exactly when the seller needs the contract path).

func TestListMyListings_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	sellerID := "seller-1"
	r := setupPropertyRouter(h, sellerID)

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE seller_id = \$1 AND status IN \(\$2,\$3,\$4\)`).
		WithArgs(sellerID, "active", "pending", "sold").
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRowWithStatus("prop-1", sellerID, "pending")...).
			AddRow(propertyRowWithStatus("prop-2", sellerID, "sold")...))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "property_id", "url", "order", "created_at"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/listings", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]interface{})
	assert.Len(t, data, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListMyListings_Empty(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("seller-1", "active", "pending", "sold").
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/listings", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp["data"])
}

func TestListMyListings_DBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("seller-1", "active", "pending", "sold").
		WillReturnError(fmt.Errorf("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/listings", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListMyListings_ExcludesDeletedAndOtherSellers(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	// The WithArgs match on seller_id + the active/pending/sold status list
	// is itself the assertion: a deleted listing or another seller's listing
	// would never satisfy this query, so returning only the caller's rows
	// (regardless of which of the three statuses they're in) proves the
	// filter is doing its job.
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("seller-1", "active", "pending", "sold").
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRowWithStatus("prop-1", "seller-1", "active")...))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "property_id", "url", "order", "created_at"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sellerRows())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/listings", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]interface{})
	assert.Len(t, data, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}
