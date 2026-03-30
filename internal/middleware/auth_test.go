package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/middleware"
	"github.com/stretchr/testify/assert"
)

const testSecret = "test-secret-key"

func init() {
	gin.SetMode(gin.TestMode)
}

func generateTestToken(userID string, tokenType string, secret string, expiry time.Duration) string {
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

func testConfig() *config.Config {
	return &config.Config{
		JWTSecret: testSecret,
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	token := generateTestToken("user-123", "access", testSecret, 15*time.Minute)

	w := httptest.NewRecorder()
	r := gin.New()
	var capturedUserID interface{}
	r.GET("/test", middleware.AuthMiddleware(cfg), func(c *gin.Context) {
		capturedUserID, _ = c.Get("userID")
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "user-123", capturedUserID)
}

// TestAuthMiddleware_Rejections consolidates all cases that should return 401.
func TestAuthMiddleware_Rejections(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	tests := []struct {
		name    string
		makeReq func() *http.Request
	}{
		{
			name: "missing Authorization header",
			makeReq: func() *http.Request {
				req, _ := http.NewRequest(http.MethodGet, "/test", nil)
				return req
			},
		},
		{
			name: "malformed header (Token scheme instead of Bearer)",
			makeReq: func() *http.Request {
				req, _ := http.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Token "+generateTestToken("user-123", "access", testSecret, 15*time.Minute))
				return req
			},
		},
		{
			name: "expired token",
			makeReq: func() *http.Request {
				req, _ := http.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Bearer "+generateTestToken("user-123", "access", testSecret, -1*time.Minute))
				return req
			},
		},
		{
			name: "invalid signature (wrong secret)",
			makeReq: func() *http.Request {
				req, _ := http.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Bearer "+generateTestToken("user-123", "access", "wrong-secret", 15*time.Minute))
				return req
			},
		},
		{
			name: "empty subject",
			makeReq: func() *http.Request {
				claims := jwt.MapClaims{
					"sub":  "",
					"type": "access",
					"exp":  time.Now().Add(15 * time.Minute).Unix(),
					"iat":  time.Now().Unix(),
				}
				token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
				signed, _ := token.SignedString([]byte(testSecret))
				req, _ := http.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Bearer "+signed)
				return req
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			r := gin.New()
			r.GET("/test", middleware.AuthMiddleware(cfg), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			r.ServeHTTP(w, tt.makeReq())
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}

func TestAuthMiddleware_EmptySubject(t *testing.T) {
	cfg := testConfig()
	claims := jwt.MapClaims{
		"sub":  "",
		"type": "access",
		"exp":  time.Now().Add(15 * time.Minute).Unix(),
		"iat":  time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(testSecret))

	w := httptest.NewRecorder()
	r := gin.New()
	r.GET("/test", middleware.AuthMiddleware(cfg), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
