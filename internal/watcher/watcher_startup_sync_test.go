package watcher

import (
	"context"
	"fmt"
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
	dsn := fmt.Sprintf("file:watcher_startup_sync_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
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
			ProxyURL:    "http://127.0.0.1:7002",
			Content:     datatypes.JSON([]byte(`{"type":"antigravity","email":"available@example.com","proxy_url":"http://127.0.0.1:7001"}`)),
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
	if snapshot[0].ProxyURL != "http://127.0.0.1:7002" {
		t.Fatalf("expected db proxy url to win, got %q", snapshot[0].ProxyURL)
	}

	if _, exists := w.pending["disabled-auth"]; exists {
		t.Fatal("unexpected update generated for disabled-auth")
	}
}

func TestPollAuth_UpdatesRuntimeProxyURLAfterDBProxyChange(t *testing.T) {
	dsn := fmt.Sprintf("file:watcher_startup_sync_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}
	if errMigrate := db.AutoMigrate(&models.Auth{}); errMigrate != nil {
		t.Fatalf("migrate auth model: %v", errMigrate)
	}

	now := time.Now().UTC()
	row := models.Auth{
		Key:         "poll-auth-proxy",
		ProxyURL:    "http://127.0.0.1:7002",
		Content:     datatypes.JSON([]byte(`{"type":"antigravity","email":"proxy@example.com","proxy_url":"http://127.0.0.1:7001"}`)),
		IsAvailable: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("seed auth row: %v", errCreate)
	}

	w := &dbWatcher{
		db:         db,
		authStates: make(map[string]authState),
		pending:    make(map[string]authUpdate, defaultDispatchBuffer),
	}
	w.dispatchCond = sync.NewCond(&w.dispatchMu)

	w.pollAuth(context.Background(), true)
	first := w.SnapshotAuths()
	if len(first) != 1 || first[0] == nil {
		t.Fatalf("expected one runtime auth after first poll, got %#v", first)
	}
	if first[0].ProxyURL != "http://127.0.0.1:7002" {
		t.Fatalf("expected first proxy from db column, got %q", first[0].ProxyURL)
	}

	updatedAt := now.Add(2 * time.Second)
	if errUpdate := db.Model(&models.Auth{}).
		Where("key = ?", "poll-auth-proxy").
		Updates(map[string]any{"proxy_url": "http://127.0.0.1:7003", "updated_at": updatedAt}).Error; errUpdate != nil {
		t.Fatalf("update db proxy_url failed: %v", errUpdate)
	}

	w.pollAuth(context.Background(), false)
	second := w.SnapshotAuths()
	if len(second) != 1 || second[0] == nil {
		t.Fatalf("expected one runtime auth after second poll, got %#v", second)
	}
	if second[0].ProxyURL != "http://127.0.0.1:7003" {
		t.Fatalf("expected updated proxy from db column, got %q", second[0].ProxyURL)
	}
}
