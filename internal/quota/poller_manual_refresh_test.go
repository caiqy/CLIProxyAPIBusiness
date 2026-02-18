package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupPollerManualRefreshDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:poller_manual_refresh_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := db.AutoMigrate(&models.Auth{}); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return db
}

func TestRefreshAuthReturnsUnsupportedProvider(t *testing.T) {
	poller := &Poller{manager: coreauth.NewManager(nil, nil, nil)}
	auth := &coreauth.Auth{ID: "auth-1", Provider: "unsupported-provider"}

	err := poller.refreshAuth(context.Background(), auth, authRowInfo{ID: 1, Type: "unsupported-provider"})
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected ErrUnsupportedProvider, got %v", err)
	}
}

func TestRefreshByAuthKeyReturnsClearErrorWhenAuthKeyMissing(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	_, errRegister := manager.Register(context.Background(), &coreauth.Auth{ID: "existing-auth", Provider: "antigravity"})
	if errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	poller := &Poller{manager: manager}
	err := poller.RefreshByAuthKey(context.Background(), "missing-auth-key")
	if err == nil {
		t.Fatal("expected error for missing auth key, got nil")
	}
	if !strings.Contains(err.Error(), "auth key not found") {
		t.Fatalf("expected missing auth key error, got %v", err)
	}
}

func TestRefreshAuthReturnsErrorWhenCodexAccountIDMissing(t *testing.T) {
	poller := &Poller{manager: coreauth.NewManager(nil, nil, nil)}
	auth := &coreauth.Auth{ID: "auth-codex", Provider: "codex"}

	err := poller.refreshAuth(context.Background(), auth, authRowInfo{ID: 1, Type: "codex"})
	if err == nil {
		t.Fatal("expected error when codex account id missing")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "account") {
		t.Fatalf("expected account id related error, got %v", err)
	}
}

func TestLoadAuthRowByKeyReturnsOnlyTargetRow(t *testing.T) {
	db := setupPollerManualRefreshDB(t)
	runtimeOnlyPayload := datatypes.JSON([]byte(`{"type":"codex","runtime_only":true}`))
	targetPayload := datatypes.JSON([]byte(`{"type":"antigravity"}`))

	rows := []models.Auth{
		{Key: "other-key", Content: runtimeOnlyPayload},
		{Key: "target-key", Content: targetPayload},
	}
	if errCreate := db.Create(&rows).Error; errCreate != nil {
		t.Fatalf("create auth rows: %v", errCreate)
	}

	poller := &Poller{db: db}
	row, ok, errLoad := poller.loadAuthRowByKey(context.Background(), "target-key")
	if errLoad != nil {
		t.Fatalf("loadAuthRowByKey returned error: %v", errLoad)
	}
	if !ok {
		t.Fatalf("expected target row found")
	}
	if row.Type != "antigravity" {
		t.Fatalf("expected type antigravity, got %q", row.Type)
	}
	if row.RuntimeOnly {
		t.Fatalf("expected runtime_only false for target row")
	}
}

func TestLoadAuthRowByKeyReturnsNotFound(t *testing.T) {
	db := setupPollerManualRefreshDB(t)
	poller := &Poller{db: db}

	_, ok, errLoad := poller.loadAuthRowByKey(context.Background(), "missing-key")
	if errLoad != nil {
		t.Fatalf("expected no error for missing key, got %v", errLoad)
	}
	if ok {
		t.Fatalf("expected missing key to return ok=false")
	}
}
