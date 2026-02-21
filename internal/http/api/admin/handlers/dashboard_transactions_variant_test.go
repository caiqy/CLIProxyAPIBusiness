package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dbpkg "github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
)

func TestAdminDashboardTransactionsReturnsVariantFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn, errOpen := dbpkg.Open(":memory:")
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := dbpkg.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}

	now := time.Now().UTC()
	user := models.User{Username: "admin-u", Password: "pwd", CreatedAt: now, UpdatedAt: now}
	if errCreate := conn.Create(&user).Error; errCreate != nil {
		t.Fatalf("create user: %v", errCreate)
	}
	usage := models.Usage{
		Provider:      "openai",
		Model:         "gpt-5.2",
		RequestID:     "  req-abc123  ",
		VariantOrigin: "xhigh",
		Variant:       "high",
		UserID:        &user.ID,
		RequestedAt:   now,
		CreatedAt:     now.Add(1200 * time.Millisecond),
	}
	if errCreate := conn.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	h := NewDashboardHandler(conn)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions?page=1&page_size=10", nil)

	h.RecentTransactions(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Transactions []struct {
			ID            uint64 `json:"id"`
			RequestID     string `json:"request_id"`
			VariantOrigin string `json:"variant_origin"`
			Variant       string `json:"variant"`
		} `json:"transactions"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if len(resp.Transactions) == 0 {
		t.Fatalf("expected at least 1 transaction")
	}
	if resp.Transactions[0].ID == 0 {
		t.Fatalf("expected id > 0, got %d", resp.Transactions[0].ID)
	}
	if resp.Transactions[0].RequestID != "req-abc123" {
		t.Fatalf("expected request_id=req-abc123, got %q", resp.Transactions[0].RequestID)
	}
	if resp.Transactions[0].VariantOrigin != "xhigh" || resp.Transactions[0].Variant != "high" {
		t.Fatalf("expected variant_origin=xhigh and variant=high, got %q => %q", resp.Transactions[0].VariantOrigin, resp.Transactions[0].Variant)
	}
}
