package watcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestStartupLoadAndFirstWatcherPollIgnoreUnavailableAuths(t *testing.T) {
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
			UpdatedAt:   now.Add(time.Second),
		},
	}
	if errCreate := db.Create(&rows).Error; errCreate != nil {
		t.Fatalf("seed auth rows: %v", errCreate)
	}
	if errUpdate := db.Model(&models.Auth{}).
		Where("key = ?", "disabled-auth").
		Updates(map[string]any{"is_available": false, "updated_at": now.Add(time.Second)}).Error; errUpdate != nil {
		t.Fatalf("mark disabled auth unavailable: %v", errUpdate)
	}

	coreStore := store.NewGormAuthStore(db)
	manager := auth.NewManager(coreStore, nil, nil)
	if errLoad := manager.Load(context.Background()); errLoad != nil {
		t.Fatalf("core manager load: %v", errLoad)
	}
	loaded := manager.List()
	if len(loaded) != 1 {
		t.Fatalf("expected 1 loaded auth, got %d", len(loaded))
	}
	if loaded[0] == nil || loaded[0].ID != "available-auth" {
		t.Fatalf("expected loaded auth available-auth, got %#v", loaded[0])
	}

	w := &dbWatcher{
		db:         db,
		authStates: make(map[string]authState),
		pending:    make(map[string]authUpdate, defaultDispatchBuffer),
	}
	w.dispatchCond = sync.NewCond(&w.dispatchMu)
	w.pollAuth(context.Background(), true)

	snapshot := w.SnapshotAuths()
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 auth after first poll, got %d", len(snapshot))
	}
	if snapshot[0] == nil || snapshot[0].ID != "available-auth" {
		t.Fatalf("expected polled auth available-auth, got %#v", snapshot[0])
	}

	if _, exists := w.pending["disabled-auth"]; exists {
		t.Fatal("unexpected update generated for disabled-auth")
	}
}
