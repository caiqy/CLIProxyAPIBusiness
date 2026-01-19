package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

type modelReferencePriceResponse struct {
	Provider              string   `json:"provider"`
	Model                 string   `json:"model"`
	PriceInputToken       *float64 `json:"price_input_token"`
	PriceOutputToken      *float64 `json:"price_output_token"`
	PriceCacheCreateToken *float64 `json:"price_cache_create_token"`
	PriceCacheReadToken   *float64 `json:"price_cache_read_token"`
}

func setupModelReferenceDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:modelref_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if errMigrate := db.AutoMigrate(&models.ModelReference{}); errMigrate != nil {
		t.Fatalf("migrate: %v", errMigrate)
	}
	return db
}

func TestModelReferencePriceProviderMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupModelReferenceDB(t)
	input := 0.01
	output := 0.02
	cacheRead := 0.003
	cacheWrite := 0.004
	row := models.ModelReference{
		ProviderName:    "OpenAI",
		ModelName:       "gpt-4o",
		ModelID:         "gpt-4o",
		InputPrice:      &input,
		OutputPrice:     &output,
		CacheReadPrice:  &cacheRead,
		CacheWritePrice: &cacheWrite,
		LastSeenAt:      time.Now().UTC(),
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create: %v", errCreate)
	}

	handler := NewModelReferenceHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/model-references/price?provider=OpenAI&model_id=gpt-4o", nil)

	handler.GetPrice(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	var res modelReferencePriceResponse
	if errDecode := json.NewDecoder(w.Body).Decode(&res); errDecode != nil {
		t.Fatalf("decode: %v", errDecode)
	}
	if res.Provider != "OpenAI" || res.Model != "gpt-4o" {
		t.Fatalf("unexpected provider/model: %s/%s", res.Provider, res.Model)
	}
	if res.PriceInputToken == nil || *res.PriceInputToken != input {
		t.Fatalf("unexpected input price")
	}
	if res.PriceOutputToken == nil || *res.PriceOutputToken != output {
		t.Fatalf("unexpected output price")
	}
	if res.PriceCacheReadToken == nil || *res.PriceCacheReadToken != cacheRead {
		t.Fatalf("unexpected cache read price")
	}
	if res.PriceCacheCreateToken == nil || *res.PriceCacheCreateToken != cacheWrite {
		t.Fatalf("unexpected cache create price")
	}
}

func TestModelReferencePriceFallbackToAnyProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupModelReferenceDB(t)
	input := 0.11
	row := models.ModelReference{
		ProviderName: "OpenAI",
		ModelName:    "gpt-4o",
		ModelID:      "gpt-4o",
		InputPrice:   &input,
		LastSeenAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create: %v", errCreate)
	}

	handler := NewModelReferenceHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/model-references/price?provider=Anthropic&model_id=gpt-4o", nil)

	handler.GetPrice(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestModelReferencePriceMissingModelID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupModelReferenceDB(t)
	handler := NewModelReferenceHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/model-references/price", nil)

	handler.GetPrice(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestModelReferencePriceNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupModelReferenceDB(t)
	handler := NewModelReferenceHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/model-references/price?provider=OpenAI&model_id=missing", nil)

	handler.GetPrice(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

func TestModelReferencePriceBackfillModelID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupModelReferenceDB(t)
	input := 0.21
	row := models.ModelReference{
		ProviderName: "OpenAI",
		ModelName:    "gpt-4o",
		ModelID:      "",
		InputPrice:   &input,
		LastSeenAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create: %v", errCreate)
	}

	handler := NewModelReferenceHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/model-references/price?model_id=gpt-4o", nil)

	handler.GetPrice(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	var updated models.ModelReference
	if errFind := db.Where("provider_name = ? AND model_name = ?", "OpenAI", "gpt-4o").First(&updated).Error; errFind != nil {
		t.Fatalf("find: %v", errFind)
	}
	if updated.ModelID != "gpt-4o" {
		t.Fatalf("expected model id backfill")
	}
}
