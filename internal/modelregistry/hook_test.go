package modelregistry

import (
	"context"
	"testing"
	"time"

	sdkcliproxy "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
)

func TestHookOnModelsRegistered_DoesNotSeedIdentityForExistingAlias(t *testing.T) {
	conn, errOpen := db.Open(":memory:")
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := db.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}

	now := time.Now().UTC()
	existing := models.ModelMapping{
		Provider:     "claude",
		ModelName:    "real-model",
		NewModelName: "alias",
		IsEnabled:    true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if errCreate := conn.Create(&existing).Error; errCreate != nil {
		t.Fatalf("create mapping: %v", errCreate)
	}

	hook := NewHook(conn, nil)
	hook.OnModelsRegistered(context.Background(), "claude", "client-1", []*sdkcliproxy.ModelInfo{{ID: "alias"}})

	var count int64
	if errCount := conn.Model(&models.ModelMapping{}).Where("provider = ?", "claude").Count(&count).Error; errCount != nil {
		t.Fatalf("count mappings: %v", errCount)
	}
	if count != 1 {
		t.Fatalf("expected 1 mapping, got %d", count)
	}
}

func TestHookOnModelsRegistered_SeedsMissingMappings(t *testing.T) {
	conn, errOpen := db.Open(":memory:")
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := db.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}

	hook := NewHook(conn, nil)
	hook.OnModelsRegistered(context.Background(), "claude", "client-1", []*sdkcliproxy.ModelInfo{{ID: "m1"}})

	var rows []models.ModelMapping
	if errFind := conn.Model(&models.ModelMapping{}).Where("provider = ?", "claude").Find(&rows).Error; errFind != nil {
		t.Fatalf("load mappings: %v", errFind)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 mapping row, got %d", len(rows))
	}
	if rows[0].ModelName != "m1" || rows[0].NewModelName != "m1" {
		t.Fatalf("expected m1->m1 mapping, got %q->%q", rows[0].ModelName, rows[0].NewModelName)
	}
	if !rows[0].IsEnabled {
		t.Fatalf("expected seeded mapping to be enabled")
	}
}
