package handlers_test

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
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

func setupOfferRouter(h *handlers.OfferHandler, callerID string) *gin.Engine {
	r := gin.New()
	injectCaller := func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		c.Next()
	}
	r.POST("/properties/:id/offers", injectCaller, h.SubmitOffer)
	r.GET("/properties/:id/offers", injectCaller, h.ListOffers)
	r.PUT("/properties/:id/offers/:offerId/accept", injectCaller, h.AcceptOffer)
	r.PUT("/properties/:id/offers/:offerId/reject", injectCaller, h.RejectOffer)
	r.DELETE("/properties/:id/offers/:offerId", injectCaller, h.WithdrawOffer)
	r.GET("/users/me/offers", injectCaller, h.ListMyOffers)
	return r
}

func offerColumns() []string {
	return []string{"id", "property_id", "buyer_id", "amount", "message", "status", "payment_deadline", "created_at", "updated_at"}
}

func offerRow(id, propertyID, buyerID string, amount float64, status string) []driver.Value {
	now := time.Now()
	return []driver.Value{id, propertyID, buyerID, amount, "", status, nil, now, now}
}

// offerRowWithDeadline is offerRow with an explicit payment_deadline, for
// trust-enforcement tests that need to control whether the deadline has passed.
func offerRowWithDeadline(id, propertyID, buyerID string, amount float64, status string, deadline time.Time) []driver.Value {
	now := time.Now()
	return []driver.Value{id, propertyID, buyerID, amount, "", status, deadline, now, now}
}

// expectNoConfirmedTrustEvent stubs the hasConfirmedTrustEvent count query
// used by SubmitOffer/AcceptOffer/CreateProperty enforcement checks, returning
// a zero count (account not flagged).
func expectNoConfirmedTrustEvent(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_events"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}

// expectConfirmedTrustEvent stubs the same query returning a nonzero count
// (account flagged).
func expectConfirmedTrustEvent(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_events"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
}

func buyerRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "email", "password_hash", "phone_number", "profile_photo_url", "role", "show_email", "show_phone", "show_listings", "created_at", "updated_at"}).
		AddRow("buyer-1", "Buyer", "buyer@example.com", "hash", nil, nil, "buyer", true, true, true, time.Now(), time.Now())
}

// SubmitOffer

func TestSubmitOffer_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	sellerID := "seller-1"
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", sellerID)...))
	expectNoConfirmedTrustEvent(mock)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "offers"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("offer-1"))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())

	body, _ := json.Marshal(map[string]interface{}{"amount": 300000})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "pending", data["status"])
}

func TestSubmitOffer_SellerCannotOffer(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	body, _ := json.Marshal(map[string]interface{}{"amount": 300000})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSubmitOffer_TrustCheckDBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_events"`).
		WillReturnError(errors.New("connection reset"))

	body, _ := json.Marshal(map[string]interface{}{"amount": 300000})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSubmitOffer_PropertyNotActive(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	row := propertyRow("prop-1", "seller-1")
	row[18] = "pending" // override status
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(row...))

	body, _ := json.Marshal(map[string]interface{}{"amount": 300000})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestSubmitOffer_InvalidAmount(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	expectNoConfirmedTrustEvent(mock)

	body, _ := json.Marshal(map[string]interface{}{"amount": -100})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ListOffers

func TestListOffers_SellerSuccess(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/offers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListOffers_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "other-user")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/offers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// AcceptOffer

func TestAcceptOffer_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	expectNoConfirmedTrustEvent(mock)
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "offers"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "offers"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "properties"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "accepted", resp["data"].(map[string]interface{})["status"])
}

func TestAcceptOffer_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAcceptOffer_AlreadyAccepted(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	expectNoConfirmedTrustEvent(mock)
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// WithdrawOffer

func TestWithdrawOffer_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "offers"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/offers/offer-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestWithdrawOffer_WrongBuyer(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "other-buyer")

	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/offers/offer-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// RejectOffer

func TestRejectOffer_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "offers"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "rejected")...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/reject", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "rejected", resp["data"].(map[string]interface{})["status"])
}

func TestRejectOffer_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/reject", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRejectOffer_AlreadyRejected(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "rejected")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/reject", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestSubmitOffer_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	body, _ := json.Marshal(map[string]interface{}{"amount": 300000})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/nonexistent/offers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAcceptOffer_PropertyNotActive(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	row := propertyRow("prop-1", "seller-1")
	row[18] = "pending"
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(row...))
	expectNoConfirmedTrustEvent(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestWithdrawOffer_AlreadyWithdrawn(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "withdrawn")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/offers/offer-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// ListMyOffers

func TestListOffers_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/nonexistent/offers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAcceptOffer_OfferNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	expectNoConfirmedTrustEvent(mock)
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/nonexistent/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRejectOffer_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/nonexistent/offers/offer-1/reject", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRejectOffer_OfferNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/nonexistent/reject", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWithdrawOffer_NotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/offers/nonexistent", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListMyOffers_DBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "offers"`).WillReturnError(errors.New("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/offers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAcceptOffer_TransactionError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	expectNoConfirmedTrustEvent(mock)
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "offers"`).WillReturnError(errors.New("db error"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListMyOffers_Empty(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/offers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp["data"])
}

func TestListMyOffers_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "property_images"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "property_id", "url", "order", "created_at"}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/offers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]interface{})
	assert.Len(t, data, 1)
}
