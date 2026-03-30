package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/stretchr/testify/assert"
)

func TestHealth_Healthy(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewHealthHandler(gormDB)

	r := gin.New()
	r.GET("/health", h.Health)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

