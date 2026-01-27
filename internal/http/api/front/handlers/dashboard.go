package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

func providerCredentialName(provider string, authLabel string, providerKeyLabel string) string {
	authLabel = strings.TrimSpace(authLabel)
	providerKeyLabel = strings.TrimSpace(providerKeyLabel)
	provider = strings.TrimSpace(provider)
	if authLabel != "" {
		return authLabel
	}
	if providerKeyLabel != "" {
		return providerKeyLabel
	}
	return provider
}

func authIndexFromAPIKey(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("api_key:" + apiKey))
	return hex.EncodeToString(sum[:8])
}

type providerAPIKeyEntry struct {
	APIKey string `json:"api_key"`
}

// DashboardHandler serves dashboard analytics endpoints.
type DashboardHandler struct {
	db *gorm.DB
}

// NewDashboardHandler constructs a DashboardHandler.
func NewDashboardHandler(db *gorm.DB) *DashboardHandler {
	return &DashboardHandler{db: db}
}

// kpiResponse defines the KPI response payload.
type kpiResponse struct {
	TotalRequests    int64   `json:"total_requests"`
	RequestsTrend    float64 `json:"requests_trend"`
	TodayTokens      int64   `json:"today_tokens"`
	TodayTokensTrend float64 `json:"today_tokens_trend"`
	AvgTokens        float64 `json:"avg_tokens"`
	AvgTokensTrend   float64 `json:"avg_tokens_trend"`
	SuccessRate      float64 `json:"success_rate"`
	SuccessRateTrend float64 `json:"success_rate_trend"`
	TodayCostMicros  int64   `json:"today_cost_micros"`
	TodayCostTrend   float64 `json:"today_cost_trend"`
	MtdCostMicros    int64   `json:"mtd_cost_micros"`
	CostTrend        float64 `json:"cost_trend"`
}

// KPI returns key performance indicators for the dashboard.
func (h *DashboardHandler) KPI(c *gin.Context) {
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
		c.JSON(http.StatusOK, kpiResponse{SuccessRate: 100.0})
		return
	}

	loc := time.Local
	now := time.Now().In(loc)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := today.AddDate(0, 0, -1)

	var todayStats struct {
		Total       int64
		Failed      int64
		TotalTokens int64
		CostMicros  int64
	}
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("api_key_id IN ? AND requested_at >= ?", apiKeyIDs, today).
		Select("COUNT(*) AS total, SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(cost_micros), 0) AS cost_micros").
		Scan(&todayStats)

	var yesterdayStats struct {
		Total       int64
		Failed      int64
		TotalTokens int64
		CostMicros  int64
	}
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("api_key_id IN ? AND requested_at >= ? AND requested_at < ?", apiKeyIDs, yesterday, today).
		Select("COUNT(*) AS total, SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(cost_micros), 0) AS cost_micros").
		Scan(&yesterdayStats)

	var mtdCost int64
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("api_key_id IN ? AND requested_at >= ?", apiKeyIDs, monthStart).
		Select("COALESCE(SUM(cost_micros), 0)").
		Scan(&mtdCost)

	lastMonthStart := monthStart.AddDate(0, -1, 0)
	lastMonthSameDay := lastMonthStart.AddDate(0, 0, now.Day()-1)
	var lastMtdCost int64
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("api_key_id IN ? AND requested_at >= ? AND requested_at < ?", apiKeyIDs, lastMonthStart, lastMonthSameDay).
		Select("COALESCE(SUM(cost_micros), 0)").
		Scan(&lastMtdCost)

	requestsTrend := calcTrend(float64(yesterdayStats.Total), float64(todayStats.Total))
	todayTokensTrend := calcTrend(float64(yesterdayStats.TotalTokens), float64(todayStats.TotalTokens))
	successRate := 100.0
	if todayStats.Total > 0 {
		successRate = float64(todayStats.Total-todayStats.Failed) / float64(todayStats.Total) * 100
	}
	yesterdaySuccessRate := 100.0
	if yesterdayStats.Total > 0 {
		yesterdaySuccessRate = float64(yesterdayStats.Total-yesterdayStats.Failed) / float64(yesterdayStats.Total) * 100
	}
	successRateTrend := successRate - yesterdaySuccessRate
	todayCostTrend := calcTrend(float64(yesterdayStats.CostMicros), float64(todayStats.CostMicros))
	costTrend := calcTrend(float64(lastMtdCost), float64(mtdCost))

	avgTokens := 0.0
	if todayStats.Total > 0 {
		avgTokens = float64(todayStats.TotalTokens) / float64(todayStats.Total)
	}
	yesterdayAvgTokens := 0.0
	if yesterdayStats.Total > 0 {
		yesterdayAvgTokens = float64(yesterdayStats.TotalTokens) / float64(yesterdayStats.Total)
	}
	avgTokensTrend := calcTrend(yesterdayAvgTokens, avgTokens)

	c.JSON(http.StatusOK, kpiResponse{
		TotalRequests:    todayStats.Total,
		RequestsTrend:    requestsTrend,
		TodayTokens:      todayStats.TotalTokens,
		TodayTokensTrend: todayTokensTrend,
		AvgTokens:        avgTokens,
		AvgTokensTrend:   avgTokensTrend,
		SuccessRate:      successRate,
		SuccessRateTrend: successRateTrend,
		TodayCostMicros:  todayStats.CostMicros,
		TodayCostTrend:   todayCostTrend,
		MtdCostMicros:    mtdCost,
		CostTrend:        costTrend,
	})
}

