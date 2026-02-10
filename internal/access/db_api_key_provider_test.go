package access

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"gorm.io/gorm"
)

func openDBAPIKeyProviderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:db_api_key_provider_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	return db
}

func newDBAPIKeyProviderForPathTest(t *testing.T) *DBAPIKeyProvider {
	t.Helper()
	return &DBAPIKeyProvider{
		db:                 openDBAPIKeyProviderTestDB(t),
		name:               ProviderTypeDBAPIKey,
		header:             "Authorization",
		scheme:             "Bearer",
		allowXAPIKey:       true,
		bypassPathPrefixes: []string{"/healthz", "/v0/management"},
	}
}

func TestDBAPIKeyProviderAuthenticateSkipsUnprotectedPath(t *testing.T) {
	provider := newDBAPIKeyProviderForPathTest(t)
	req := httptest.NewRequest("GET", "/assets/app.js", nil)

	result, authErr := provider.Authenticate(context.Background(), req)

	if authErr != nil {
		t.Fatalf("expected nil authErr, got %v", authErr)
	}
	if result != nil {
		t.Fatalf("expected nil result")
	}
}

func TestDBAPIKeyProviderAuthenticateSkipsPrefixBoundaryPath(t *testing.T) {
	provider := newDBAPIKeyProviderForPathTest(t)
	req := httptest.NewRequest("GET", "/v10/models", nil)

	result, authErr := provider.Authenticate(context.Background(), req)

	if authErr != nil {
		t.Fatalf("expected nil authErr, got %v", authErr)
	}
	if result != nil {
		t.Fatalf("expected nil result")
	}
}

func TestDBAPIKeyProviderAuthenticateRequiresAuthOnCLIProxyPath(t *testing.T) {
	provider := newDBAPIKeyProviderForPathTest(t)
	req := httptest.NewRequest("GET", "/v1/models", nil)

	_, authErr := provider.Authenticate(context.Background(), req)

	if !sdkaccess.IsAuthErrorCode(authErr, sdkaccess.AuthErrorCodeNoCredentials) {
		t.Fatalf("expected no_credentials auth error, got %v", authErr)
	}
}
