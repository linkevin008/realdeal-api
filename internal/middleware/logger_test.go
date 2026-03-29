package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/middleware"
	"github.com/stretchr/testify/assert"
)

func TestLoggerMiddleware_LogsRequest(t *testing.T) {
	w := httptest.NewRecorder()
	r := gin.New()
	r.Use(middleware.Logger())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test?foo=bar", nil)
	r.ServeHTTP(w, req)

	// Middleware should not panic and request should pass through normally
	assert.Equal(t, http.StatusOK, w.Code)
}
