package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/access"
	log "github.com/sirupsen/logrus"
)

// AccessAuthMiddleware authenticates API keys and injects access metadata.
func AccessAuthMiddleware(manager *sdkaccess.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if manager == nil {
			c.Next()
			return
		}

		result, err := manager.Authenticate(c.Request.Context(), c.Request)
		if err == nil {
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
		case errors.Is(err, sdkaccess.ErrNoCredentials):
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing API key"})
		case errors.Is(err, sdkaccess.ErrInvalidCredential):
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
		case errors.Is(err, access.ErrInsufficientBalance):
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{"error": "Insufficient balance"})
		default:
			log.WithError(err).Error("access auth middleware error")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Authentication service error"})
		}
	}
}
