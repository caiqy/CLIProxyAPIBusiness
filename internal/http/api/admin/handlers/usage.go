package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// UsageHandler handles admin usage listing endpoints.
type UsageHandler struct {
	db *gorm.DB
}

// NewUsageHandler constructs a UsageHandler.
func NewUsageHandler(db *gorm.DB) *UsageHandler {
	return &UsageHandler{db: db}
}

// List returns usage records with optional filters.
func (h *UsageHandler) List(c *gin.Context) {
	var (
		apiKeyIDStr = strings.TrimSpace(c.Query("api_key_id"))
		fromStr     = strings.TrimSpace(c.Query("from"))
		toStr       = strings.TrimSpace(c.Query("to"))
		limitStr    = strings.TrimSpace(c.Query("limit"))
	)

	limit := 100
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			if v > 1000 {
				v = 1000
			}
			limit = v
		}
	}

	q := h.db.WithContext(c.Request.Context()).Model(&models.Usage{})
	if apiKeyIDStr != "" {
		if id, errParseUint := strconv.ParseUint(apiKeyIDStr, 10, 64); errParseUint == nil {
			q = q.Where("api_key_id = ?", id)
		}
	}
	if fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			q = q.Where("requested_at >= ?", t.UTC())
		}
	}
	if toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			q = q.Where("requested_at <= ?", t.UTC())
		}
	}

	var rows []models.Usage
	if errFind := q.Order("requested_at DESC").Limit(limit).Find(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usage": rows})
}
