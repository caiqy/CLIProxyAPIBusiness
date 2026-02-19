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

func TestFrontDashboardTransactionsReturnsVariantFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn, errOpen := dbpkg.Open(":memory:")
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := dbpkg.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}

	now := time.Now().UTC()
	user := models.User{Username: "front-u", Password: "pwd", CreatedAt: now, UpdatedAt: now}
	if errCreate := conn.Create(&user).Error; errCreate != nil {
		t.Fatalf("create user: %v", errCreate)
	}
	apiKey := models.APIKey{Name: "k1", APIKey: "k-front-1", UserID: &user.ID, Active: true, CreatedAt: now, UpdatedAt: now}
	if errCreate := conn.Create(&apiKey).Error; errCreate != nil {
		t.Fatalf("create api key: %v", errCreate)
	}
	usage := models.Usage{
		Provider:      "openai",
		Model:         "gpt-5.2",
		VariantOrigin: "xhigh",
		Variant:       "high",
		APIKeyID:      &apiKey.ID,
		UserID:        &user.ID,
		RequestedAt:   now,
		CreatedAt:     now.Add(1500 * time.Millisecond),
	}
	if errCreate := conn.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	h := NewDashboardHandler(conn)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("userID", user.ID)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/front/dashboard/transactions?page=1&page_size=10", nil)

	h.RecentTransactions(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Transactions []struct {
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
	if resp.Transactions[0].VariantOrigin != "xhigh" || resp.Transactions[0].Variant != "high" {
		t.Fatalf("expected variant_origin=xhigh and variant=high, got %q => %q", resp.Transactions[0].VariantOrigin, resp.Transactions[0].Variant)
	}
}
