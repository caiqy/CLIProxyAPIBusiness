package store

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestGormAuthStoreListExcludesUnavailable(t *testing.T) {
	db, errOpen := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}
	if errMigrate := db.AutoMigrate(&models.Auth{}); errMigrate != nil {
		t.Fatalf("migrate auth model: %v", errMigrate)
	}

	now := time.Now().UTC()
	rows := []models.Auth{
		{
			Key:         "available-auth",
			Content:     datatypes.JSON([]byte(`{"type":"antigravity","email":"available@example.com"}`)),
			IsAvailable: true,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Key:         "disabled-auth",
			Content:     datatypes.JSON([]byte(`{"type":"antigravity","email":"disabled@example.com"}`)),
			IsAvailable: false,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	if errCreate := db.Create(&rows).Error; errCreate != nil {
		t.Fatalf("seed auth rows: %v", errCreate)
	}
	if errUpdate := db.Model(&models.Auth{}).
		Where("key = ?", "disabled-auth").
		Updates(map[string]any{"is_available": false, "updated_at": now}).Error; errUpdate != nil {
		t.Fatalf("mark disabled auth unavailable: %v", errUpdate)
	}

	store := NewGormAuthStore(db)
	auths, errList := store.List(context.Background())
	if errList != nil {
		t.Fatalf("list auths: %v", errList)
	}

	if len(auths) != 1 {
		t.Fatalf("expected 1 available auth, got %d", len(auths))
	}
	if auths[0] == nil || auths[0].ID != "available-auth" {
		t.Fatalf("expected available-auth, got %#v", auths[0])
	}
}
