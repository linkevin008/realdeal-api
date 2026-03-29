package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewAuthHandler(db *gorm.DB, cfg *config.Config) *AuthHandler {
	return &AuthHandler{db: db, cfg: cfg}
}

type signupRequest struct {
	Name     string          `json:"name" binding:"required"`
	Email    string          `json:"email" binding:"required,email"`
	Password string          `json:"password" binding:"required,min=6"`
	Role     models.UserRole `json:"role"`
}

type signinRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type authResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	ExpiresAt    time.Time   `json:"expires_at"`
	User         *models.User `json:"user,omitempty"`
}

func (h *AuthHandler) generateTokens(userID string) (accessToken, refreshToken string, expiresAt time.Time, err error) {
	expiresAt = time.Now().Add(15 * time.Minute)

	accessClaims := jwt.MapClaims{
		"sub":  userID,
		"exp":  expiresAt.Unix(),
		"iat":  time.Now().Unix(),
		"type": "access",
	}
	at := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessToken, err = at.SignedString([]byte(h.cfg.JWTSecret))
	if err != nil {
		return
	}

	refreshExp := time.Now().Add(7 * 24 * time.Hour)
	refreshClaims := jwt.MapClaims{
		"sub":  userID,
		"exp":  refreshExp.Unix(),
		"iat":  time.Now().Unix(),
		"type": "refresh",
	}
	rt := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshToken, err = rt.SignedString([]byte(h.cfg.JWTSecret))
	return
}

// POST /api/v1/auth/signup
func (h *AuthHandler) Signup(c *gin.Context) {
	var req signupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	// Default role
	if req.Role == "" {
		req.Role = models.UserRoleBuyer
	}

	// Check if email already exists
	var existing models.User
	if err := h.db.Where("email = ?", req.Email).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email already in use", "code": "EMAIL_TAKEN"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password", "code": "INTERNAL_ERROR"})
		return
	}

	user := models.User{
		Name:         req.Name,
		Email:        req.Email,
		PasswordHash: string(hash),
		Role:         req.Role,
		ShowEmail:    true,
		ShowPhone:    true,
		ShowListings: true,
	}

	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user", "code": "INTERNAL_ERROR"})
		return
	}

	accessToken, refreshToken, expiresAt, err := h.generateTokens(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tokens", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"data": authResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    expiresAt,
			User:         &user,
		},
		"message": "account created successfully",
	})
}

// POST /api/v1/auth/signin
func (h *AuthHandler) Signin(c *gin.Context) {
	var req signinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	var user models.User
	if err := h.db.Where("email = ?", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password", "code": "INVALID_CREDENTIALS"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password", "code": "INVALID_CREDENTIALS"})
		return
	}

	accessToken, refreshToken, expiresAt, err := h.generateTokens(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tokens", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": authResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    expiresAt,
			User:         &user,
		},
		"message": "signed in successfully",
	})
}

// POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	token, err := jwt.Parse(req.RefreshToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(h.cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired refresh token", "code": "UNAUTHORIZED"})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims", "code": "UNAUTHORIZED"})
		return
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not a refresh token", "code": "UNAUTHORIZED"})
		return
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token subject", "code": "UNAUTHORIZED"})
		return
	}

	accessToken, newRefreshToken, expiresAt, err := h.generateTokens(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tokens", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": authResponse{
			AccessToken:  accessToken,
			RefreshToken: newRefreshToken,
			ExpiresAt:    expiresAt,
		},
		"message": "tokens refreshed successfully",
	})
}

// POST /api/v1/auth/signout
func (h *AuthHandler) Signout(c *gin.Context) {
	// Stateless JWT — no server-side token invalidation needed.
	// Client should discard tokens.
	c.Status(http.StatusNoContent)
}
