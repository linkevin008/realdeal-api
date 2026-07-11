package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"database/sql/driver"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/kevinlin/realdeal-api/internal/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupContractRouter(h *handlers.ContractHandler, callerID string) *gin.Engine {
	r := gin.New()
	injectCaller := func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		c.Next()
	}
	r.GET("/properties/:id/offers/:offerId/contract", injectCaller, h.GetContract)
	r.PUT("/properties/:id/offers/:offerId/contract/terms", injectCaller, h.ProposeTerms)
	r.POST("/properties/:id/offers/:offerId/contract/agree-terms", injectCaller, h.AgreeTerms)
	r.POST("/properties/:id/offers/:offerId/contract/sign", injectCaller, h.Sign)
	r.POST("/properties/:id/offers/:offerId/contract/cancel", injectCaller, h.Cancel)
	r.GET("/users/me/contracts", injectCaller, h.ListMyContracts)
	return r
}

func contractColumns() []string {
	return []string{
		"id", "offer_id", "property_id", "seller_id", "buyer_id", "status",
		"move_in_date", "transfer_date", "conditions", "terms_proposed_by",
		"buyer_agreed_at", "seller_agreed_at", "buyer_signed_at", "seller_signed_at",
		"execution_deadline", "created_at", "updated_at",
	}
}

type contractRowOpts struct {
	moveInDate      interface{}
	transferDate    interface{}
	conditions      string
	termsProposedBy interface{}
	buyerAgreedAt   interface{}
	sellerAgreedAt  interface{}
	buyerSignedAt   interface{}
	sellerSignedAt  interface{}
	deadline        time.Time
}

func contractRow(id, offerID, propertyID, sellerID, buyerID, status string, opts contractRowOpts) []driver.Value {
	now := time.Now()
	deadline := opts.deadline
	if deadline.IsZero() {
		deadline = now.Add(14 * 24 * time.Hour)
	}
	return []driver.Value{
		id, offerID, propertyID, sellerID, buyerID, status,
		opts.moveInDate, opts.transferDate, opts.conditions, opts.termsProposedBy,
		opts.buyerAgreedAt, opts.sellerAgreedAt, opts.buyerSignedAt, opts.sellerSignedAt,
		deadline, now, now,
	}
}

// draftContractRow is a freshly-created contract: no terms, no agreement, no
// signatures, deadline in the future.
func draftContractRow(id, offerID, propertyID, sellerID, buyerID string) []driver.Value {
	return contractRow(id, offerID, propertyID, sellerID, buyerID, "draft", contractRowOpts{})
}

// expiredDeadlineContractRow is non-terminal but with a deadline in the past
// — the handler should lazily expire it.
func expiredDeadlineContractRow(id, offerID, propertyID, sellerID, buyerID, status string, opts contractRowOpts) []driver.Value {
	opts.deadline = time.Now().Add(-1 * time.Hour)
	return contractRow(id, offerID, propertyID, sellerID, buyerID, status, opts)
}

// GetContract

func TestGetContract_PartySuccess(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/offers/offer-1/contract", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "draft", data["status"])
}

func TestGetContract_NonPartyForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "stranger")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/offers/offer-1/contract", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestGetContract_NotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/offers/offer-1/contract", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetContract_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/offers/offer-1/contract", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetContract_ExpiredOnRead(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(expiredDeadlineContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "draft", contractRowOpts{})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	// property currently pending (accepted-offer default) so the property row
	// is fetched inside the tx.
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(func() []driver.Value {
			row := propertyRow("prop-1", "seller-1")
			row[18] = "pending"
			return row
		}()...))
	mock.ExpectExec(`UPDATE "properties"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/offers/offer-1/contract", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "expired", data["status"])
	require.NoError(t, mock.ExpectationsWereMet())
}

// ProposeTerms

func validTermsBody() map[string]interface{} {
	moveIn := time.Now().Add(30 * 24 * time.Hour)
	transfer := time.Now().Add(45 * 24 * time.Hour)
	return map[string]interface{}{
		"move_in_date":  moveIn.Format(time.RFC3339),
		"transfer_date": transfer.Format(time.RFC3339),
		"conditions":    "Standard sale conditions apply.",
	}
}

func TestProposeTerms_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "draft", contractRowOpts{
				conditions: "Standard sale conditions apply.", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(),
			})...))

	body, _ := json.Marshal(validTermsBody())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.NotNil(t, data["buyer_agreed_at"])
	assert.Nil(t, data["seller_agreed_at"])
}

func TestProposeTerms_RepublishVoidsSignatures(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	// Contract fully signed by both parties (executed-adjacent state used
	// here as buyer_signed to keep it non-terminal so re-proposing is legal).
	existing := contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "buyer_signed", contractRowOpts{
		conditions: "Old conditions", termsProposedBy: "buyer-1",
		buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(), buyerSignedAt: time.Now(),
	})

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(existing...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "draft", contractRowOpts{
				conditions: "Standard sale conditions apply.", termsProposedBy: "seller-1", sellerAgreedAt: time.Now(),
			})...))

	body, _ := json.Marshal(validTermsBody())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "draft", data["status"])
	assert.Nil(t, data["buyer_signed_at"])
	assert.Nil(t, data["buyer_agreed_at"])
}

func TestProposeTerms_EmptyConditions(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	body, _ := json.Marshal(map[string]interface{}{"conditions": ""})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProposeTerms_ConditionsTooLong(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	oversized := make([]byte, 5001)
	for i := range oversized {
		oversized[i] = 'a'
	}
	body, _ := json.Marshal(map[string]interface{}{"conditions": string(oversized)})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProposeTerms_TransferBeforeMoveIn(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	moveIn := time.Now().Add(45 * 24 * time.Hour)
	transfer := time.Now().Add(30 * 24 * time.Hour)
	body, _ := json.Marshal(map[string]interface{}{
		"move_in_date":  moveIn.Format(time.RFC3339),
		"transfer_date": transfer.Format(time.RFC3339),
		"conditions":    "Conditions",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProposeTerms_PastDate(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	past := time.Now().Add(-24 * time.Hour)
	body, _ := json.Marshal(map[string]interface{}{
		"move_in_date": past.Format(time.RFC3339),
		"conditions":   "Conditions",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProposeTerms_TerminalContractConflict(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "cancelled", contractRowOpts{})...))

	body, _ := json.Marshal(validTermsBody())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// AgreeTerms

func TestAgreeTerms_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "draft", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(),
			})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "terms_agreed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
			})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/agree-terms", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "terms_agreed", data["status"])
}

func TestAgreeTerms_NoTermsProposed(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/agree-terms", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAgreeTerms_AlreadyAgreed(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "draft", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(),
			})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/agree-terms", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// Sign

func TestSign_FirstSignatureBuyer(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "terms_agreed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
			})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "buyer_signed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
				buyerSignedAt: time.Now(),
			})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "buyer_signed", resp["data"].(map[string]interface{})["status"])
}

func TestSign_SecondSignatureExecutes(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "buyer_signed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
				buyerSignedAt: time.Now(),
			})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "executed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
				buyerSignedAt: time.Now(), sellerSignedAt: time.Now(),
			})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "executed", resp["data"].(map[string]interface{})["status"])
}

func TestSign_BeforeTermsAgreedConflict(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestSign_DoubleSignConflict(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "buyer_signed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
				buyerSignedAt: time.Now(),
			})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestSign_ExpiredContractConflict(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(expiredDeadlineContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "terms_agreed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
			})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(func() []driver.Value {
			row := propertyRow("prop-1", "seller-1")
			row[18] = "pending"
			return row
		}()...))
	mock.ExpectExec(`UPDATE "properties"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "CONTRACT_EXPIRED", resp["code"])
}

