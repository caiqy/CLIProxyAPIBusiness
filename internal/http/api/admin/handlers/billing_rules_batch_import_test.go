package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

type batchImportResponse struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

func setupBillingRulesHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:billing_handler_%d?mode=memory&cache=shared", time.Now().UnixNano())
	conn, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}
	if errMigrate := db.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate: %v", errMigrate)
	}
	return conn
}

func TestBillingRulesBatchImport_NormalizesProviderAndIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := setupBillingRulesHandlerDB(t)

	mapping := models.ModelMapping{
		Provider:     " OpenAI ",
		ModelName:    "gpt-5.3-codex",
		NewModelName: "gpt-5.3-codex",
		IsEnabled:    true,
	}
	if errCreate := conn.Create(&mapping).Error; errCreate != nil {
		t.Fatalf("create model mapping: %v", errCreate)
	}

	input := 1.75
	output := 14.0
	cacheRead := 0.175
	cacheWrite := 1.75
	ref := models.ModelReference{
		ProviderName:    "OpenAI",
		ModelName:       "GPT 5.3 Codex",
		ModelID:         "gpt-5.3-codex",
		InputPrice:      &input,
		OutputPrice:     &output,
		CacheReadPrice:  &cacheRead,
		CacheWritePrice: &cacheWrite,
		LastSeenAt:      time.Now().UTC(),
	}
	if errCreate := conn.Create(&ref).Error; errCreate != nil {
		t.Fatalf("create model reference: %v", errCreate)
	}

	handler := NewBillingRuleHandler(conn)
	body := []byte(`{"auth_group_id":1,"user_group_id":1,"billing_type":2}`)

	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/v0/admin/billing-rules/batch-import", bytes.NewReader(body))
	c1.Request.Header.Set("Content-Type", "application/json")
	handler.BatchImport(c1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first batch import status=%d body=%s", w1.Code, w1.Body.String())
	}
	var res1 batchImportResponse
	if errDecode := json.NewDecoder(w1.Body).Decode(&res1); errDecode != nil {
		t.Fatalf("decode first response: %v", errDecode)
	}
	if res1.Created != 1 {
		t.Fatalf("expected first import created=1, got %+v", res1)
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/v0/admin/billing-rules/batch-import", bytes.NewReader(body))
	c2.Request.Header.Set("Content-Type", "application/json")
	handler.BatchImport(c2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second batch import status=%d body=%s", w2.Code, w2.Body.String())
	}
	var res2 batchImportResponse
	if errDecode := json.NewDecoder(w2.Body).Decode(&res2); errDecode != nil {
		t.Fatalf("decode second response: %v", errDecode)
	}
	if res2.Updated < 1 {
		t.Fatalf("expected second import updated>=1, got %+v", res2)
	}

	var rows []models.BillingRule
	if errFind := conn.Where("auth_group_id = ? AND user_group_id = ? AND model = ?", 1, 1, "gpt-5.3-codex").Find(&rows).Error; errFind != nil {
		t.Fatalf("query billing rules: %v", errFind)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one billing rule row, got %d", len(rows))
	}
	if rows[0].Provider != "openai" {
		t.Fatalf("expected normalized provider 'openai', got %q", rows[0].Provider)
	}
}
