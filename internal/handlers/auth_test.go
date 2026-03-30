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
	"github.com/golang-jwt/jwt/v5"
	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const jwtTestSecret = "test-secret-key-auth"

func testAuthConfig() *config.Config {
	return &config.Config{
		JWTSecret: jwtTestSecret,
	}
}

func newTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)

	return gormDB, mock
}

func generateToken(userID string, tokenType string, secret string, expiry time.Duration) string {
	claims := jwt.MapClaims{
		"sub":  userID,
		"type": tokenType,
		"exp":  time.Now().Add(expiry).Unix(),
		"iat":  time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(secret))
	return signed
}

func postJSON(router *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

// userColumns returns the column list for a user row.
func userColumns() []string {
	return []string{
		"id", "name", "email", "password_hash", "phone_number", "profile_photo_url",
		"role", "show_email", "show_phone", "show_listings", "created_at", "updated_at",
	}
}

func userRowValues(id, name, email, hash string) []driver.Value {
	now := time.Now()
	return []driver.Value{id, name, email, hash, nil, nil, "buyer", true, true, true, now, now}
}

// ----- Signup tests -----

func TestSignup_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	cfg := testAuthConfig()
	h := handlers.NewAuthHandler(gormDB, cfg)

	r := gin.New()
	r.POST("/signup", h.Signup)

	// First query: check email exists → return no rows
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("test@example.com", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// Insert new user
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("uuid-1", time.Now(), time.Now()))
	mock.ExpectCommit()

	w := postJSON(r, "/signup", map[string]interface{}{
		"name":     "Alice",
		"email":    "test@example.com",
		"password": "password123",
	})

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, data["access_token"])
	assert.NotEmpty(t, data["refresh_token"])
}

// TestSignup_InvalidInput consolidates all signup input-validation failure cases.
func TestSignup_InvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    map[string]interface{}
		rawBody string // non-empty triggers a raw (malformed-JSON) request
	}{
		{
			name: "missing email",
			body: map[string]interface{}{"name": "Alice", "password": "password123"},
		},
		{
			name: "missing password",
			body: map[string]interface{}{"name": "Alice", "email": "test@example.com"},
		},
		{
			name: "missing name",
			body: map[string]interface{}{"email": "test@example.com", "password": "password123"},
		},
		{
			name:    "bad JSON",
			rawBody: "not-json",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gormDB, _ := newTestDB(t)
			h := handlers.NewAuthHandler(gormDB, testAuthConfig())
			r := gin.New()
			r.POST("/signup", h.Signup)

			var w *httptest.ResponseRecorder
			if tt.rawBody != "" {
				w = httptest.NewRecorder()
				req, _ := http.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(tt.rawBody))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
			} else {
				w = postJSON(r, "/signup", tt.body)
			}
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestSignup_DBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	cfg := testAuthConfig()
	h := handlers.NewAuthHandler(gormDB, cfg)

	r := gin.New()
	r.POST("/signup", h.Signup)

	// Email does not exist
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("new@example.com", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// Insert fails
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "users"`).
		WillReturnError(fmt.Errorf("db connection lost"))
	mock.ExpectRollback()

	w := postJSON(r, "/signup", map[string]interface{}{
		"name":     "Alice",
		"email":    "new@example.com",
		"password": "password123",
	})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSignup_DBError(t *testing.T) {
	gormDB, mock := newTestDB(t)
	cfg := testAuthConfig()
	h := handlers.NewAuthHandler(gormDB, cfg)

	r := gin.New()
	r.POST("/signup", h.Signup)

	// Email does not exist
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("new@example.com", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// Insert fails
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "users"`).
		WillReturnError(fmt.Errorf("db connection lost"))
	mock.ExpectRollback()

	w := postJSON(r, "/signup", map[string]interface{}{
		"name":     "Alice",
		"email":    "new@example.com",
		"password": "password123",
	})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSignup_DuplicateEmail(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	cfg := testAuthConfig()
	h := handlers.NewAuthHandler(gormDB, cfg)

	r := gin.New()
	r.POST("/signup", h.Signup)

	// Email exists → return a row
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("dupe@example.com", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("existing-id", "Existing", "dupe@example.com", "hash")...))

	w := postJSON(r, "/signup", map[string]interface{}{
		"name":     "Bob",
		"email":    "dupe@example.com",
		"password": "password123",
	})

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "EMAIL_TAKEN", resp["code"])
}

// ----- Signin tests -----

func TestSignin_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	cfg := testAuthConfig()
	h := handlers.NewAuthHandler(gormDB, cfg)

	r := gin.New()
	r.POST("/signin", h.Signin)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.MinCost)
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user@example.com", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-id-1", "User", "user@example.com", string(hash))...))

	w := postJSON(r, "/signin", map[string]interface{}{
		"email":    "user@example.com",
		"password": "correctpass",
	})

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, data["access_token"])
	assert.NotEmpty(t, data["refresh_token"])
}

