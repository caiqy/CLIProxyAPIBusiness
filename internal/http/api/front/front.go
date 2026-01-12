package front

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/config"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/http/api/front/handlers"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/modelregistry"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/security"
	"gorm.io/gorm"
)

// RegisterFrontRoutes registers public and authenticated front-end routes.
func RegisterFrontRoutes(r *gin.Engine, db *gorm.DB, jwtCfg config.JWTConfig, modelStore *modelregistry.Store) {
	if r == nil || db == nil {
		return
	}

	front := r.Group("/v0/front")

	authHandler := handlers.NewAuthHandler(db, jwtCfg)
	front.POST("/register", authHandler.Register)
	front.POST("/login", authHandler.Login)
	front.POST("/login/prepare", authHandler.LoginPrepare)
	front.POST("/login/totp", authHandler.LoginTOTP)
	front.POST("/login/passkey/options", authHandler.LoginPasskeyOptions)
	front.POST("/login/passkey/verify", authHandler.LoginPasskeyVerify)
	front.POST("/reset-password", authHandler.ResetPassword)
	front.GET("/config", handlers.GetPublicConfig)

	authed := front.Group("")
	authed.Use(userAuthMiddleware(db, jwtCfg))

	profileHandler := handlers.NewProfileHandler(db)
	authed.GET("/profile", profileHandler.Get)
	authed.PUT("/profile/password", profileHandler.ChangePassword)

	webAuthn, errWebAuthn := security.NewWebAuthn()
	if errWebAuthn != nil {
		webAuthn = nil
	}
	mfaHandler := handlers.NewMFAHandler(db, webAuthn)
	authed.GET("/mfa/status", mfaHandler.Status)
	authed.POST("/mfa/totp/prepare", mfaHandler.PrepareTOTP)
	authed.POST("/mfa/totp/confirm", mfaHandler.ConfirmTOTP)
	authed.POST("/mfa/totp/disable", mfaHandler.DisableTOTP)
	authed.POST("/mfa/passkey/options", mfaHandler.BeginPasskeyRegistration)
	authed.POST("/mfa/passkey/verify", mfaHandler.FinishPasskeyRegistration)
	authed.POST("/mfa/passkey/disable", mfaHandler.DisablePasskey)

	prepaidHandler := handlers.NewPrepaidCardFrontHandler(db)
	authed.GET("/prepaid-card", prepaidHandler.GetCurrent)
	authed.POST("/prepaid-card/redeem", prepaidHandler.Redeem)
	authed.GET("/prepaid-cards", prepaidHandler.List)

	planHandler := handlers.NewPlanFrontHandler(db)
	authed.GET("/plans", planHandler.List)

	billHandler := handlers.NewBillFrontHandler(db)
	authed.POST("/bills", billHandler.Create)
	authed.GET("/bills", billHandler.List)

	apiKeyHandler := handlers.NewAPIKeyHandler(db)
	authed.GET("/api-keys", apiKeyHandler.List)
	authed.GET("/api-keys/stats", apiKeyHandler.Stats)
	authed.POST("/api-keys", apiKeyHandler.Create)
	authed.PUT("/api-keys/:id", apiKeyHandler.Update)
	authed.POST("/api-keys/:id/revoke", apiKeyHandler.Revoke)
	authed.DELETE("/api-keys/:id", apiKeyHandler.Delete)
	authed.POST("/api-keys/:id/renew", apiKeyHandler.Renew)
	authed.POST("/api-keys/:id/regenerate", apiKeyHandler.Regenerate)

	usageHandler := handlers.NewUsageHandler(db)
	authed.GET("/usage/stats", usageHandler.Stats)

	dashboardHandler := handlers.NewDashboardHandler(db)
	authed.GET("/dashboard/kpi", dashboardHandler.KPI)
	authed.GET("/dashboard/traffic", dashboardHandler.Traffic)
	authed.GET("/dashboard/cost-distribution", dashboardHandler.CostDistribution)
	authed.GET("/dashboard/model-health", dashboardHandler.ModelHealth)
	authed.GET("/dashboard/transactions", dashboardHandler.RecentTransactions)

	modelPricingHandler := handlers.NewModelPricingHandler(db, modelStore)
	authed.GET("/models/pricing", modelPricingHandler.List)

	logsHandler := handlers.NewLogsHandler(db)
	authed.GET("/logs", logsHandler.List)
	authed.GET("/logs/stats", logsHandler.Stats)
	authed.GET("/logs/trend", logsHandler.Trend)
	authed.GET("/logs/models", logsHandler.Models)
	authed.GET("/logs/projects", logsHandler.Projects)
	authed.GET("/logs/detail", logsHandler.Detail)
}

// userAuthMiddleware validates user JWTs and loads the user into context.
func userAuthMiddleware(db *gorm.DB, jwtCfg config.JWTConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			return
		}
		token = strings.TrimSpace(token)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "empty token"})
			return
		}

		claims, errJWT := security.ParseToken(jwtCfg.Secret, token)
		if errJWT != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		var user models.User
		if errFind := db.WithContext(c.Request.Context()).First(&user, claims.UserID).Error; errFind != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}
		if user.Disabled {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "user disabled"})
			return
		}

		c.Set("userID", user.ID)
		c.Next()
	}
}
