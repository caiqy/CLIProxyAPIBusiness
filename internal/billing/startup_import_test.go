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

func setupStartupImportDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:billing_startup_import_%d?mode=memory&cache=shared", time.Now().UnixNano())
	conn, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}
	if errMigrate := db.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return conn
}

func TestAutoImportDefaultGroupOnce_ImportsWhenModelReferencesReady(t *testing.T) {
	conn := setupStartupImportDB(t)

	if errCreate := conn.Create(&models.AuthGroup{Name: "default-auth", IsDefault: true}).Error; errCreate != nil {
		t.Fatalf("create default auth group: %v", errCreate)
	}
	if errCreate := conn.Create(&models.UserGroup{Name: "default-user", IsDefault: true}).Error; errCreate != nil {
		t.Fatalf("create default user group: %v", errCreate)
	}

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

	if errImport := AutoImportDefaultGroupOnce(context.Background(), conn, 2*time.Second, 20*time.Millisecond); errImport != nil {
		t.Fatalf("auto import default group once: %v", errImport)
	}

	var row models.BillingRule
	if errFind := conn.Where("provider = ? AND model = ?", "openai", "gpt-5.3-codex").First(&row).Error; errFind != nil {
		t.Fatalf("find imported billing rule: %v", errFind)
	}
	if row.PriceInputToken == nil || *row.PriceInputToken != input {
		t.Fatalf("unexpected imported input price")
	}
}

func TestAutoImportDefaultGroupOnce_TimesOutWhenModelReferencesNotReady(t *testing.T) {
	conn := setupStartupImportDB(t)

	if errCreate := conn.Create(&models.AuthGroup{Name: "default-auth", IsDefault: true}).Error; errCreate != nil {
		t.Fatalf("create default auth group: %v", errCreate)
	}
	if errCreate := conn.Create(&models.UserGroup{Name: "default-user", IsDefault: true}).Error; errCreate != nil {
		t.Fatalf("create default user group: %v", errCreate)
	}

	errImport := AutoImportDefaultGroupOnce(context.Background(), conn, 60*time.Millisecond, 20*time.Millisecond)
	if errImport == nil {
		t.Fatal("expected timeout error when model references are not ready")
	}
}
