package handlers_test

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/stretchr/testify/assert"
)

func setupTrustRouter(h *handlers.TrustHandler, callerID string) *gin.Engine {
	r := gin.New()
	injectCaller := func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		c.Next()
	}
	r.POST("/properties/:id/offers/:offerId/report-nonpayment", injectCaller, h.ReportNonPayment)
	r.POST("/properties/:id/offers/:offerId/report-seller", injectCaller, h.ReportSeller)
	r.POST("/users/me/trust-appeal", injectCaller, h.FileAppeal)
	return r
}

func trustEventColumns() []string {
	return []string{"id", "user_id", "event_type", "status", "severity", "offer_id", "property_id", "reported_by", "notes", "created_at"}
}

func trustEventRow(id, userID string, status string, createdAt time.Time) []driver.Value {
	return []driver.Value{id, userID, "offer_default", status, 100, nil, nil, nil, "", createdAt}
}

func trustAppealRequestBody(statement string) []byte {
	b, _ := json.Marshal(map[string]interface{}{"statement": statement})
	return b
}

// ReportNonPayment

func TestReportNonPayment_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "seller-1")

	pastDeadline := time.Now().Add(-time.Hour)

	row := propertyRow("prop-1", "seller-1")
	row[18] = "pending" // property went pending on acceptance
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(row...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRowWithDeadline("offer-1", "prop-1", "buyer-1", 300000, "accepted", pastDeadline)...))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "offers"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`INSERT INTO "trust_events"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("event-1"))
	mock.ExpectExec(`UPDATE "properties"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRowWithDeadline("offer-1", "prop-1", "buyer-1", 300000, "defaulted", pastDeadline)...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "defaulted", data["status"])
	// Trust event must never be echoed back.
	assert.NotContains(t, resp, "event")
	assert.NotContains(t, w.Body.String(), "trust")
}

