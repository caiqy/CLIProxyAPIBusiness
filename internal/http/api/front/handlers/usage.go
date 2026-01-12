package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// UsageHandler handles usage statistics endpoints.
type UsageHandler struct {
	db *gorm.DB
}

// NewUsageHandler constructs a UsageHandler.
func NewUsageHandler(db *gorm.DB) *UsageHandler {
	return &UsageHandler{db: db}
}

// usageSummary aggregates usage statistics.
type usageSummary struct {
	TotalRequests int64 `json:"total_requests"`
	TotalTokens   int64 `json:"total_tokens"`
	CostMicros    int64 `json:"cost_micros"`
}

// Stats returns usage summaries for recent time windows.
func (h *UsageHandler) Stats(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var apiKeyIDs []uint64
	if errFind := h.db.WithContext(c.Request.Context()).Model(&models.APIKey{}).
		Where("user_id = ?", userID).
		Pluck("id", &apiKeyIDs).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query api keys failed"})
		return
	}

	if len(apiKeyIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"today":   usageSummary{},
			"3_days":  usageSummary{},
			"7_days":  usageSummary{},
			"15_days": usageSummary{},
			"30_days": usageSummary{},
		})
		return
	}

	loc := time.Local
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	periods := map[string]time.Time{
		"today":   today,
		"3_days":  today.AddDate(0, 0, -2),
		"7_days":  today.AddDate(0, 0, -6),
		"15_days": today.AddDate(0, 0, -14),
		"30_days": today.AddDate(0, 0, -29),
	}

	result := make(map[string]usageSummary)
	for name, since := range periods {
		var summary usageSummary
		if errScan := h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
			Where("api_key_id IN ? AND requested_at >= ?", apiKeyIDs, since).
			Select("COUNT(*) AS total_requests, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(cost_micros), 0) AS cost_micros").
			Scan(&summary).Error; errScan != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query usage failed"})
			return
		}
		result[name] = summary
	}

	c.JSON(http.StatusOK, result)
}
