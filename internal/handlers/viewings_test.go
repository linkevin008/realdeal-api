package handlers_test

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
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

func setupViewingRouter(h *handlers.ViewingHandler, callerID string) *gin.Engine {
	r := gin.New()
	injectCaller := func(c *gin.Context) {
		if callerID != "" {
			c.Set("userID", callerID)
		}
		c.Next()
	}
	r.POST("/properties/:id/viewing-slots", injectCaller, h.CreateSlot)
	r.GET("/properties/:id/viewing-slots", h.ListSlots)
	r.DELETE("/properties/:id/viewing-slots/:slotId", injectCaller, h.DeleteSlot)
	r.POST("/properties/:id/viewing-slots/:slotId/requests", injectCaller, h.RequestViewing)
	r.GET("/properties/:id/viewing-requests", injectCaller, h.ListRequests)
	r.PUT("/properties/:id/viewing-requests/:requestId/accept", injectCaller, h.AcceptRequest)
	r.PUT("/properties/:id/viewing-requests/:requestId/decline", injectCaller, h.DeclineRequest)
	r.DELETE("/viewing-requests/:requestId", injectCaller, h.CancelRequest)
	r.GET("/users/me/viewing-requests", injectCaller, h.ListMyRequests)
	return r
}

func slotColumns() []string {
	return []string{"id", "property_id", "start_time", "end_time", "created_at", "updated_at"}
}

func slotRow(id, propertyID string, start, end time.Time) []driver.Value {
	now := time.Now()
	return []driver.Value{id, propertyID, start, end, now, now}
}

func viewingRequestColumns() []string {
	return []string{"id", "slot_id", "property_id", "buyer_id", "message", "status", "created_at", "updated_at"}
}

func viewingRequestRow(id, slotID, propertyID, buyerID, status string) []driver.Value {
	now := time.Now()
	return []driver.Value{id, slotID, propertyID, buyerID, "", status, now, now}
}

// CreateSlot