func TestReportNonPayment_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestReportNonPayment_DeadlineNotPassed(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "seller-1")

	futureDeadline := time.Now().Add(time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRowWithDeadline("offer-1", "prop-1", "buyer-1", 300000, "accepted", futureDeadline)...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReportNonPayment_NilDeadline(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...)) // payment_deadline nil

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReportNonPayment_OfferNotAccepted(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReportNonPayment_Duplicate(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "seller-1")

	pastDeadline := time.Now().Add(-time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRowWithDeadline("offer-1", "prop-1", "buyer-1", 300000, "accepted", pastDeadline)...))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "offers"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`INSERT INTO "trust_events"`).
		WillReturnError(errors.New("ERROR: duplicate key value violates unique constraint"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReportNonPayment_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnError(errors.New("record not found"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestReportNonPayment_OfferNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnError(errors.New("record not found"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestReportNonPayment_TransactionDBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "seller-1")

	pastDeadline := time.Now().Add(-time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRowWithDeadline("offer-1", "prop-1", "buyer-1", 300000, "accepted", pastDeadline)...))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "offers"`).WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-nonpayment", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ReportSeller

func TestReportSeller_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "trust_events"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("event-1"))
	mock.ExpectCommit()

	body, _ := json.Marshal(map[string]interface{}{"violation": "deed_default", "notes": "seller never delivered the deed"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestReportSeller_WrongBuyerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "other-buyer")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))

	body, _ := json.Marshal(map[string]interface{}{"violation": "deed_default"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestReportSeller_InvalidViolationType(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))

	body, _ := json.Marshal(map[string]interface{}{"violation": "not_a_real_violation"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReportSeller_OfferNotAccepted(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "pending")...))

	body, _ := json.Marshal(map[string]interface{}{"violation": "document_fraud"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReportSeller_Duplicate(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "trust_events"`).
		WillReturnError(errors.New("ERROR: duplicate key value violates unique constraint"))
	mock.ExpectRollback()

	body, _ := json.Marshal(map[string]interface{}{"violation": "document_fraud"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReportSeller_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnError(errors.New("record not found"))

	body, _ := json.Marshal(map[string]interface{}{"violation": "deed_default"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestReportSeller_OfferNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnError(errors.New("record not found"))

	body, _ := json.Marshal(map[string]interface{}{"violation": "deed_default"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestReportSeller_NotesTooLong(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))

	longNotes := strings.Repeat("a", 2001)
	body, _ := json.Marshal(map[string]interface{}{"violation": "deed_default", "notes": longNotes})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReportSeller_NoSellerOnRecord(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	row := propertyRow("prop-1", "seller-1")
	row[17] = nil // seller_id nil: property has no seller on record
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(row...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))

	body, _ := json.Marshal(map[string]interface{}{"violation": "deed_default"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "NO_SELLER")
}

func TestReportSeller_CreateDBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRow("offer-1", "prop-1", "buyer-1", 300000, "accepted")...))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "trust_events"`).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	body, _ := json.Marshal(map[string]interface{}{"violation": "document_fraud"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/report-seller", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Enforcement

func TestSubmitOffer_BlockedByConfirmedTrustEvent(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	expectConfirmedTrustEvent(mock)

	body, _ := json.Marshal(map[string]interface{}{"amount": 300000})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// Message must stay neutral — no hint of trust/flag mechanics.
	assert.Equal(t, "offers cannot be submitted from this account", resp["error"])
	assert.NotContains(t, w.Body.String(), "trust")
	assert.NotContains(t, w.Body.String(), "flag")
}

func TestCreateProperty_BlockedByConfirmedTrustEvent(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewPropertyHandler(gormDB)
	r := setupPropertyRouter(h, "seller-1")

	expectConfirmedTrustEvent(mock)

	body, _ := json.Marshal(map[string]interface{}{
		"street": "123 Main St", "city": "Springfield", "state": "IL",
		"postal_code": "62701", "country": "US", "price": 250000, "property_type": "house",
		"bedrooms": 3, "bathrooms": 2.0, "square_feet": 1800, "year_built": 1998,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "listings cannot be created from this account", resp["error"])
}

func TestAcceptOffer_BlockedByConfirmedTrustEvent(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	expectConfirmedTrustEvent(mock)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "offers cannot be accepted from this account", resp["error"])
}

func TestSubmitOffer_PendingReviewDoesNotBlock(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewOfferHandler(gormDB, 72)
	r := setupOfferRouter(h, "buyer-1")

	sellerID := "seller-1"
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", sellerID)...))
	// A pending_review (or dismissed) event exists but must not enforce —
	// hasConfirmedTrustEvent only counts status = confirmed, so the count
	// query legitimately returns 0 here.
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
}

func TestAcceptOffer_StampsPaymentDeadline(t *testing.T) {
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

	deadline := time.Now().Add(72 * time.Hour)
	mock.ExpectQuery(`SELECT .* FROM "offers"`).
		WillReturnRows(sqlmock.NewRows(offerColumns()).
			AddRow(offerRowWithDeadline("offer-1", "prop-1", "buyer-1", 300000, "accepted", deadline)...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, data["payment_deadline"])
}

// FileAppeal

func TestFileAppeal_MissingStatement(t *testing.T) {
	t.Parallel()
	gormDB, _ := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "trust")
}

func TestFileAppeal_BlankStatement(t *testing.T) {
	t.Parallel()
	gormDB, _ := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	body := trustAppealRequestBody("   ")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFileAppeal_StatementTooLong(t *testing.T) {
	t.Parallel()
	gormDB, _ := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	longStatement := strings.Repeat("a", 2001)
	body := trustAppealRequestBody(longStatement)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestFileAppeal_AlreadyPending(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	body := trustAppealRequestBody("please reconsider")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "APPEAL_PENDING")
}

func TestFileAppeal_PendingCheckDBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnError(errors.New("connection reset"))

	body := trustAppealRequestBody("please reconsider")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// NoConfirmedEvents is the minimal-disclosure case: a caller with zero
// confirmed trust events gets the exact same 409 as a caller who has
// already appealed every confirmed event (see noAppealAvailableResponse).
func TestFileAppeal_NoConfirmedEvents(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "trust_events"`).
		WillReturnRows(sqlmock.NewRows(trustEventColumns()))

	body := trustAppealRequestBody("please reconsider")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "NO_APPEAL_AVAILABLE")
	assert.NotContains(t, w.Body.String(), "trust_event")
}

func TestFileAppeal_ConfirmedEventsQueryDBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "trust_events"`).
		WillReturnError(errors.New("connection reset"))

	body := trustAppealRequestBody("please reconsider")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFileAppeal_AllConfirmedEventsAlreadyAppealed(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	now := time.Now()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "trust_events"`).
		WillReturnRows(sqlmock.NewRows(trustEventColumns()).
			AddRow(trustEventRow("event-1", "user-1", "confirmed", now)...))
	mock.ExpectQuery(`SELECT "trust_event_id" FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"trust_event_id"}).AddRow("event-1"))

	body := trustAppealRequestBody("please reconsider")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "NO_APPEAL_AVAILABLE")
}

func TestFileAppeal_AppealedEventsQueryDBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	now := time.Now()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "trust_events"`).
		WillReturnRows(sqlmock.NewRows(trustEventColumns()).
			AddRow(trustEventRow("event-1", "user-1", "confirmed", now)...))
	mock.ExpectQuery(`SELECT "trust_event_id" FROM "trust_appeals"`).
		WillReturnError(errors.New("connection reset"))

	body := trustAppealRequestBody("please reconsider")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestFileAppeal_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	now := time.Now()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "trust_events"`).
		WillReturnRows(sqlmock.NewRows(trustEventColumns()).
			AddRow(trustEventRow("event-1", "user-1", "confirmed", now)...))
	mock.ExpectQuery(`SELECT "trust_event_id" FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"trust_event_id"}))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("appeal-1"))
	mock.ExpectCommit()

	body := trustAppealRequestBody("please reconsider, I paid on time")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "pending", data["status"])
	// Never disclose event details.
	assert.NotContains(t, w.Body.String(), "event-1")
	assert.NotContains(t, w.Body.String(), "trust_event")
}

func TestFileAppeal_CreateRaceLostToUniqueViolation(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	now := time.Now()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "trust_events"`).
		WillReturnRows(sqlmock.NewRows(trustEventColumns()).
			AddRow(trustEventRow("event-1", "user-1", "confirmed", now)...))
	mock.ExpectQuery(`SELECT "trust_event_id" FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"trust_event_id"}))
	mock.ExpectQuery(`INSERT INTO "trust_appeals"`).
		WillReturnError(errors.New("ERROR: duplicate key value violates unique constraint"))

	body := trustAppealRequestBody("please reconsider")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "NO_APPEAL_AVAILABLE")
}

func TestFileAppeal_CreateDBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewTrustHandler(gormDB)
	r := setupTrustRouter(h, "user-1")

	now := time.Now()
	mock.ExpectQuery(`SELECT count\(\*\) FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT \* FROM "trust_events"`).
		WillReturnRows(sqlmock.NewRows(trustEventColumns()).
			AddRow(trustEventRow("event-1", "user-1", "confirmed", now)...))
	mock.ExpectQuery(`SELECT "trust_event_id" FROM "trust_appeals"`).
		WillReturnRows(sqlmock.NewRows([]string{"trust_event_id"}))
	mock.ExpectQuery(`INSERT INTO "trust_appeals"`).
		WillReturnError(errors.New("connection reset"))

	body := trustAppealRequestBody("please reconsider")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/users/me/trust-appeal", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