func TestSignin_WrongPassword(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	cfg := testAuthConfig()
	h := handlers.NewAuthHandler(gormDB, cfg)

	r := gin.New()
	r.POST("/signin", h.Signin)

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.MinCost)
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("user@example.com", 1).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("user-id-1", "User", "user@example.com", string(hash))...))

	w := postJSON(r, "/signin", map[string]interface{}{
		"email":    "user@example.com",
		"password": "wrongpass",
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "INVALID_CREDENTIALS", resp["code"])
}

func TestSignin_UserNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	cfg := testAuthConfig()
	h := handlers.NewAuthHandler(gormDB, cfg)

	r := gin.New()
	r.POST("/signin", h.Signin)

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WithArgs("nobody@example.com", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	w := postJSON(r, "/signin", map[string]interface{}{
		"email":    "nobody@example.com",
		"password": "anypass",
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "INVALID_CREDENTIALS", resp["code"])
}

func TestSignin_BadJSON(t *testing.T) {
	t.Parallel()
	gormDB, _ := newTestDB(t)
	h := handlers.NewAuthHandler(gormDB, testAuthConfig())

	r := gin.New()
	r.POST("/signin", h.Signin)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/signin", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ----- Refresh tests -----

func TestRefreshToken_Success(t *testing.T) {
	t.Parallel()
	gormDB, _ := newTestDB(t)
	cfg := testAuthConfig()
	h := handlers.NewAuthHandler(gormDB, cfg)

	r := gin.New()
	r.POST("/refresh", h.Refresh)

	refreshToken := generateToken("user-abc", "refresh", jwtTestSecret, 7*24*time.Hour)

	w := postJSON(r, "/refresh", map[string]interface{}{
		"refresh_token": refreshToken,
	})

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, data["access_token"])
	assert.NotEmpty(t, data["refresh_token"])
}

// TestRefreshToken_InvalidInputs consolidates all refresh-token rejection cases.
func TestRefreshToken_InvalidInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		makeBody func() map[string]interface{}
		wantCode int
		wantErr  string
	}{
		{
			name: "invalid token string",
			makeBody: func() map[string]interface{} {
				return map[string]interface{}{"refresh_token": "this.is.not.a.valid.token"}
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "wrong token type (access instead of refresh)",
			makeBody: func() map[string]interface{} {
				return map[string]interface{}{
					"refresh_token": generateToken("user-abc", "access", jwtTestSecret, 15*time.Minute),
				}
			},
			wantCode: http.StatusUnauthorized,
			wantErr:  "UNAUTHORIZED",
		},
		{
			name:     "missing body",
			makeBody: func() map[string]interface{} { return map[string]interface{}{} },
			wantCode: http.StatusBadRequest,
		},
		{
			name: "empty subject",
			makeBody: func() map[string]interface{} {
				claims := jwt.MapClaims{
					"sub":  "",
					"type": "refresh",
					"exp":  time.Now().Add(7 * 24 * time.Hour).Unix(),
					"iat":  time.Now().Unix(),
				}
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				signed, _ := token.SignedString([]byte(jwtTestSecret))
				return map[string]interface{}{"refresh_token": signed}
			},
			wantCode: http.StatusUnauthorized,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gormDB, _ := newTestDB(t)
			h := handlers.NewAuthHandler(gormDB, testAuthConfig())
			r := gin.New()
			r.POST("/refresh", h.Refresh)

			w := postJSON(r, "/refresh", tt.makeBody())
			assert.Equal(t, tt.wantCode, w.Code)
			if tt.wantErr != "" {
				var resp map[string]interface{}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.wantErr, resp["code"])
			}
		})
	}
}

// ----- Signout tests -----

func TestSignout_Success(t *testing.T) {
	t.Parallel()
	gormDB, _ := newTestDB(t)
	h := handlers.NewAuthHandler(gormDB, testAuthConfig())

	r := gin.New()
	r.POST("/signout", h.Signout)

	w := postJSON(r, "/signout", nil)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestRefreshToken_MissingBody(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewAuthHandler(gormDB, testAuthConfig())

	r := gin.New()
	r.POST("/refresh", h.Refresh)

	w := postJSON(r, "/refresh", map[string]interface{}{})

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ----- Signout tests -----

func TestSignout_Success(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewAuthHandler(gormDB, testAuthConfig())

	r := gin.New()
	r.POST("/signout", h.Signout)

	w := postJSON(r, "/signout", nil)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestSignup_BadJSON(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewAuthHandler(gormDB, testAuthConfig())

	r := gin.New()
	r.POST("/signup", h.Signup)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSignin_BadJSON(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewAuthHandler(gormDB, testAuthConfig())

	r := gin.New()
	r.POST("/signin", h.Signin)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/signin", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRefreshToken_EmptySubject(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewAuthHandler(gormDB, testAuthConfig())

	r := gin.New()
	r.POST("/refresh", h.Refresh)

	claims := jwt.MapClaims{
		"sub":  "",
		"type": "refresh",
		"exp":  time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(jwtTestSecret))

	w := postJSON(r, "/refresh", map[string]interface{}{
		"refresh_token": signed,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
