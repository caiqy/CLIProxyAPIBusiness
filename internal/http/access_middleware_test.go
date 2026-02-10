package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/access"
)

type stubAccessProvider struct {
	result  *sdkaccess.Result
	authErr *sdkaccess.AuthError
}

func (s *stubAccessProvider) Identifier() string {
	return "stub"
}

func (s *stubAccessProvider) Authenticate(_ context.Context, _ *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if s == nil {
		return nil, sdkaccess.NewNotHandledError()
	}
	return s.result, s.authErr
}

func buildTestAccessManager(provider sdkaccess.Provider) *sdkaccess.Manager {
	manager := sdkaccess.NewManager()
	manager.SetProviders([]sdkaccess.Provider{provider})
	return manager
}

func runRequestWithMiddleware(t *testing.T, middleware gin.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware)
	router.GET("/*path", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	responseRecorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(responseRecorder, req)

	return responseRecorder
}

func TestAccessAuthMiddlewareMapsNoCredentialsToUnauthorized(t *testing.T) {
	provider := &stubAccessProvider{authErr: sdkaccess.NewNoCredentialsError()}
	manager := buildTestAccessManager(provider)

	responseRecorder := runRequestWithMiddleware(t, AccessAuthMiddleware(manager), "/healthz")

	if responseRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", responseRecorder.Code)
	}
}

func TestAccessAuthMiddlewareMapsInsufficientBalanceToPaymentRequired(t *testing.T) {
	provider := &stubAccessProvider{authErr: sdkaccess.NewInternalAuthError("Insufficient balance", access.ErrInsufficientBalance)}
	manager := buildTestAccessManager(provider)

	responseRecorder := runRequestWithMiddleware(t, AccessAuthMiddleware(manager), "/v1/models")

	if responseRecorder.Code != http.StatusPaymentRequired {
		t.Fatalf("expected status 402, got %d", responseRecorder.Code)
	}
}

func TestAccessAuthMiddlewareMapsInvalidCredentialToUnauthorized(t *testing.T) {
	provider := &stubAccessProvider{authErr: sdkaccess.NewInvalidCredentialError()}
	manager := buildTestAccessManager(provider)

	responseRecorder := runRequestWithMiddleware(t, AccessAuthMiddleware(manager), "/v1/models")

	if responseRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", responseRecorder.Code)
	}
}

func TestAccessAuthMiddlewareAllowsRequestWhenAuthenticated(t *testing.T) {
	provider := &stubAccessProvider{result: &sdkaccess.Result{Provider: "stub", Principal: "p-1"}}
	manager := buildTestAccessManager(provider)

	responseRecorder := runRequestWithMiddleware(t, AccessAuthMiddleware(manager), "/v1/models")

	if responseRecorder.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", responseRecorder.Code)
	}
}
