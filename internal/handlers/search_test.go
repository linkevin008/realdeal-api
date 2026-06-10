package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSearchRouter(h *handlers.SearchHandler) *gin.Engine {
	r := gin.New()
	r.GET("/search/properties", h.SearchProperties)
	return r
}

func searchRequest(t *testing.T, r *gin.Engine, url string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	return w
}

func expectSearchPreloads(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "property_id", "url", "order", "created_at"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(sellerRows())
}

func TestSearchProperties_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewSearchHandler(gormDB)
	r := setupSearchRouter(h)

	// Only the active-status filter applies by default
	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WithArgs("active").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT .* FROM "properties" .*ORDER BY created_at DESC`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	expectSearchPreloads(mock)

	w := searchRequest(t, r, "/search/properties")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp["data"], 1)
	assert.Equal(t, float64(1), resp["total"])
	assert.Equal(t, float64(1), resp["page"])
	assert.Equal(t, float64(20), resp["limit"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSearchProperties_FiltersApplied(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewSearchHandler(gormDB)
	r := setupSearchRouter(h)

	// status, q (x3 ILIKE), min_price, max_price, beds, property_type, city
	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties" WHERE status = .* ILIKE .* price >= .* price <= .* bedrooms >= .* type IN .* city ILIKE`).
		WithArgs("active", "%maple%", "%maple%", "%maple%", 100000.0, 500000.0, 3, "house", "Springfield").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := searchRequest(t, r, "/search/properties?q=maple&min_price=100000&max_price=500000&beds=3&property_type=house&city=Springfield")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSearchProperties_SellerSourceAndGeoFilters(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewSearchHandler(gormDB)
	r := setupSearchRouter(h)

	// status, source IN, seller_id, haversine (lat, lon, lat, radiusKm)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties" WHERE status = .* source IN .* seller_id = .* acos`).
		WithArgs("active", "mls", "user_generated", "seller-1", 39.78, -89.65, 39.78, 10*1.60934).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := searchRequest(t, r, "/search/properties?source=mls,user_generated&seller_id=seller-1&lat=39.78&lon=-89.65&radius_miles=10")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSearchProperties_SortPriceAsc(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewSearchHandler(gormDB)
	r := setupSearchRouter(h)

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM "properties" .*ORDER BY price ASC`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := searchRequest(t, r, "/search/properties?sort=price_asc")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSearchProperties_InvalidSort(t *testing.T) {
	t.Parallel()
	gormDB, _ := newTestDB(t)
	h := handlers.NewSearchHandler(gormDB)
	r := setupSearchRouter(h)

	w := searchRequest(t, r, "/search/properties?sort=cheapest")

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSearchProperties_LimitCapped(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewSearchHandler(gormDB)
	r := setupSearchRouter(h)

	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	// LIMIT is parameterized — assert the capped value via query args
	mock.ExpectQuery(`SELECT .* FROM "properties" .*LIMIT`).
		WithArgs("active", 100).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := searchRequest(t, r, "/search/properties?limit=500")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(100), resp["limit"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSearchProperties_InvalidNumericParamsIgnored(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewSearchHandler(gormDB)
	r := setupSearchRouter(h)

	// Unparseable numbers fall back to no filter — only the status arg remains
	mock.ExpectQuery(`SELECT count\(\*\) FROM "properties"`).
		WithArgs("active").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := searchRequest(t, r, "/search/properties?min_price=abc&beds=many&page=zero")

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}