// trafficPoint defines a time-series point for traffic charts.
type trafficPoint struct {
	Time     string `json:"time"`
	Requests int64  `json:"requests"`
	Errors   int64  `json:"errors"`
}

// Traffic returns hourly traffic statistics for the current day.
func (h *DashboardHandler) Traffic(c *gin.Context) {
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

	loc := time.Local
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	points := make([]trafficPoint, 24)
	for i := 0; i < 24; i++ {
		hourStart := today.Add(time.Duration(i) * time.Hour)
		hourEnd := hourStart.Add(time.Hour)

		var count int64
		var errCount int64
		if len(apiKeyIDs) > 0 {
			h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
				Where("api_key_id IN ? AND requested_at >= ? AND requested_at < ?", apiKeyIDs, hourStart, hourEnd).
				Count(&count)
			h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
				Where("api_key_id IN ? AND requested_at >= ? AND requested_at < ? AND failed = true", apiKeyIDs, hourStart, hourEnd).
				Count(&errCount)
		}
		points[i] = trafficPoint{
			Time:     hourStart.Format("15:04"),
			Requests: count,
			Errors:   errCount,
		}
	}

	c.JSON(http.StatusOK, gin.H{"points": points})
}

// costItem defines the cost breakdown entry.
type costItem struct {
	Model      string  `json:"model"`
	CostMicros int64   `json:"cost_micros"`
	Percentage float64 `json:"percentage"`
}

// CostDistribution returns cost breakdown by model.
func (h *DashboardHandler) CostDistribution(c *gin.Context) {
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
		c.JSON(http.StatusOK, gin.H{"items": []costItem{}})
		return
	}

	loc := time.Local
	now := time.Now().In(loc)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)

	// modelCost holds aggregate cost per model.
	type modelCost struct {
		Model      string
		CostMicros int64
	}
	var results []modelCost
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("api_key_id IN ? AND requested_at >= ?", apiKeyIDs, monthStart).
		Select("model, COALESCE(SUM(cost_micros), 0) AS cost_micros").
		Group("model").
		Order("cost_micros DESC").
		Scan(&results)

	var totalCost int64
	for _, r := range results {
		totalCost += r.CostMicros
	}

	items := make([]costItem, 0, len(results))
	for _, r := range results {
		pct := 0.0
		if totalCost > 0 {
			pct = float64(r.CostMicros) / float64(totalCost) * 100
		}
		items = append(items, costItem{
			Model:      r.Model,
			CostMicros: r.CostMicros,
			Percentage: pct,
		})
	}

	c.JSON(http.StatusOK, gin.H{"items": items})
}

