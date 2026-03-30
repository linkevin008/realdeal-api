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

func setupUserRouter(h *handlers.UserHandler, callerID string) *gin.Engine {
	r := gin.New()
	r.GET("/users/:id", func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		h.GetUser(c)
	})
	r.PUT("/users/:id", func(c *gin.Context) {
		c.Set("userID", callerID)
		h.UpdateUser(c)
	})
	r.DELETE("/users/:id", func(c *gin.Context) {
		c.Set("userID", callerID)
		h.DeleteUser(c)
	})
	return r
}

func TestGetUser_Self(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-1", "Alice", "alice@example.com", "hash")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/user-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	// Own profile: email should be visible
	assert.Equal(t, "alice@example.com", data["email"])
}

func TestGetUser_Other_HidesFields(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	// callerID = "caller-id" fetching user-2's profile
	r := setupUserRouter(h, "caller-id")

	// show_email = false
	now := time.Now()
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-2", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow("user-2", "Bob", "bob@example.com", "hash", nil, nil, "seller", false, false, true, now, now))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/user-2", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	// show_email=false → email should be blank
	assert.Equal(t, "", data["email"])
}

func TestGetUser_NotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "caller-id")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("no-such-user", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/no-such-user", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateUser_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "user-1")

	// First: fetch user to update
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-1", "Alice", "alice@example.com", "hash")...))

	// Update
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "users"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// Reload after update
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-1", "Alice Updated", "alice@example.com", "hash")...))

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{"name": "Alice Updated"})
	req, _ := http.NewRequest(http.MethodPut, "/users/user-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUpdateUser_Forbidden(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	// callerID is different from the target user
	r := setupUserRouter(h, "other-user")

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{"name": "Hacked"})
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("/users/%s", "user-1"), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeleteUser_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-1", "Alice", "alice@example.com", "hash")...))

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "users"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/users/user-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteUser_Forbidden(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "other-user")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/users/user-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeleteUser_NotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/users/user-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteUser_DBError(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-1", "Alice", "alice@example.com", "hash")...))

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "users"`).
		WillReturnError(fmt.Errorf("db error"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/users/user-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdateUser_AllFields(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-1", "Alice", "alice@example.com", "hash")...))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "users"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-1", "Alice Updated", "alice@example.com", "hash")...))

	phone := "+1-555-0100"
	photo := "https://example.com/photo.jpg"
	showEmail := false
	showPhone := false
	showListings := true

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{
		"name":              "Alice Updated",
		"phone_number":      phone,
		"profile_photo_url": photo,
		"show_email":        showEmail,
		"show_phone":        showPhone,
		"show_listings":     showListings,
	})
	req, _ := http.NewRequest(http.MethodPut, "/users/user-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUpdateUser_BadJSON(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "user-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/users/user-1", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateUser_NotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupUserRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{"name": "Ghost"})
	req, _ := http.NewRequest(http.MethodPut, "/users/user-1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ----- GetMe tests -----

func setupMeRouter(h *handlers.UserHandler, callerID string) *gin.Engine {
	r := gin.New()
	r.GET("/users/me", func(c *gin.Context) {
		c.Set("userID", callerID)
		h.GetMe(c)
	})
	return r
}

func TestGetMe_Success(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupMeRouter(h, "user-1")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user-1", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-1", "Alice", "alice@example.com", "hash")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "alice@example.com", data["email"])
}

func TestGetMe_NotFound(t *testing.T) {
	gormDB, mock := newTestDB(t)
	h := handlers.NewUserHandler(gormDB)
	r := setupMeRouter(h, "ghost-user")

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("ghost-user", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