// Cancel

func TestCancel_BuyerRevertsProperty(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(func() []driver.Value {
			row := propertyRow("prop-1", "seller-1")
			row[18] = "pending"
			return row
		}()...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(func() []driver.Value {
			row := propertyRow("prop-1", "seller-1")
			row[18] = "pending"
			return row
		}()...))
	mock.ExpectExec(`UPDATE "properties"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "cancelled", contractRowOpts{})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/cancel", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "cancelled", resp["data"].(map[string]interface{})["status"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCancel_SellerAllowed(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(func() []driver.Value {
			row := propertyRow("prop-1", "seller-1")
			row[18] = "pending"
			return row
		}()...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(func() []driver.Value {
			row := propertyRow("prop-1", "seller-1")
			row[18] = "pending"
			return row
		}()...))
	mock.ExpectExec(`UPDATE "properties"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "cancelled", contractRowOpts{})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/cancel", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCancel_AfterExecutedConflict(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "executed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
				buyerSignedAt: time.Now(), sellerSignedAt: time.Now(),
			})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/cancel", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// ListMyContracts

func TestListMyContracts_Buyer(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/contracts", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp["data"], 1)
}

func TestListMyContracts_Seller(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/contracts", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp["data"], 1)
}

func TestListMyContracts_DBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "contracts"`).WillReturnError(errors.New("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/contracts", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Property-not-found short-circuits every mutation endpoint the same way
// (via loadContractForParty) — one representative test per endpoint closes
// that shared branch for each handler.

func TestProposeTerms_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	body, _ := json.Marshal(validTermsBody())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAgreeTerms_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/agree-terms", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAgreeTerms_TerminalContractConflict(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "cancelled", contractRowOpts{})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/agree-terms", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAgreeTerms_TransactionError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "draft", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(),
			})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnError(errors.New("db error"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/agree-terms", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestProposeTerms_TransactionError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnError(errors.New("db error"))
	mock.ExpectRollback()

	body, _ := json.Marshal(validTermsBody())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestProposeTerms_TransferDatePast(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))

	past := time.Now().Add(-24 * time.Hour)
	body, _ := json.Marshal(map[string]interface{}{
		"transfer_date": past.Format(time.RFC3339),
		"conditions":    "Conditions",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/offers/offer-1/contract/terms", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSign_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSign_SellerDoubleSignConflict(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "seller_signed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
				sellerSignedAt: time.Now(),
			})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestSign_SellerFirstSignature(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "terms_agreed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
			})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "seller_signed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
				sellerSignedAt: time.Now(),
			})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "seller_signed", resp["data"].(map[string]interface{})["status"])
}

func TestSign_TransactionError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "terms_agreed", contractRowOpts{
				conditions: "Conditions", termsProposedBy: "buyer-1", buyerAgreedAt: time.Now(), sellerAgreedAt: time.Now(),
			})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnError(errors.New("db error"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/sign", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCancel_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/cancel", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCancel_TransactionError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnError(errors.New("db error"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/cancel", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCancel_NonPendingPropertyNotReverted(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	// Property already active (e.g. reverted by a separate flow) — cancel
	// should still succeed but skip the property UPDATE entirely.
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).AddRow(draftContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(contractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "cancelled", contractRowOpts{})...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/offers/offer-1/contract/cancel", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExpireContractIfPastDeadline_UpdateError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewContractHandler(gormDB)
	r := setupContractRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "contracts"`).
		WillReturnRows(sqlmock.NewRows(contractColumns()).
			AddRow(expiredDeadlineContractRow("contract-1", "offer-1", "prop-1", "seller-1", "buyer-1", "draft", contractRowOpts{})...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "contracts"`).WillReturnError(errors.New("db error"))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/offers/offer-1/contract", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
