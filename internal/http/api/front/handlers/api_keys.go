package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/security"
	"gorm.io/gorm"
)

// APIKeyHandler handles API key endpoints for front users.
type APIKeyHandler struct {
	db *gorm.DB
}

// NewAPIKeyHandler constructs an APIKeyHandler.
func NewAPIKeyHandler(db *gorm.DB) *APIKeyHandler {
	return &APIKeyHandler{db: db}
}

// listAPIKeysQuery defines query parameters for listing API keys.
type listAPIKeysQuery struct {
	Page   int    `form:"page,default=1"`
	Limit  int    `form:"limit,default=20"`
	Search string `form:"search"`
	Status string `form:"status"`
}

// List returns a paginated list of API keys.
func (h *APIKeyHandler) List(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var q listAPIKeysQuery
	if errBind := c.ShouldBindQuery(&q); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid query"})
		return
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 20
	}

	query := h.db.WithContext(c.Request.Context()).Model(&models.APIKey{}).Where("user_id = ?", userID)

	if q.Search != "" {
		search := "%" + strings.ToLower(q.Search) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(api_key) LIKE ?", search, search)
	}

	now := time.Now()
	sevenDaysLater := now.AddDate(0, 0, 7)
	switch q.Status {
	case "active":
		query = query.Where("active = true AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", sevenDaysLater)
	case "expiring":
		query = query.Where("active = true AND revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= ? AND expires_at > ?", sevenDaysLater, now)
	case "revoked":
		query = query.Where("revoked_at IS NOT NULL")
	}

	var total int64
	if errCount := query.Count(&total).Error; errCount != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "count failed"})
		return
	}

	var rows []models.APIKey
	offset := (q.Page - 1) * q.Limit
	if errFind := query.Order("created_at DESC").Offset(offset).Limit(q.Limit).Find(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list api keys failed"})
		return
	}

	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.serializeAPIKey(&row))
	}

	c.JSON(http.StatusOK, gin.H{
		"api_keys": out,
		"total":    total,
		"page":     q.Page,
		"limit":    q.Limit,
	})
}

// serializeAPIKey converts a model to an API response payload.
func (h *APIKeyHandler) serializeAPIKey(row *models.APIKey) gin.H {
	prefix := ""
	if len(row.APIKey) >= 8 {
		prefix = row.APIKey[:8] + "········" + row.APIKey[len(row.APIKey)-4:]
	}
	return gin.H{
		"id":           row.ID,
		"name":         row.Name,
		"key":          row.APIKey,
		"key_prefix":   prefix,
		"active":       row.Active,
		"status":       row.Status(),
		"expires_at":   row.ExpiresAt,
		"revoked_at":   row.RevokedAt,
		"last_used_at": row.LastUsedAt,
		"created_at":   row.CreatedAt,
		"updated_at":   row.UpdatedAt,
	}
}

// Stats returns aggregate API key statistics for the user.
func (h *APIKeyHandler) Stats(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx := c.Request.Context()
	now := time.Now()
	sevenDaysLater := now.AddDate(0, 0, 7)
	thirtyDaysAgo := now.AddDate(0, 0, -30)

	var totalKeys int64
	if errTotal := h.db.WithContext(ctx).Model(&models.APIKey{}).Where("user_id = ?", userID).Count(&totalKeys).Error; errTotal != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "count total failed"})
		return
	}

	var activeKeys int64
	if errActive := h.db.WithContext(ctx).Model(&models.APIKey{}).
		Where("user_id = ? AND active = true AND revoked_at IS NULL", userID).
		Count(&activeKeys).Error; errActive != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "count active failed"})
		return
	}

	var expiringKeys int64
	if errExpiring := h.db.WithContext(ctx).Model(&models.APIKey{}).
		Where("user_id = ? AND active = true AND revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= ? AND expires_at > ?", userID, sevenDaysLater, now).
		Count(&expiringKeys).Error; errExpiring != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "count expiring failed"})
		return
	}

	var apiKeyIDs []uint64
	if errIDs := h.db.WithContext(ctx).Model(&models.APIKey{}).
		Where("user_id = ?", userID).
		Pluck("id", &apiKeyIDs).Error; errIDs != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get api key ids failed"})
		return
	}

	var totalTokens int64
	if len(apiKeyIDs) > 0 {
		if errTokens := h.db.WithContext(ctx).Model(&models.Usage{}).
			Where("api_key_id IN ? AND requested_at >= ?", apiKeyIDs, thirtyDaysAgo).
			Select("COALESCE(SUM(total_tokens), 0)").Scan(&totalTokens).Error; errTokens != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "sum tokens failed"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_keys":              totalKeys,
		"active_keys":             activeKeys,
		"expiring_keys":           expiringKeys,
		"total_usage_30d":         totalTokens,
		"total_usage_30d_display": formatTokens(totalTokens),
	})
}

