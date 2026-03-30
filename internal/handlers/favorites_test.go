package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupFavoriteRouter(h *handlers.FavoriteHandler, callerID string) *gin.Engine {
	r := gin.New()
	r.GET("/users/:id/favorites", func(c *gin.Context) {
		c.Set("userID", callerID)
		h.ListFavorites(c)
	})
	r.POST("/users/:id/favorites", func(c *gin.Context) {
		c.Set("userID", callerID)
		h.AddFavorite(c)
	})
	r.DELETE("/users/:id/favorites/:propertyId", func(c *gin.Context) {
		c.Set("userID", callerID)
		h.RemoveFavorite(c)
	})
	return r
}

func favoriteColumns() []string {
	return []string{"id", "user_id", "property_id", "saved_at"}
}

func TestListFavorites_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "favorites"`).
		WillReturnRows(sqlmock.NewRows(favoriteColumns()).
			AddRow("fav-1", "user-1", "prop-1", time.Now()))

	// Preload Property
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "seller-1")...))

	// Preload Property.Images
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/user-1/favorites", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["total"])
}

func TestListFavorites_Forbidden(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	// callerID is different from the user whose favorites are requested
	r := setupFavoriteRouter(h, "other-user")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/user-1/favorites", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAddFavorite_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "user-1")

	// Check property exists
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "seller-1")...))

	// Insert favorite
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "favorites"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "saved_at"}).
			AddRow("fav-new", time.Now()))
	mock.ExpectCommit()

	// Reload with Property.Images
	mock.ExpectQuery(`SELECT .* FROM "favorites"`).
		WithArgs("fav-new", 1).
		WillReturnRows(sqlmock.NewRows(favoriteColumns()).
			AddRow("fav-new", "user-1", "prop-1", time.Now()))

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "seller-1")...))

	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	body, _ := json.Marshal(map[string]interface{}{
		"property_id": "prop-1",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/user-1/favorites", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestAddFavorite_Duplicate(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "user-1")

	// Property exists
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("prop-1", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).
			AddRow(propertyRow("prop-1", "seller-1")...))

	// Insert fails with unique constraint
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "favorites"`).
		WillReturnError(fmt.Errorf("ERROR: duplicate key value violates unique constraint"))
	mock.ExpectRollback()

	body, _ := json.Marshal(map[string]interface{}{
		"property_id": "prop-1",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/user-1/favorites", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestRemoveFavorite_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "user-1")

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "favorites"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/users/user-1/favorites/prop-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestRemoveFavorite_NotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "user-1")

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "favorites"`).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/users/user-1/favorites/no-such-prop", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRemoveFavorite_Forbidden(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "other-user")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/users/user-1/favorites/prop-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAddFavorite_Forbidden(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "other-user")

	body, _ := json.Marshal(map[string]interface{}{"property_id": "prop-1"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/user-1/favorites", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAddFavorite_PropertyNotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WithArgs("no-such-prop", 1).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	body, _ := json.Marshal(map[string]interface{}{"property_id": "no-such-prop"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/user-1/favorites", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAddFavorite_BadJSON(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "user-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/user-1/favorites", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListFavorites_DBError(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewFavoriteHandler(gormDB)
	r := setupFavoriteRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "favorites"`).
		WillReturnError(fmt.Errorf("connection refused"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/user-1/favorites", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
