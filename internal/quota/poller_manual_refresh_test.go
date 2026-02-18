package quota

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
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
	if errMigrate := db.AutoMigrate(&models.Auth{}, &models.Quota{}); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return db
}

type sequenceProviderExecutor struct {
	mu       sync.Mutex
	statuses []int
	bodies   []string
	index    int
}

type authHealthSnapshot struct {
	TokenInvalid    bool
	LastAuthCheckAt string
	LastAuthError   string
}

func (s *sequenceProviderExecutor) Identifier() string {
	return "codex"
}

func (s *sequenceProviderExecutor) Execute(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, errors.New("not implemented")
}

func (s *sequenceProviderExecutor) ExecuteStream(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (<-chan cliproxyexecutor.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (s *sequenceProviderExecutor) Refresh(context.Context, *coreauth.Auth) (*coreauth.Auth, error) {
	return nil, errors.New("not implemented")
}

func (s *sequenceProviderExecutor) CountTokens(context.Context, *coreauth.Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, errors.New("not implemented")
}

func (s *sequenceProviderExecutor) PrepareRequest(_ *http.Request, _ *coreauth.Auth) error {
	return nil
}

func (s *sequenceProviderExecutor) HttpRequest(_ context.Context, _ *coreauth.Auth, _ *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := http.StatusOK
	body := `{"ok":true}`
	if s.index < len(s.statuses) {
		status = s.statuses[s.index]
	}
	if s.index < len(s.bodies) {
		body = s.bodies[s.index]
	}
	s.index++
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
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

func TestRefreshAuthMarksTokenInvalidOnUnauthorizedStatus(t *testing.T) {
	db := setupPollerManualRefreshDB(t)
	manager := coreauth.NewManager(nil, nil, nil)
	executor := &sequenceProviderExecutor{statuses: []int{http.StatusUnauthorized}}
	manager.RegisterExecutor(executor)

	authRecord := &coreauth.Auth{
		ID:       "auth-unauthorized",
		Provider: "codex",
		Metadata: map[string]any{"account_id": "acct-test"},
	}
	auth, errRegister := manager.Register(context.Background(), authRecord)
	if errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	row := models.Auth{Key: auth.ID, Content: datatypes.JSON([]byte(`{"type":"codex"}`))}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	poller := &Poller{db: db, manager: manager}
	errRefresh := poller.refreshAuth(context.Background(), auth, authRowInfo{ID: row.ID, Type: "codex"})
	if errRefresh == nil {
		t.Fatalf("expected refresh error on 401 response")
	}

	var updated authHealthSnapshot
	if errLoad := db.Table("auths").
		Select("token_invalid", "last_auth_check_at", "last_auth_error").
		Where("id = ?", row.ID).
		Take(&updated).Error; errLoad != nil {
		t.Fatalf("load updated auth row: %v", errLoad)
	}
	if !updated.TokenInvalid {
		t.Fatalf("expected token_invalid=true after 401")
	}
	if strings.TrimSpace(updated.LastAuthCheckAt) == "" {
		t.Fatalf("expected last_auth_check_at to be set")
	}
	if strings.TrimSpace(updated.LastAuthError) == "" {
		t.Fatalf("expected last_auth_error to be set")
	}
}

func TestRefreshAuthClearsTokenInvalidAfterSuccessfulRefresh(t *testing.T) {
	db := setupPollerManualRefreshDB(t)
	manager := coreauth.NewManager(nil, nil, nil)
	executor := &sequenceProviderExecutor{
		statuses: []int{http.StatusForbidden, http.StatusOK},
		bodies:   []string{`{"error":"forbidden"}`, `{"ok":true}`},
	}
	manager.RegisterExecutor(executor)

	authRecord := &coreauth.Auth{
		ID:       "auth-recovery",
		Provider: "codex",
		Metadata: map[string]any{"account_id": "acct-test"},
	}
	auth, errRegister := manager.Register(context.Background(), authRecord)
	if errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	row := models.Auth{Key: auth.ID, Content: datatypes.JSON([]byte(`{"type":"codex"}`))}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	poller := &Poller{db: db, manager: manager}
	errFirst := poller.refreshAuth(context.Background(), auth, authRowInfo{ID: row.ID, Type: "codex"})
	if errFirst == nil {
		t.Fatalf("expected first refresh to fail on 403")
	}
	errSecond := poller.refreshAuth(context.Background(), auth, authRowInfo{ID: row.ID, Type: "codex"})
	if errSecond != nil {
		t.Fatalf("expected second refresh to succeed, got %v", errSecond)
	}

	var updated authHealthSnapshot
	if errLoad := db.Table("auths").
		Select("token_invalid", "last_auth_check_at", "last_auth_error").
		Where("id = ?", row.ID).
		Take(&updated).Error; errLoad != nil {
		t.Fatalf("load updated auth row: %v", errLoad)
	}
	if updated.TokenInvalid {
		t.Fatalf("expected token_invalid=false after successful refresh")
	}
	if strings.TrimSpace(updated.LastAuthError) != "" {
		t.Fatalf("expected last_auth_error to be cleared, got %q", updated.LastAuthError)
	}
	if strings.TrimSpace(updated.LastAuthCheckAt) == "" {
		t.Fatalf("expected last_auth_check_at to stay set")
	}
}

func TestRefreshAuthKeepsTokenInvalidOnNonAuthFailureAfterInvalid(t *testing.T) {
	db := setupPollerManualRefreshDB(t)
	manager := coreauth.NewManager(nil, nil, nil)
	executor := &sequenceProviderExecutor{
		statuses: []int{http.StatusUnauthorized, http.StatusInternalServerError},
		bodies:   []string{`{"error":"unauthorized"}`, `{"error":"server error"}`},
	}
	manager.RegisterExecutor(executor)

	authRecord := &coreauth.Auth{
		ID:       "auth-keep-invalid",
		Provider: "codex",
		Metadata: map[string]any{"account_id": "acct-test"},
	}
	auth, errRegister := manager.Register(context.Background(), authRecord)
	if errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	row := models.Auth{Key: auth.ID, Content: datatypes.JSON([]byte(`{"type":"codex"}`))}
	if errCreate := db.Create(&row).Error; errCreate != nil {
		t.Fatalf("create auth row: %v", errCreate)
	}

	poller := &Poller{db: db, manager: manager}
	errFirst := poller.refreshAuth(context.Background(), auth, authRowInfo{ID: row.ID, Type: "codex"})
	if errFirst == nil {
		t.Fatalf("expected first refresh to fail on 401")
	}
	errSecond := poller.refreshAuth(context.Background(), auth, authRowInfo{ID: row.ID, Type: "codex"})
	if errSecond == nil {
		t.Fatalf("expected second refresh to fail on 500")
	}

	var updated authHealthSnapshot
	if errLoad := db.Table("auths").
		Select("token_invalid", "last_auth_check_at", "last_auth_error").
		Where("id = ?", row.ID).
		Take(&updated).Error; errLoad != nil {
		t.Fatalf("load updated auth row: %v", errLoad)
	}
	if !updated.TokenInvalid {
		t.Fatalf("expected token_invalid=true to be preserved after 500")
	}
	if strings.TrimSpace(updated.LastAuthError) == "" {
		t.Fatalf("expected last_auth_error to be set on 500")
	}
}
