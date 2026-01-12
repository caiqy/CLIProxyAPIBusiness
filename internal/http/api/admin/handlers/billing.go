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

// BillingHandler handles billing summary endpoints.
type BillingHandler struct {
	db *gorm.DB
}

// NewBillingHandler constructs a BillingHandler.
func NewBillingHandler(db *gorm.DB) *BillingHandler {
	return &BillingHandler{db: db}
}

// Summary returns aggregated billing usage by API key.
func (h *BillingHandler) Summary(c *gin.Context) {
	var (
		apiKeyIDStr = strings.TrimSpace(c.Query("api_key_id"))
		fromStr     = strings.TrimSpace(c.Query("from"))
		toStr       = strings.TrimSpace(c.Query("to"))
	)

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

	// row holds aggregated billing data.
	type row struct {
		APIKeyID    *uint64 `json:"api_key_id"`
		TotalTokens int64   `json:"total_tokens"`
		CostMicros  int64   `json:"cost_micros"`
		Currency    string  `json:"currency"`
	}
	var rows []row
	if errScan := q.Select("api_key_id, currency, SUM(total_tokens) AS total_tokens, SUM(cost_micros) AS cost_micros").
		Group("api_key_id, currency").
		Order("cost_micros DESC").
		Scan(&rows).Error; errScan != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"billing": rows})
}
