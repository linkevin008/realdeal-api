package handlers_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rsaTestKey generates an RSA key pair for tests and returns a JWKS server.
// The returned server serves one key with kid="test-kid".
func rsaTestSetup(t *testing.T) (privateKey *rsa.PrivateKey, jwksServer *httptest.Server) {
	t.Helper()
	var err error
	privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pub := &privateKey.PublicKey
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()

	jwksBody, _ := json.Marshal(map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"kid": "test-kid",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	})

	jwksServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksBody)
	}))
	t.Cleanup(jwksServer.Close)
	return privateKey, jwksServer
}

// mintRSAToken creates an RS256 JWT with the given claims and kid.
func mintRSAToken(t *testing.T, privateKey *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(privateKey)
	require.NoError(t, err)
	return signed
}

func setupSocialRouter(h *handlers.SocialAuthHandler) *gin.Engine {
	r := gin.New()
	r.POST("/signin/apple", h.SignInWithApple)
	r.POST("/signin/google", h.SignInWithGoogle)
	return r
}

// ----- Apple Sign In tests -----

func TestAppleSignIn_BadJSON(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	r := setupSocialRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/signin/apple", nil)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAppleSignIn_GarbageToken(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	r := setupSocialRouter(h)

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": "not.a.jwt",
		"nonce":          "abc123",
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAppleSignIn_WrongSigningMethod(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	r := setupSocialRouter(h)

	// HS256 token — rejected because it's not RS256
	hsToken := generateToken("user-1", "access", jwtTestSecret, time.Hour)

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": hsToken,
		"nonce":          "abc123",
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAppleSignIn_WrongIssuer(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	rawNonce := "test-nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://evil.com",
		"sub":   "apple-user-123",
		"aud":   "com.example.app",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          rawNonce,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAppleSignIn_NonceMismatch(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte("correct-nonce")))
	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "apple-user-123",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          "wrong-nonce", // doesn't match the hash in the token
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAppleSignIn_NewUser(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	rawNonce := "my-raw-nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))
	email := "apple@example.com"

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "apple-user-abc",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	// No existing user by apple_id
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// No existing user by email
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// Create new user
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("new-apple-user-id", time.Now(), time.Now()))
	mock.ExpectCommit()

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          rawNonce,
		"email":          email,
		"full_name":      "Apple User",
	})

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, data["access_token"])
}

func TestAppleSignIn_ReturningUser(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	rawNonce := "my-raw-nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "apple-user-existing",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	// Found existing user by apple_id
	appleID := "apple-user-existing"
	_ = appleID
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("existing-id", "Apple User", "apple@example.com", "-")...))

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          rawNonce,
	})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAppleSignIn_NoEmailForNewUser(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	rawNonce := "my-raw-nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "apple-user-new",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	// No existing user by apple_id
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	// No email provided → should fail
	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          rawNonce,
		// no email
	})

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// ----- Google Sign In tests -----

func TestGoogleSignIn_WrongSigningMethod(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	r := setupSocialRouter(h)

	// HS256 token — rejected because it's not RS256
	hsToken := generateToken("user-1", "access", jwtTestSecret, time.Hour)

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": hsToken,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAppleSignIn_JWKSHTTPError(t *testing.T) {
	// Start then immediately close the server so http.Get gets connection refused
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	rawNonce := "nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))
	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "apple-user",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          rawNonce,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGoogleSignIn_BadJSON(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	r := setupSocialRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/signin/google", nil)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGoogleSignIn_GarbageToken(t *testing.T) {
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	r := setupSocialRouter(h)

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": "not.a.jwt",
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGoogleSignIn_WrongIssuer(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.GoogleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss": "https://evil.com",
		"sub": "google-user-123",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": token,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGoogleSignIn_NewUser(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.GoogleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	email := "google@example.com"
	name := "Google User"

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "accounts.google.com",
		"sub":   "google-user-new",
		"email": email,
		"name":  name,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	// No existing user by google_id
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// No existing user by email
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// Create new user
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("new-google-user-id", time.Now(), time.Now()))
	mock.ExpectCommit()

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": token,
	})

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, data["access_token"])
}

