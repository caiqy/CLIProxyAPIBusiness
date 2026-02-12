package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/access"
	log "github.com/sirupsen/logrus"
)

// CLIProxyAuthMiddleware enforces CLIProxyAPI authentication on selected routes.
func CLIProxyAuthMiddleware(manager *sdkaccess.Manager, websocketAuth bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if manager == nil || c == nil || c.Request == nil || c.Request.URL == nil {
			if c != nil {
				c.Next()
			}
			return
		}

		path := c.Request.URL.Path
		if !requiresCLIProxyAuth(path, websocketAuth) {
			c.Next()
			return
		}

		result, authErr := manager.Authenticate(c.Request.Context(), c.Request)
		if authErr == nil {
			if result != nil {
				c.Set("apiKey", result.Principal)
				c.Set("accessProvider", result.Provider)
				if len(result.Metadata) > 0 {
					c.Set("accessMetadata", result.Metadata)
				}
			}
			c.Next()
			return
		}

		switch {
		case errors.Is(authErr, access.ErrDailyMaxUsageExceeded):
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Daily max usage exceeded",
				"code":  "daily_max_usage_exceeded",
			})
		case errors.Is(authErr, access.ErrInsufficientBalance):
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{"error": "Insufficient balance"})
		case sdkaccess.IsAuthErrorCode(authErr, sdkaccess.AuthErrorCodeNoCredentials):
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing API key"})
		case sdkaccess.IsAuthErrorCode(authErr, sdkaccess.AuthErrorCodeInvalidCredential):
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
		default:
			log.Errorf("authentication middleware error: %v", authErr)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Authentication service error"})
		}
	}
}

// requiresCLIProxyAuth determines whether a path needs auth enforcement.
func requiresCLIProxyAuth(path string, websocketAuth bool) bool {
	if hasPathPrefix(path, "/v1") {
		if path == "/v1/ws" && !websocketAuth {
			return false
		}
		return true
	}
	if hasPathPrefix(path, "/v1beta") {
		return true
	}
	if hasPathPrefix(path, "/api") {
		return true
	}
	return false
}

// hasPathPrefix checks a prefix match on a path boundary.
func hasPathPrefix(path string, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if len(path) == len(prefix) {
		return true
	}
	return path[len(prefix)] == '/'
}
