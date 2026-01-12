package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// BillingRuleHandler manages admin CRUD endpoints for billing rules.
type BillingRuleHandler struct {
	db *gorm.DB // Database handle for billing rules.
}

// NewBillingRuleHandler constructs a billing rule handler.
func NewBillingRuleHandler(db *gorm.DB) *BillingRuleHandler {
	return &BillingRuleHandler{db: db}
}

// createBillingRuleRequest captures the payload for creating a billing rule.
type createBillingRuleRequest struct {
	AuthGroupID           uint64   `json:"auth_group_id"`            // Auth group ID.
	UserGroupID           uint64   `json:"user_group_id"`            // User group ID.
	Provider              string   `json:"provider"`                 // Provider name.
	Model                 string   `json:"model"`                    // Model name.
	BillingType           int      `json:"billing_type"`             // Billing type.
	PricePerRequest       *float64 `json:"price_per_request"`        // Price per request.
	PriceInputToken       *float64 `json:"price_input_token"`        // Price per input token.
	PriceOutputToken      *float64 `json:"price_output_token"`       // Price per output token.
	PriceCacheCreateToken *float64 `json:"price_cache_create_token"` // Price per cache create token.
	PriceCacheReadToken   *float64 `json:"price_cache_read_token"`   // Price per cache read token.
	IsEnabled             *bool    `json:"is_enabled"`               // Required enabled flag.
}

// Create validates input and inserts a billing rule.
func (h *BillingRuleHandler) Create(c *gin.Context) {
	var body createBillingRuleRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	if body.AuthGroupID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth_group_id is required"})
		return
	}
	if body.UserGroupID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_group_id is required"})
		return
	}
	if body.IsEnabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "is_enabled is required"})
		return
	}

	billingType := models.BillingType(body.BillingType)
	if billingType != models.BillingTypePerRequest && billingType != models.BillingTypePerToken {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing_type must be 1 (per_request) or 2 (per_token)"})
		return
	}

	if billingType == models.BillingTypePerRequest {
		if body.PricePerRequest == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_per_request is required for per_request billing"})
			return
		}
	} else {
		if body.PriceInputToken == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_input_token is required for per_token billing"})
			return
		}
		if body.PriceOutputToken == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_output_token is required for per_token billing"})
			return
		}
		if body.PriceCacheCreateToken == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_cache_create_token is required for per_token billing"})
			return
		}
		if body.PriceCacheReadToken == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_cache_read_token is required for per_token billing"})
			return
		}
	}

	provider := strings.TrimSpace(body.Provider)
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is required"})
		return
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	now := time.Now().UTC()
	rule := models.BillingRule{
		AuthGroupID:           body.AuthGroupID,
		UserGroupID:           body.UserGroupID,
		Provider:              provider,
		Model:                 model,
		BillingType:           billingType,
		PricePerRequest:       body.PricePerRequest,
		PriceInputToken:       body.PriceInputToken,
		PriceOutputToken:      body.PriceOutputToken,
		PriceCacheCreateToken: body.PriceCacheCreateToken,
		PriceCacheReadToken:   body.PriceCacheReadToken,
		IsEnabled:             *body.IsEnabled,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	if errCreate := h.db.WithContext(c.Request.Context()).Create(&rule).Error; errCreate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create billing rule failed"})
		return
	}
	c.JSON(http.StatusCreated, h.formatRule(&rule))
}

// List returns billing rules filtered by query parameters.
func (h *BillingRuleHandler) List(c *gin.Context) {
	var (
		authGroupIDQ = strings.TrimSpace(c.Query("auth_group_id"))
		userGroupIDQ = strings.TrimSpace(c.Query("user_group_id"))
		isEnabledQ   = strings.TrimSpace(c.Query("is_enabled"))
	)

	q := h.db.WithContext(c.Request.Context()).Model(&models.BillingRule{})
	if authGroupIDQ != "" {
		if id, errParse := strconv.ParseUint(authGroupIDQ, 10, 64); errParse == nil {
			q = q.Where("auth_group_id = ?", id)
		}
	}
	if userGroupIDQ != "" {
		if id, errParse := strconv.ParseUint(userGroupIDQ, 10, 64); errParse == nil {
			q = q.Where("user_group_id = ?", id)
		}
	}
	if isEnabledQ != "" {
		if isEnabledQ == "true" || isEnabledQ == "1" {
			q = q.Where("is_enabled = ?", true)
		} else if isEnabledQ == "false" || isEnabledQ == "0" {
			q = q.Where("is_enabled = ?", false)
		}
	}

	var rows []models.BillingRule
	if errFind := q.Order("created_at DESC").Find(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list billing rules failed"})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.formatRule(&row))
	}
	c.JSON(http.StatusOK, gin.H{"billing_rules": out})
}

// Get fetches a billing rule by ID.
func (h *BillingRuleHandler) Get(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var rule models.BillingRule
	if errFind := h.db.WithContext(c.Request.Context()).First(&rule, id).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, h.formatRule(&rule))
}

// updateBillingRuleRequest captures optional fields for billing rule updates.
type updateBillingRuleRequest struct {
	AuthGroupID           *uint64  `json:"auth_group_id"`            // Optional auth group ID.
	UserGroupID           *uint64  `json:"user_group_id"`            // Optional user group ID.
	Provider              *string  `json:"provider"`                 // Optional provider name.
	Model                 *string  `json:"model"`                    // Optional model name.
	BillingType           *int     `json:"billing_type"`             // Optional billing type.
	PricePerRequest       *float64 `json:"price_per_request"`        // Optional per-request price.
	PriceInputToken       *float64 `json:"price_input_token"`        // Optional input token price.
	PriceOutputToken      *float64 `json:"price_output_token"`       // Optional output token price.
	PriceCacheCreateToken *float64 `json:"price_cache_create_token"` // Optional cache create price.
	PriceCacheReadToken   *float64 `json:"price_cache_read_token"`   // Optional cache read price.
	IsEnabled             *bool    `json:"is_enabled"`               // Optional enabled flag.
}