// formatTokens formats token counts into human-readable units.
func formatTokens(tokens int64) string {
	if tokens >= 1_000_000_000 {
		return fmt.Sprintf("%.1fB", float64(tokens)/1_000_000_000)
	}
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}

// createAPIKeyRequest defines the request body for creating keys.
type createAPIKeyRequest struct {
	Name      string `json:"name"`
	ExpiresIn *int   `json:"expires_in_days"`
}

// Create creates a new API key for the user.
func (h *APIKeyHandler) Create(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var body createAPIKeyRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing name"})
		return
	}

	token, errGenerate := security.GenerateAPIKey()
	if errGenerate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "generate api key failed"})
		return
	}

	now := time.Now().UTC()
	var expiresAt *time.Time
	if body.ExpiresIn != nil && *body.ExpiresIn > 0 {
		exp := now.AddDate(0, 0, *body.ExpiresIn)
		expiresAt = &exp
	}

	row := models.APIKey{
		UserID:    &userID,
		Name:      name,
		APIKey:    token,
		IsAdmin:   false,
		Active:    true,
		ExpiresAt: expiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if errCreate := h.db.WithContext(c.Request.Context()).Create(&row).Error; errCreate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create api key failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":    row.ID,
		"name":  row.Name,
		"token": token,
	})
}

// updateAPIKeyRequest defines the request body for updating keys.
type updateAPIKeyRequest struct {
	Name      *string `json:"name"`
	ExpiresIn *int    `json:"expires_in_days"`
}

// Update updates an API key's metadata or expiry.
func (h *APIKeyHandler) Update(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var body updateAPIKeyRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	updates := map[string]any{"updated_at": time.Now().UTC()}
	if body.Name != nil {
		updates["name"] = strings.TrimSpace(*body.Name)
	}
	if body.ExpiresIn != nil {
		if *body.ExpiresIn <= 0 {
			updates["expires_at"] = nil
		} else {
			exp := time.Now().UTC().AddDate(0, 0, *body.ExpiresIn)
			updates["expires_at"] = &exp
		}
	}

	res := h.db.WithContext(c.Request.Context()).Model(&models.APIKey{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, userID).
		Updates(updates)
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

// Revoke revokes an API key and marks it inactive.
func (h *APIKeyHandler) Revoke(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	now := time.Now().UTC()
	res := h.db.Debug().WithContext(c.Request.Context()).Model(&models.APIKey{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, userID).
		Updates(map[string]any{
			"active":     false,
			"revoked_at": &now,
			"updated_at": now,
		})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "revoke failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// Delete removes a revoked API key permanently.
func (h *APIKeyHandler) Delete(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	res := h.db.WithContext(c.Request.Context()).
		Where("id = ? AND user_id = ? AND revoked_at IS NOT NULL", id, userID).
		Delete(&models.APIKey{})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found or not revoked"})
		return
	}
	c.Status(http.StatusNoContent)
}

// Renew extends the expiration of an API key.
func (h *APIKeyHandler) Renew(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	// body holds the renew request payload.
	var body struct {
		Days int `json:"days"`
	}
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		body.Days = 90
	}
	if body.Days <= 0 {
		body.Days = 90
	}

	now := time.Now().UTC()
	newExpiry := now.AddDate(0, 0, body.Days)

	res := h.db.WithContext(c.Request.Context()).Model(&models.APIKey{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, userID).
		Updates(map[string]any{
			"expires_at": &newExpiry,
			"updated_at": now,
		})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "renew failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"expires_at": newExpiry,
	})
}

// Regenerate replaces the API key token value.
func (h *APIKeyHandler) Regenerate(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	token, errGenerate := security.GenerateAPIKey()
	if errGenerate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "generate api key failed"})
		return
	}

	now := time.Now().UTC()
	res := h.db.WithContext(c.Request.Context()).Model(&models.APIKey{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, userID).
		Updates(map[string]any{
			"api_key":    token,
			"updated_at": now,
		})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "regenerate failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token": token,
	})
}