func TestCreateSlot_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("slot-1"))
	mock.ExpectCommit()

	body, _ := json.Marshal(map[string]interface{}{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestCreateSlot_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	body, _ := json.Marshal(map[string]interface{}{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestCreateSlot_Overlap(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	body, _ := json.Marshal(map[string]interface{}{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateSlot_EndBeforeStart(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(-1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	body, _ := json.Marshal(map[string]interface{}{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSlot_PropertyNotActive(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	row := propertyRow("prop-1", "seller-1")
	row[18] = "pending"
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(row...))

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)
	body, _ := json.Marshal(map[string]interface{}{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateSlot_StartTimeInPast(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	start := time.Now().Add(-1 * time.Hour)
	end := start.Add(1 * time.Hour)
	body, _ := json.Marshal(map[string]interface{}{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSlot_DBErrorOnOverlapCheck(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_slots"`).
		WillReturnError(fmt.Errorf("db error"))

	body, _ := json.Marshal(map[string]interface{}{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateSlot_InvalidJSON(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSlot_DBErrorOnCreate(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "viewing_slots"`).
		WillReturnError(fmt.Errorf("db error"))
	mock.ExpectRollback()

	body, _ := json.Marshal(map[string]interface{}{
		"start_time": start.Format(time.RFC3339),
		"end_time":   end.Format(time.RFC3339),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// RequestViewing

func TestRequestViewing_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests" WHERE slot_id = \$1 AND status = \$2`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests" WHERE slot_id = \$1 AND buyer_id = \$2 AND status = \$3`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("req-1"))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "pending")...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))

	body, _ := json.Marshal(map[string]interface{}{"message": "Looking forward to it"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRequestViewing_OwnPropertyForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	body, _ := json.Marshal(map[string]interface{}{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequestViewing_AlreadyBooked(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	body, _ := json.Marshal(map[string]interface{}{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestRequestViewing_PropertyNotActive(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	row := propertyRow("prop-1", "seller-1")
	row[18] = "pending"
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(row...))

	body, _ := json.Marshal(map[string]interface{}{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// AcceptRequest

func TestAcceptRequest_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "viewing_requests" SET "status"=\$1,"updated_at"=\$2 WHERE "id" = \$3`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "viewing_requests" SET "status"=\$1,"updated_at"=\$2 WHERE slot_id = \$3 AND id != \$4 AND status = \$5`).
		WillReturnResult(sqlmock.NewResult(1, 2))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "accepted")...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "accepted", resp["data"].(map[string]interface{})["status"])
}

func TestAcceptRequest_NotPending(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "accepted")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAcceptRequest_ConcurrentAcceptRace(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	// Both requests pass the friendly pre-check (still "pending" as read),
	// but a competing accept commits first; the partial unique index on
	// viewing_requests(slot_id) WHERE status='accepted' rejects the second
	// accept's UPDATE, which must map to the same 409 the pre-check gives.
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-2", "slot-1", "prop-1", "buyer-2", "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "viewing_requests" SET "status"=\$1,"updated_at"=\$2 WHERE "id" = \$3`).
		WillReturnError(errors.New(`ERROR: duplicate key value violates unique constraint "idx_viewing_requests_one_accepted_per_slot"`))
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-2/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "SLOT_BOOKED", resp["code"])
	assert.Equal(t, "viewing slot is already booked", resp["error"])
}

func TestAcceptRequest_AfterCancelledAcceptFreesSlot(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	// A prior acceptance on this slot was cancelled, so its row no longer has
	// status='accepted' and falls outside the partial index — a second,
	// distinct pending request on the same slot must still be acceptable.
	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-2", "slot-1", "prop-1", "buyer-2", "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "viewing_requests" SET "status"=\$1,"updated_at"=\$2 WHERE "id" = \$3`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "viewing_requests" SET "status"=\$1,"updated_at"=\$2 WHERE slot_id = \$3 AND id != \$4 AND status = \$5`).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-2", "slot-1", "prop-1", "buyer-2", "accepted")...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-2/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "accepted", resp["data"].(map[string]interface{})["status"])
}

func TestAcceptRequest_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// DeclineRequest

func TestDeclineRequest_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "viewing_requests"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "declined")...))
	mock.ExpectQuery(`SELECT .* FROM "users"`).WillReturnRows(buyerRow())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/decline", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDeclineRequest_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/decline", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// CancelRequest

func TestCancelRequest_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "pending")...))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "viewing_requests"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/viewing-requests/req-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestCancelRequest_WrongBuyerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "other-buyer")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "pending")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/viewing-requests/req-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestCancelRequest_NotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/viewing-requests/req-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeclineRequest_NotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/decline", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeclineRequest_NotSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "other-seller")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/decline", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequestViewing_AlreadyRequestedByBuyer(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests" WHERE slot_id = \$1 AND status = \$2`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests" WHERE slot_id = \$1 AND buyer_id = \$2 AND status = \$3`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	body, _ := json.Marshal(map[string]interface{}{"message": "Already requested"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestDeleteSlot_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "other-seller")

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/viewing-slots/slot-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// DeleteSlot

func TestDeleteSlot_AcceptedRequestConflict(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/viewing-slots/slot-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestDeleteSlot_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "viewing_requests"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM "viewing_slots"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/viewing-slots/slot-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteSlot_DBErrorOnAcceptedCheck(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties"`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots"`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests"`).
		WillReturnError(fmt.Errorf("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/viewing-slots/slot-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ListSlots

func TestListSlots_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*property_id.*\$1`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests".*WHERE.*slot_id.*status`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/viewing-slots", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListSlots_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/viewing-slots", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ListRequests

func TestListRequests_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests" WHERE.*property_id.*\$1`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "pending")...))
	mock.ExpectQuery(`SELECT .* FROM "users" WHERE.*id.*\$1`).
		WillReturnRows(buyerRow())
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", time.Now().Add(24*time.Hour), time.Now().Add(25*time.Hour))...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/viewing-requests", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListRequests_NonSellerForbidden(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/viewing-requests", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// ListMyRequests

func TestListMyRequests_Success(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests" WHERE.*buyer_id.*\$1`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "pending")...))
	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "property_images" WHERE.*property_id.*\$1`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "property_id", "url", "created_at"}).AddRow("img-1", "prop-1", "https://example.com/image.jpg", time.Now()))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", time.Now().Add(24*time.Hour), time.Now().Add(25*time.Hour))...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/viewing-requests", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListMyRequests_Empty(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests" WHERE.*buyer_id.*\$1`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/viewing-requests", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListSlots_DBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*property_id.*\$1`).
		WillReturnError(fmt.Errorf("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/viewing-slots", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListRequests_DBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests" WHERE.*property_id.*\$1`).
		WillReturnError(fmt.Errorf("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/properties/prop-1/viewing-requests", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListMyRequests_DBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests" WHERE.*buyer_id.*\$1`).
		WillReturnError(fmt.Errorf("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/users/me/viewing-requests", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeleteSlot_NotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(slotColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/viewing-slots/slot-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteSlot_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/properties/prop-1/viewing-slots/slot-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRequestViewing_SlotNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(slotColumns()))

	body, _ := json.Marshal(map[string]interface{}{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRequestViewing_SlotInPast(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	start := time.Now().Add(-1 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))

	body, _ := json.Marshal(map[string]interface{}{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestRequestViewing_SlotBooked(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests".*WHERE.*slot_id.*status`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	body, _ := json.Marshal(map[string]interface{}{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestRequestViewing_DBErrorOnAcceptedCount(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	start := time.Now().Add(24 * time.Hour)
	end := start.Add(1 * time.Hour)

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_slots" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(slotColumns()).AddRow(slotRow("slot-1", "prop-1", start, end)...))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "viewing_requests".*WHERE.*slot_id.*status`).
		WillReturnError(fmt.Errorf("db error"))

	body, _ := json.Marshal(map[string]interface{}{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/properties/prop-1/viewing-slots/slot-1/requests", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAcceptRequest_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAcceptRequest_RequestNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()).AddRow(propertyRow("prop-1", "seller-1")...))
	mock.ExpectQuery(`SELECT .* FROM "viewing_requests" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/accept", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeclineRequest_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "seller-1")

	mock.ExpectQuery(`SELECT .* FROM "properties" WHERE.*id.*\$1`).
		WillReturnRows(sqlmock.NewRows(propertyColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/properties/prop-1/viewing-requests/req-1/decline", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCancelRequest_PropertyNotFound(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/viewing-requests/req-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCancelRequest_DeclinedStatus(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "declined")...))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/viewing-requests/req-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCancelRequest_DBError(t *testing.T) {
	t.Parallel()
	gormDB, mock := newTestDB(t)
	h := handlers.NewViewingHandler(gormDB)
	r := setupViewingRouter(h, "buyer-1")

	mock.ExpectQuery(`SELECT .* FROM "viewing_requests"`).
		WillReturnRows(sqlmock.NewRows(viewingRequestColumns()).AddRow(viewingRequestRow("req-1", "slot-1", "prop-1", "buyer-1", "pending")...))
	mock.ExpectExec(`UPDATE "viewing_requests"`).WillReturnError(fmt.Errorf("db error"))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/viewing-requests/req-1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
