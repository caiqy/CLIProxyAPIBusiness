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
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestAdminDashboardTransactionsProviderDisplayUsesAuthNameFirst(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := openAdminDashboardProviderDisplayTestDB(t)

	now := time.Now().UTC()
	auth := models.Auth{Key: "codex-auth.json", Name: "业务主账号", Content: datatypes.JSON([]byte(`{"type":"codex"}`)), CreatedAt: now, UpdatedAt: now}
	if errCreate := conn.Create(&auth).Error; errCreate != nil {
		t.Fatalf("create auth: %v", errCreate)
	}
	usage := models.Usage{Provider: providerCodex, Model: "gpt-5", AuthID: &auth.ID, RequestedAt: now, CreatedAt: now}
	if errCreate := conn.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	transaction := fetchSingleAdminDashboardTransaction(t, conn)
	if transaction.Provider != "业务主账号" {
		t.Fatalf("expected provider from auth name, got %q", transaction.Provider)
	}
}

func TestAdminDashboardTransactionsProviderDisplayFallsBackToAuthKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := openAdminDashboardProviderDisplayTestDB(t)

	now := time.Now().UTC()
	auth := models.Auth{Key: "codex-auth.json", Name: "", Content: datatypes.JSON([]byte(`{"type":"codex"}`)), CreatedAt: now, UpdatedAt: now}
	if errCreate := conn.Create(&auth).Error; errCreate != nil {
		t.Fatalf("create auth: %v", errCreate)
	}
	usage := models.Usage{Provider: providerCodex, Model: "gpt-5", AuthID: &auth.ID, RequestedAt: now, CreatedAt: now}
	if errCreate := conn.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	transaction := fetchSingleAdminDashboardTransaction(t, conn)
	if transaction.Provider != "codex-auth.json" {
		t.Fatalf("expected provider from auth key, got %q", transaction.Provider)
	}
}

func TestAdminDashboardTransactionsProviderDisplayFallsBackToProviderAPIKeyName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := openAdminDashboardProviderDisplayTestDB(t)

	now := time.Now().UTC()
	providerAPIKey := models.ProviderAPIKey{
		Provider:  providerCodex,
		Name:      "codex-for1",
		APIKey:    "clp_test_provider_key",
		IsEnabled: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if errCreate := conn.Create(&providerAPIKey).Error; errCreate != nil {
		t.Fatalf("create provider api key: %v", errCreate)
	}
	usage := models.Usage{
		Provider:    providerCodex,
		Model:       "gpt-5",
		AuthIndex:   authIndexFromAPIKey(providerAPIKey.APIKey),
		RequestedAt: now,
		CreatedAt:   now,
	}
	if errCreate := conn.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	transaction := fetchSingleAdminDashboardTransaction(t, conn)
	if transaction.Provider != "codex-for1" {
		t.Fatalf("expected provider from provider api key name, got %q", transaction.Provider)
	}
}

func TestAdminDashboardTransactionsProviderDisplayFallsBackToUsageProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := openAdminDashboardProviderDisplayTestDB(t)

	now := time.Now().UTC()
	usage := models.Usage{Provider: providerCodex, Model: "gpt-5", RequestedAt: now, CreatedAt: now}
	if errCreate := conn.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	transaction := fetchSingleAdminDashboardTransaction(t, conn)
	if transaction.Provider != providerCodex {
		t.Fatalf("expected provider fallback to usage provider, got %q", transaction.Provider)
	}
}

func openAdminDashboardProviderDisplayTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	conn, errOpen := dbpkg.Open(":memory:")
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := dbpkg.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return conn
}

type adminDashboardProviderDisplayTransaction struct {
	Provider string `json:"provider"`
}

func fetchSingleAdminDashboardTransaction(t *testing.T, conn *gorm.DB) adminDashboardProviderDisplayTransaction {
	t.Helper()
	h := NewDashboardHandler(conn)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions?page=1&page_size=10", nil)

	h.RecentTransactions(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Transactions []adminDashboardProviderDisplayTransaction `json:"transactions"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if len(resp.Transactions) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(resp.Transactions))
	}
	return resp.Transactions[0]
}