// healthItem defines a model health status item.
type healthItem struct {
	Provider string `json:"provider"`
	Status   string `json:"status"`
	Latency  string `json:"latency"`
}

// ModelHealth returns placeholder model health status.
func (h *DashboardHandler) ModelHealth(c *gin.Context) {
	items := []healthItem{
		{Provider: "OpenAI API", Status: "healthy", Latency: "12ms"},
		{Provider: "Anthropic API", Status: "healthy", Latency: "45ms"},
		{Provider: "Local LLM Cluster", Status: "degraded", Latency: "Degraded"},
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// transactionItem defines a recent transaction entry.
type transactionItem struct {
	Status        string `json:"status"`
	StatusType    string `json:"status_type"`
	Timestamp     string `json:"timestamp"`
	Provider      string `json:"provider"` // Provider credential display name.
	Model         string `json:"model"`
	RequestTimeMs int64  `json:"request_time_ms"`
	InputTokens   int64  `json:"input_tokens"`
	CachedTokens  int64  `json:"cached_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	CostMicros    int64  `json:"cost_micros"`
}

// RecentTransactions returns recent usage records as transactions.
func (h *DashboardHandler) RecentTransactions(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	page := 1
	if v := strings.TrimSpace(c.Query("page")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	pageSize := 15
	if v := strings.TrimSpace(c.Query("page_size")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	var apiKeyIDs []uint64
	if errFind := h.db.WithContext(c.Request.Context()).Model(&models.APIKey{}).
		Where("user_id = ?", userID).
		Pluck("id", &apiKeyIDs).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query api keys failed"})
		return
	}

	if len(apiKeyIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"transactions": []transactionItem{},
			"total":        int64(0),
			"page":         page,
			"page_size":    pageSize,
		})
		return
	}

	base := h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("api_key_id IN ?", apiKeyIDs)

	var total int64
	if errCount := base.Session(&gorm.Session{}).Count(&total).Error; errCount != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query usages failed"})
		return
	}

	var usages []models.Usage
	if errFind := base.Session(&gorm.Session{}).
		Order("requested_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&usages).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query usages failed"})
		return
	}

	authIDsSet := make(map[uint64]struct{})
	providersSet := make(map[string]struct{})
	authIndexesSet := make(map[string]struct{})
	authKeysSet := make(map[string]struct{})
	for _, u := range usages {
		if u.AuthID != nil {
			authIDsSet[*u.AuthID] = struct{}{}
			continue
		}
		provider := strings.TrimSpace(u.Provider)
		if provider != "" {
			providersSet[provider] = struct{}{}
		}
		if idx := strings.TrimSpace(u.AuthIndex); idx != "" {
			authIndexesSet[idx] = struct{}{}
		}
		if key := strings.TrimSpace(u.AuthKey); key != "" {
			authKeysSet[key] = struct{}{}
		}
	}

	authIDToLabel := make(map[uint64]string)
	if len(authIDsSet) > 0 {
		authIDs := make([]uint64, 0, len(authIDsSet))
		for id := range authIDsSet {
			authIDs = append(authIDs, id)
		}
		var authRows []models.Auth
		_ = h.db.WithContext(c.Request.Context()).
			Model(&models.Auth{}).
			Select("id", "name").
			Where("id IN ?", authIDs).
			Find(&authRows).Error
		for _, a := range authRows {
			label := strings.TrimSpace(a.Name)
			// Never fall back to a.Key here: it can be sensitive.
			authIDToLabel[a.ID] = label
		}
	}

	providerKeyNameByAuthIndex := make(map[string]string)
	providerKeyPriorityByAuthIndex := make(map[string]int)

	// providerKeyNameByAuthKey is kept for legacy usage rows where AuthKey stores
	// the raw provider API key (older versions).
	providerKeyNameByAuthKey := make(map[string]string)
	providerKeyPriorityByAuthKey := make(map[string]int)

	if len(providersSet) > 0 && (len(authIndexesSet) > 0 || len(authKeysSet) > 0) {
		providers := make([]string, 0, len(providersSet))
		for p := range providersSet {
			if p != "" {
				providers = append(providers, p)
			}
		}
		var providerRows []models.ProviderAPIKey
		if len(providers) > 0 {
			_ = h.db.WithContext(c.Request.Context()).
				Model(&models.ProviderAPIKey{}).
				Select("provider", "name", "priority", "api_key", "api_key_entries").
				Where("provider IN ?", providers).
				Find(&providerRows).Error
		}
		for _, row := range providerRows {
			name := strings.TrimSpace(row.Name)
			// Do not fall back to API key values. If name is empty, leave it empty and let caller fall back to provider.

			update := func(key string) {
				key = strings.TrimSpace(key)
				if key == "" {
					return
				}

				// Preferred path: match by AuthIndex (hashed from api_key).
				if len(authIndexesSet) > 0 {
					idx := authIndexFromAPIKey(key)
					if idx != "" {
						if _, ok := authIndexesSet[idx]; ok {
							currentPriority, exists := providerKeyPriorityByAuthIndex[idx]
							if !exists || row.Priority > currentPriority {
								providerKeyPriorityByAuthIndex[idx] = row.Priority
								providerKeyNameByAuthIndex[idx] = name
							}
						}
					}
				}

				// Legacy fallback: match by raw key when present in usage rows.
				if len(authKeysSet) > 0 {
					if _, ok := authKeysSet[key]; ok {
						currentPriority, exists := providerKeyPriorityByAuthKey[key]
						if !exists || row.Priority > currentPriority {
							providerKeyPriorityByAuthKey[key] = row.Priority
							providerKeyNameByAuthKey[key] = name
						}
					}
				}
			}

			update(row.APIKey)
			if len(row.APIKeyEntries) > 0 {
				var entries []providerAPIKeyEntry
				if err := json.Unmarshal(row.APIKeyEntries, &entries); err == nil {
					for _, e := range entries {
						update(e.APIKey)
					}
				}
			}
		}
	}

	transactions := make([]transactionItem, 0, len(usages))
	for _, u := range usages {
		authLabel := ""
		if u.AuthID != nil {
			authLabel = authIDToLabel[*u.AuthID]
		}
		providerKeyLabel := providerKeyNameByAuthIndex[strings.TrimSpace(u.AuthIndex)]
		if providerKeyLabel == "" {
			providerKeyLabel = providerKeyNameByAuthKey[strings.TrimSpace(u.AuthKey)]
		}
		providerLabel := providerCredentialName(u.Provider, authLabel, providerKeyLabel)

		status := "200 OK"
		statusType := "success"
		if u.Failed {
			status = "Error"
			statusType = "error"
		}

		requestTimeMs := int64(0)
		if !u.CreatedAt.IsZero() && u.CreatedAt.After(u.RequestedAt) {
			requestTimeMs = u.CreatedAt.Sub(u.RequestedAt).Milliseconds()
		}
		transactions = append(transactions, transactionItem{
			Status:        status,
			StatusType:    statusType,
			Timestamp:     u.RequestedAt.In(time.Local).Format("2006-01-02 15:04:05"),
			Provider:      providerLabel,
			Model:         u.Model,
			RequestTimeMs: requestTimeMs,
			InputTokens:   u.InputTokens,
			CachedTokens:  u.CachedTokens,
			OutputTokens:  u.OutputTokens,
			CostMicros:    u.CostMicros,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"transactions": transactions,
		"total":        total,
		"page":         page,
		"page_size":    pageSize,
	})
}

// calcTrend computes percentage change between two values.
func calcTrend(prev, current float64) float64 {
	if prev == 0 {
		if current > 0 {
			return 100.0
		}
		return 0.0
	}
	return (current - prev) / prev * 100
}