// Update validates and applies billing rule changes.
func (h *BillingRuleHandler) Update(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body updateBillingRuleRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	var existing models.BillingRule
	if errFind := h.db.WithContext(c.Request.Context()).First(&existing, id).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	newAuthGroupID := existing.AuthGroupID
	if body.AuthGroupID != nil {
		if *body.AuthGroupID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_group_id cannot be 0"})
			return
		}
		newAuthGroupID = *body.AuthGroupID
	}

	newUserGroupID := existing.UserGroupID
	if body.UserGroupID != nil {
		if *body.UserGroupID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "user_group_id cannot be 0"})
			return
		}
		newUserGroupID = *body.UserGroupID
	}

	newProvider := existing.Provider
	if body.Provider != nil {
		value := strings.TrimSpace(*body.Provider)
		if value == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "provider cannot be empty"})
			return
		}
		newProvider = value
	}

	newModel := existing.Model
	if body.Model != nil {
		value := strings.TrimSpace(*body.Model)
		if value == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "model cannot be empty"})
			return
		}
		newModel = value
	}

	newBillingType := existing.BillingType
	if body.BillingType != nil {
		bt := models.BillingType(*body.BillingType)
		if bt != models.BillingTypePerRequest && bt != models.BillingTypePerToken {
			c.JSON(http.StatusBadRequest, gin.H{"error": "billing_type must be 1 (per_request) or 2 (per_token)"})
			return
		}
		newBillingType = bt
	}

	newPricePerRequest := existing.PricePerRequest
	if body.PricePerRequest != nil {
		newPricePerRequest = body.PricePerRequest
	}
	newPriceInputToken := existing.PriceInputToken
	if body.PriceInputToken != nil {
		newPriceInputToken = body.PriceInputToken
	}
	newPriceOutputToken := existing.PriceOutputToken
	if body.PriceOutputToken != nil {
		newPriceOutputToken = body.PriceOutputToken
	}
	newPriceCacheCreateToken := existing.PriceCacheCreateToken
	if body.PriceCacheCreateToken != nil {
		newPriceCacheCreateToken = body.PriceCacheCreateToken
	}
	newPriceCacheReadToken := existing.PriceCacheReadToken
	if body.PriceCacheReadToken != nil {
		newPriceCacheReadToken = body.PriceCacheReadToken
	}

	if newBillingType == models.BillingTypePerRequest {
		if newPricePerRequest == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_per_request is required for per_request billing"})
			return
		}
	} else {
		if newPriceInputToken == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_input_token is required for per_token billing"})
			return
		}
		if newPriceOutputToken == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_output_token is required for per_token billing"})
			return
		}
		if newPriceCacheCreateToken == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_cache_create_token is required for per_token billing"})
			return
		}
		if newPriceCacheReadToken == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "price_cache_read_token is required for per_token billing"})
			return
		}
	}

	now := time.Now().UTC()
	updates := map[string]any{
		"updated_at":               now,
		"auth_group_id":            newAuthGroupID,
		"user_group_id":            newUserGroupID,
		"provider":                 newProvider,
		"model":                    newModel,
		"billing_type":             newBillingType,
		"price_per_request":        newPricePerRequest,
		"price_input_token":        newPriceInputToken,
		"price_output_token":       newPriceOutputToken,
		"price_cache_create_token": newPriceCacheCreateToken,
		"price_cache_read_token":   newPriceCacheReadToken,
	}
	if body.IsEnabled != nil {
		updates["is_enabled"] = *body.IsEnabled
	}

	res := h.db.WithContext(c.Request.Context()).Model(&models.BillingRule{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes a billing rule by ID.
func (h *BillingRuleHandler) Delete(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	res := h.db.WithContext(c.Request.Context()).Delete(&models.BillingRule{}, id)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// setEnabledRequest captures the enabled flag for toggling a rule.
type setEnabledRequest struct {
	IsEnabled bool `json:"is_enabled"` // Desired enabled state.
}

// SetEnabled toggles the enabled state for a billing rule.
func (h *BillingRuleHandler) SetEnabled(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body setEnabledRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	now := time.Now().UTC()
	res := h.db.WithContext(c.Request.Context()).Model(&models.BillingRule{}).Where("id = ?", id).
		Updates(map[string]any{"is_enabled": body.IsEnabled, "updated_at": now})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// formatRule converts a billing rule into a response payload.
func (h *BillingRuleHandler) formatRule(rule *models.BillingRule) gin.H {
	return gin.H{
		"id":                       rule.ID,
		"auth_group_id":            rule.AuthGroupID,
		"user_group_id":            rule.UserGroupID,
		"provider":                 rule.Provider,
		"model":                    rule.Model,
		"billing_type":             rule.BillingType,
		"price_per_request":        rule.PricePerRequest,
		"price_input_token":        rule.PriceInputToken,
		"price_output_token":       rule.PriceOutputToken,
		"price_cache_create_token": rule.PriceCacheCreateToken,
		"price_cache_read_token":   rule.PriceCacheReadToken,
		"is_enabled":               rule.IsEnabled,
		"created_at":               rule.CreatedAt,
		"updated_at":               rule.UpdatedAt,
	}
}
