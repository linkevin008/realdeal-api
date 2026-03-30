package handlers

import (
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/kevinlin/realdeal-api/internal/config"
	"github.com/kevinlin/realdeal-api/internal/models"
	"gorm.io/gorm"
)

const (
	appleJWKSURL  = "https://appleid.apple.com/auth/keys"
	appleIssuer   = "https://appleid.apple.com"
	googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"
	jwksCacheTTL  = time.Hour
)

// jwksCache holds RSA public keys keyed by kid with a TTL.
type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func (c *jwksCache) get(kid string) (*rsa.PublicKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Since(c.fetchedAt) > jwksCacheTTL {
		return nil, false
	}
	k, ok := c.keys[kid]
	return k, ok
}

func (c *jwksCache) set(keys map[string]*rsa.PublicKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = keys
	c.fetchedAt = time.Now()
}

type jwksKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

func fetchJWKS(url string) (map[string]*rsa.PublicKey, error) {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := jwkToRSA(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

func jwkToRSA(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}

// SocialAuthHandler handles Apple and Google Sign In.
// AppleJWKSURL and GoogleJWKSURL can be overridden in tests.
type SocialAuthHandler struct {
	db            *gorm.DB
	cfg           *config.Config
	appleCache    jwksCache
	googleCache   jwksCache
	AppleJWKSURL  string
	GoogleJWKSURL string
}

func NewSocialAuthHandler(db *gorm.DB, cfg *config.Config) *SocialAuthHandler {
	return &SocialAuthHandler{
		db:            db,
		cfg:           cfg,
		AppleJWKSURL:  appleJWKSURL,
		GoogleJWKSURL: googleJWKSURL,
	}
}

func (h *SocialAuthHandler) appleKey(kid string) (*rsa.PublicKey, error) {
	if k, ok := h.appleCache.get(kid); ok {
		return k, nil
	}
	keys, err := fetchJWKS(h.AppleJWKSURL)
	if err != nil {
		return nil, err
	}
	h.appleCache.set(keys)
	k, ok := keys[kid]
	if !ok {
		return nil, fmt.Errorf("kid %q not in Apple JWKS", kid)
	}
	return k, nil
}

func (h *SocialAuthHandler) googleKey(kid string) (*rsa.PublicKey, error) {
	if k, ok := h.googleCache.get(kid); ok {
		return k, nil
	}
	keys, err := fetchJWKS(h.GoogleJWKSURL)
	if err != nil {
		return nil, err
	}
	h.googleCache.set(keys)
	k, ok := keys[kid]
	if !ok {
		return nil, fmt.Errorf("kid %q not in Google JWKS", kid)
	}
	return k, nil
}

// POST /api/v1/auth/signin/apple
func (h *SocialAuthHandler) SignInWithApple(c *gin.Context) {
	var req struct {
		IdentityToken string  `json:"identity_token" binding:"required"`
		Nonce         string  `json:"nonce" binding:"required"`
		FullName      *string `json:"full_name"`
		Email         *string `json:"email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	token, err := jwt.Parse(req.IdentityToken, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		return h.appleKey(kid)
	}, jwt.WithValidMethods([]string{"RS256"}))

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid Apple identity token", "code": "UNAUTHORIZED"})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims", "code": "UNAUTHORIZED"})
		return
	}

	if iss, _ := claims["iss"].(string); iss != appleIssuer {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token issuer", "code": "UNAUTHORIZED"})
		return
	}

	// Verify nonce: Apple stores SHA256(rawNonce) in the token
	nonceClaim, _ := claims["nonce"].(string)
	if fmt.Sprintf("%x", sha256.Sum256([]byte(req.Nonce))) != nonceClaim {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "nonce mismatch", "code": "UNAUTHORIZED"})
		return
	}

	appleUserID, _ := claims["sub"].(string)
	if appleUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing subject in token", "code": "UNAUTHORIZED"})
		return
	}

	user, err := h.upsertSocialUser(appleUserID, "", req.Email, req.FullName)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "code": "EMAIL_REQUIRED"})
		return
	}

	h.respondWithTokens(c, user, "signed in with Apple")
}

// POST /api/v1/auth/signin/google
func (h *SocialAuthHandler) SignInWithGoogle(c *gin.Context) {
	var req struct {
		IDToken string `json:"id_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}

	token, err := jwt.Parse(req.IDToken, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		return h.googleKey(kid)
	}, jwt.WithValidMethods([]string{"RS256"}))

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid Google ID token", "code": "UNAUTHORIZED"})
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims", "code": "UNAUTHORIZED"})
		return
	}

	iss, _ := claims["iss"].(string)
	if iss != "accounts.google.com" && iss != "https://accounts.google.com" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token issuer", "code": "UNAUTHORIZED"})
		return
	}

	googleUserID, _ := claims["sub"].(string)
	if googleUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing subject in token", "code": "UNAUTHORIZED"})
		return
	}

	var email, name *string
	if v, _ := claims["email"].(string); v != "" {
		email = &v
	}
	if v, _ := claims["name"].(string); v != "" {
		name = &v
	}

	user, err := h.upsertSocialUser("", googleUserID, email, name)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "code": "EMAIL_REQUIRED"})
		return
	}

	h.respondWithTokens(c, user, "signed in with Google")
}

// upsertSocialUser finds or creates a user for a social sign-in.
// appleID or googleID (but not both) should be non-empty.
// email is only provided by Apple/Google on the first sign-in.
func (h *SocialAuthHandler) upsertSocialUser(appleID, googleID string, email, name *string) (*models.User, error) {
	var user models.User

	// Try to find by social ID
	if appleID != "" {
		if err := h.db.Where("apple_id = ?", appleID).First(&user).Error; err == nil {
			return &user, nil
		}
	} else if googleID != "" {
		if err := h.db.Where("google_id = ?", googleID).First(&user).Error; err == nil {
			return &user, nil
		}
	}

	// New social user — email required on first sign-in
	if email == nil || *email == "" {
		return nil, fmt.Errorf("email is required for first-time sign-in")
	}

	// If an account already exists with this email, link the social ID to it
	if err := h.db.Where("email = ?", *email).First(&user).Error; err == nil {
		updates := map[string]interface{}{}
		if appleID != "" {
			updates["apple_id"] = appleID
		} else {
			updates["google_id"] = googleID
		}
		h.db.Model(&user).Updates(updates)
		return &user, nil
	}

	// Create a brand new user
	displayName := *email
	if name != nil && *name != "" {
		displayName = *name
	}

	newUser := models.User{
		Name:         displayName,
		Email:        *email,
		PasswordHash: "-", // social-only; never used for password auth
		Role:         models.UserRoleBuyer,
		ShowEmail:    true,
		ShowPhone:    true,
		ShowListings: true,
	}
	if appleID != "" {
		newUser.AppleID = &appleID
	} else {
		newUser.GoogleID = &googleID
	}

	if err := h.db.Create(&newUser).Error; err != nil {
		return nil, err
	}
	return &newUser, nil
}

func (h *SocialAuthHandler) respondWithTokens(c *gin.Context, user *models.User, message string) {
	accessToken, refreshToken, expiresAt, err := generateTokens(h.cfg.JWTSecret, user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tokens", "code": "INTERNAL_ERROR"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": authResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    expiresAt,
			User:         user,
		},
		"message": message,
	})
}
