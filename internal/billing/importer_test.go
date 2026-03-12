package billing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

func setupImporterDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:billing_import_%d?mode=memory&cache=shared", time.Now().UnixNano())
	conn, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}
	if errMigrate := db.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return conn
}

func TestImportFromModelMappings_NormalizesProviderAndUpserts(t *testing.T) {
	conn := setupImporterDB(t)

	mapping := models.ModelMapping{
		Provider:     " OpenAI ",
		ModelName:    "gpt-5.3-codex",
		NewModelName: "gpt-5.3-codex",
		IsEnabled:    true,
	}
	if errCreate := conn.Create(&mapping).Error; errCreate != nil {
		t.Fatalf("create mapping: %v", errCreate)
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

	oldInput := 1.25
	oldOutput := 10.0
	oldCacheRead := 0.125
	oldCacheWrite := 1.25
	existing := models.BillingRule{
		AuthGroupID:           1,
		UserGroupID:           1,
		Provider:              "openai",
		Model:                 "gpt-5.3-codex",
		BillingType:           models.BillingTypePerToken,
		PriceInputToken:       &oldInput,
		PriceOutputToken:      &oldOutput,
		PriceCacheReadToken:   &oldCacheRead,
		PriceCacheCreateToken: &oldCacheWrite,
		IsEnabled:             true,
	}
	if errCreate := conn.Create(&existing).Error; errCreate != nil {
		t.Fatalf("create existing billing rule: %v", errCreate)
	}

	result, errImport := ImportFromModelMappings(context.Background(), conn, 1, 1, models.BillingTypePerToken)
	if errImport != nil {
		t.Fatalf("import from model mappings: %v", errImport)
	}
	if result.Created != 0 || result.Updated != 1 {
		t.Fatalf("unexpected import result: %+v", result)
	}

	var count int64
	if errCount := conn.Raw(`
		SELECT COUNT(*)
		FROM billing_rules
		WHERE auth_group_id = ?
		  AND user_group_id = ?
		  AND lower(trim(provider)) = ?
		  AND model = ?
	`, 1, 1, "openai", "gpt-5.3-codex").Scan(&count).Error; errCount != nil {
		t.Fatalf("count billing rules: %v", errCount)
	}
	if count != 1 {
		t.Fatalf("expected exactly one billing rule after upsert, got %d", count)
	}

	var row models.BillingRule
	if errFind := conn.Where("auth_group_id = ? AND user_group_id = ? AND provider = ? AND model = ?", 1, 1, "openai", "gpt-5.3-codex").First(&row).Error; errFind != nil {
		t.Fatalf("find upserted billing rule: %v", errFind)
	}
	if row.PriceInputToken == nil || *row.PriceInputToken != input {
		t.Fatalf("unexpected input price: %+v", row.PriceInputToken)
	}
	if row.PriceOutputToken == nil || *row.PriceOutputToken != output {
		t.Fatalf("unexpected output price: %+v", row.PriceOutputToken)
	}
	if row.PriceCacheReadToken == nil || *row.PriceCacheReadToken != cacheRead {
		t.Fatalf("unexpected cache read price: %+v", row.PriceCacheReadToken)
	}
	if row.PriceCacheCreateToken == nil || *row.PriceCacheCreateToken != cacheWrite {
		t.Fatalf("unexpected cache create price: %+v", row.PriceCacheCreateToken)
	}
}