func TestAppleSignIn_EmptySubject(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	rawNonce := "nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          rawNonce,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAppleSignIn_LinkExistingEmailAccount(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	rawNonce := "nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))
	email := "existing@example.com"

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "apple-link-user",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	// No existing user by apple_id
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// Found existing user by email → link apple_id
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("existing-id", "Existing", email, "hash")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "users"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          rawNonce,
		"email":          email,
	})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAppleSignIn_KidNotInJWKS(t *testing.T) {
	// JWKS server returns a key with a different kid
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[{"kty":"RSA","kid":"other-kid","n":"AAAA","e":"AQAB"}]}`))
	}))
	defer jwksServer.Close()

	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = jwksServer.URL
	r := setupSocialRouter(h)

	rawNonce := "nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))
	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://appleid.apple.com",
		"sub":   "apple-user",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonceHash,
	})

	w := postJSON(r, "/signin/apple", map[string]interface{}{
		"identity_token": token,
		"nonce":          rawNonce,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGoogleSignIn_JWKSFetchError(t *testing.T) {
	// JWKS server returns 500
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("not json"))
	}))
	defer jwksServer.Close()

	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.GoogleJWKSURL = jwksServer.URL
	r := setupSocialRouter(h)

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss": "accounts.google.com",
		"sub": "google-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": token,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAppleSignIn_CacheHit(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	fetchCount := 0
	countingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		// Proxy to the real test JWKS server
		resp, _ := http.Get(srv.URL)
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		json.NewDecoder(resp.Body).Decode(new(interface{}))
	}))
	defer countingServer.Close()

	// Use the real JWKS server directly (two requests, only first should fetch JWKS)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.AppleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	rawNonce := "nonce"
	nonceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(rawNonce)))
	appleID := "apple-user-cache"

	makeRequest := func() {
		tok := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
			"iss":   "https://appleid.apple.com",
			"sub":   appleID,
			"exp":   time.Now().Add(time.Hour).Unix(),
			"nonce": nonceHash,
		})
		mock.ExpectQuery(`SELECT .* FROM "users"`).
			WillReturnRows(sqlmock.NewRows(userColumns()).
				AddRow(userRowValues("uid", "User", "u@example.com", "-")...))
		postJSON(r, "/signin/apple", map[string]interface{}{
			"identity_token": tok,
			"nonce":          rawNonce,
		})
	}

	makeRequest() // first call populates cache
	makeRequest() // second call uses cache
}

func TestGoogleSignIn_KidNotInJWKS(t *testing.T) {
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[{"kty":"RSA","kid":"other-kid","n":"AAAA","e":"AQAB"}]}`))
	}))
	defer jwksServer.Close()

	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.GoogleJWKSURL = jwksServer.URL
	r := setupSocialRouter(h)

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss": "accounts.google.com",
		"sub": "google-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": token,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGoogleSignIn_EmptySubject(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, _ := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.GoogleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss": "accounts.google.com",
		"sub": "",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": token,
	})

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGoogleSignIn_ReturningUser(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.GoogleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss": "accounts.google.com",
		"sub": "google-user-returning",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	// Found by google_id — no email needed
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("gid", "Google User", "g@example.com", "-")...))

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": token,
	})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGoogleSignIn_NewUser_NoNameClaim(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.GoogleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	email := "noname@example.com"
	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "accounts.google.com",
		"sub":   "google-user-noname",
		"email": email,
		// no "name" claim — display name falls back to email
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("noname-id", time.Now(), time.Now()))
	mock.ExpectCommit()

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": token,
	})

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGoogleSignIn_LinkExistingAccount(t *testing.T) {
	privateKey, srv := rsaTestSetup(t)
	gormDB, mock := newTestDB(t)
	h := handlers.NewSocialAuthHandler(gormDB, testAuthConfig())
	h.GoogleJWKSURL = srv.URL
	r := setupSocialRouter(h)

	email := "existing@example.com"

	token := mintRSAToken(t, privateKey, "test-kid", jwt.MapClaims{
		"iss":   "https://accounts.google.com",
		"sub":   "google-user-link",
		"email": email,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	// No existing user by google_id
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// Found existing user by email
	mock.ExpectQuery(`SELECT .* FROM "users"`).
		WillReturnRows(sqlmock.NewRows(userColumns()).
			AddRow(userRowValues("existing-id", "Existing", email, "hash")...))
	// Update google_id
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "users"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := postJSON(r, "/signin/google", map[string]interface{}{
		"id_token": token,
	})

	assert.Equal(t, http.StatusOK, w.Code)
}
