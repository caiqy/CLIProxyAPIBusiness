package handlers

import (
	"encoding/json"
	"math"
	"net/http"
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

// DashboardHandler serves admin dashboard analytics endpoints.
type DashboardHandler struct {
	db *gorm.DB // Database handle for usage analytics.
}

// NewDashboardHandler constructs a dashboard handler with database access.
func NewDashboardHandler(db *gorm.DB) *DashboardHandler {
	return &DashboardHandler{db: db}
}

// kpiResponse defines the KPI response payload.
type kpiResponse struct {
	TotalRequests    int64   `json:"total_requests"`      // Total requests today.
	RequestsTrend    float64 `json:"requests_trend"`      // Trend vs yesterday.
	AvgRequestTimeMs int64   `json:"avg_request_time_ms"` // Average request time in ms.
	RequestTimeTrend float64 `json:"request_time_trend"`  // Trend vs yesterday.
	SuccessRate      float64 `json:"success_rate"`        // Success rate percentage.
	SuccessRateTrend float64 `json:"success_rate_trend"`  // Trend vs yesterday.
	MtdCostMicros    int64   `json:"mtd_cost_micros"`     // Month-to-date cost in micros.
	CostTrend        float64 `json:"cost_trend"`          // Trend vs last month.
}

// KPI returns global KPI data for all users
func (h *DashboardHandler) KPI(c *gin.Context) {
	loc := time.Local
	now := time.Now().In(loc)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := today.AddDate(0, 0, -1)

	var todayStats struct {
		Total            int64
		Failed           int64
		AvgRequestTimeMs float64
	}
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("requested_at >= ?", today).
		Select(`
			COUNT(*) AS total,
			SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed,
			COALESCE(AVG(GREATEST(EXTRACT(EPOCH FROM (created_at - requested_at)) * 1000, 0)), 0) AS avg_request_time_ms
		`).
		Scan(&todayStats)

	var yesterdayStats struct {
		Total            int64
		Failed           int64
		AvgRequestTimeMs float64
	}
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("requested_at >= ? AND requested_at < ?", yesterday, today).
		Select(`
			COUNT(*) AS total,
			SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed,
			COALESCE(AVG(GREATEST(EXTRACT(EPOCH FROM (created_at - requested_at)) * 1000, 0)), 0) AS avg_request_time_ms
		`).
		Scan(&yesterdayStats)

	var mtdCost int64
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("requested_at >= ?", monthStart).
		Select("COALESCE(SUM(cost_micros), 0)").
		Scan(&mtdCost)

	lastMonthStart := monthStart.AddDate(0, -1, 0)
	lastMonthSameDay := lastMonthStart.AddDate(0, 0, now.Day()-1)
	var lastMtdCost int64
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("requested_at >= ? AND requested_at < ?", lastMonthStart, lastMonthSameDay).
		Select("COALESCE(SUM(cost_micros), 0)").
		Scan(&lastMtdCost)

	requestsTrend := calcTrend(float64(yesterdayStats.Total), float64(todayStats.Total))
	successRate := 100.0
	if todayStats.Total > 0 {
		successRate = float64(todayStats.Total-todayStats.Failed) / float64(todayStats.Total) * 100
	}
	yesterdaySuccessRate := 100.0
	if yesterdayStats.Total > 0 {
		yesterdaySuccessRate = float64(yesterdayStats.Total-yesterdayStats.Failed) / float64(yesterdayStats.Total) * 100
	}
	successRateTrend := successRate - yesterdaySuccessRate
	avgRequestTimeToday := int64(math.Round(todayStats.AvgRequestTimeMs))
	avgRequestTimeYesterday := int64(math.Round(yesterdayStats.AvgRequestTimeMs))
	requestTimeTrend := calcTrend(float64(avgRequestTimeYesterday), float64(avgRequestTimeToday))
	costTrend := calcTrend(float64(lastMtdCost), float64(mtdCost))

	c.JSON(http.StatusOK, kpiResponse{
		TotalRequests:    todayStats.Total,
		RequestsTrend:    requestsTrend,
		AvgRequestTimeMs: avgRequestTimeToday,
		RequestTimeTrend: requestTimeTrend,
		SuccessRate:      successRate,
		SuccessRateTrend: successRateTrend,
		MtdCostMicros:    mtdCost,
		CostTrend:        costTrend,
	})
}

// trafficPoint represents hourly traffic metrics.
type trafficPoint struct {
	Time     string `json:"time"`     // Hour label.
	Requests int64  `json:"requests"` // Request count.
	Errors   int64  `json:"errors"`   // Failed request count.
}

// Traffic returns global traffic data (hourly requests for 24 hours)
func (h *DashboardHandler) Traffic(c *gin.Context) {
	loc := time.Local
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	points := make([]trafficPoint, 24)
	for i := 0; i < 24; i++ {
		hourStart := today.Add(time.Duration(i) * time.Hour)
		hourEnd := hourStart.Add(time.Hour)

		var count int64
		var errCount int64
		h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
			Where("requested_at >= ? AND requested_at < ?", hourStart, hourEnd).
			Count(&count)
		h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
			Where("requested_at >= ? AND requested_at < ? AND failed = true", hourStart, hourEnd).
			Count(&errCount)

		points[i] = trafficPoint{
			Time:     hourStart.Format("15:04"),
			Requests: count,
			Errors:   errCount,
		}
	}

	c.JSON(http.StatusOK, gin.H{"points": points})
}

// costItem represents cost distribution for a model.
type costItem struct {
	Model      string  `json:"model"`       // Model identifier.
	CostMicros int64   `json:"cost_micros"` // Cost in micros.
	Percentage float64 `json:"percentage"`  // Share of total cost.
}

// CostDistribution returns global cost distribution grouped by model
func (h *DashboardHandler) CostDistribution(c *gin.Context) {
	loc := time.Local
	now := time.Now().In(loc)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)

	// modelCost captures aggregated costs per model.
	type modelCost struct {
		Model      string // Model identifier.
		CostMicros int64  // Aggregated cost in micros.
	}
	var results []modelCost
	h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("requested_at >= ?", monthStart).
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

// healthItem represents a provider health status entry.
type healthItem struct {
	Provider string `json:"provider"` // Provider display name.
	Status   string `json:"status"`   // Health status label.
	Latency  string `json:"latency"`  // Observed latency label.
}

// ModelHealth returns model health status
func (h *DashboardHandler) ModelHealth(c *gin.Context) {
	items := []healthItem{
		{Provider: "OpenAI API", Status: "healthy", Latency: "12ms"},
		{Provider: "Anthropic API", Status: "healthy", Latency: "45ms"},
		{Provider: "Local LLM Cluster", Status: "degraded", Latency: "Degraded"},
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// transactionItem represents a recent usage record for the dashboard.
type transactionItem struct {
	Username      string `json:"username"`        // Caller username.
	Status        string `json:"status"`          // HTTP-like status label.
	StatusType    string `json:"status_type"`     // UI status type.
	Timestamp     string `json:"timestamp"`       // Local timestamp string.
	Provider      string `json:"provider"`        // Provider credential display name.
	Model         string `json:"model"`           // Model identifier.
	RequestTimeMs int64  `json:"request_time_ms"` // Request duration in milliseconds.
	InputTokens   int64  `json:"input_tokens"`    // Input token count.
	CachedTokens  int64  `json:"cached_tokens"`   // Cached token count.
	OutputTokens  int64  `json:"output_tokens"`   // Output token count.
	CostMicros    int64  `json:"cost_micros"`     // Cost in micros.
}

// RecentTransactions returns recent transactions for all users
func (h *DashboardHandler) RecentTransactions(c *gin.Context) {
	var usages []models.Usage
	if errFind := h.db.WithContext(c.Request.Context()).
		Order("requested_at DESC").
		Limit(20).
		Find(&usages).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query usages failed"})
		return
	}

	authIDsSet := make(map[uint64]struct{})

	userIDsSet := make(map[uint64]struct{})
	apiKeyIDsSet := make(map[uint64]struct{})
	for _, u := range usages {
		if u.AuthID != nil {
			authIDsSet[*u.AuthID] = struct{}{}
		}
		if u.UserID != nil {
			userIDsSet[*u.UserID] = struct{}{}
			continue
		}
		if u.APIKeyID != nil {
			apiKeyIDsSet[*u.APIKeyID] = struct{}{}
		}
	}

	authIDToLabel := make(map[uint64]string)
	if len(authIDsSet) > 0 {
		authIDs := make([]uint64, 0, len(authIDsSet))
		for id := range authIDsSet {
			authIDs = append(authIDs, id)
		}
		var authRows []models.Auth
		if errAuths := h.db.WithContext(c.Request.Context()).
			Model(&models.Auth{}).
			Select("id", "name").
			Where("id IN ?", authIDs).
			Find(&authRows).Error; errAuths == nil {
			for _, a := range authRows {
				label := strings.TrimSpace(a.Name)
				// Never fall back to a.Key here: it can be sensitive.
				authIDToLabel[a.ID] = label
			}
		}
	}

	providerKeyNameByAuthKey := make(map[string]string)
	providerKeyPriorityByAuthKey := make(map[string]int)
	providersSet := make(map[string]struct{})
	authKeysSet := make(map[string]struct{})
	for _, u := range usages {
		if u.AuthID != nil {
			continue
		}
		key := strings.TrimSpace(u.AuthKey)
		if key == "" {
			continue
		}
		providersSet[strings.TrimSpace(u.Provider)] = struct{}{}
		authKeysSet[key] = struct{}{}
	}
	if len(providersSet) > 0 && len(authKeysSet) > 0 {
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
				Select("provider", "name", "api_key", "api_key_entries").
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
				if _, ok := authKeysSet[key]; !ok {
					return
				}
				currentPriority, exists := providerKeyPriorityByAuthKey[key]
				if !exists || row.Priority > currentPriority {
					providerKeyPriorityByAuthKey[key] = row.Priority
					providerKeyNameByAuthKey[key] = name
				}
			}

			update(row.APIKey)
			if len(row.APIKeyEntries) > 0 {
				var entries []apiKeyEntry
				if err := json.Unmarshal(row.APIKeyEntries, &entries); err == nil {
					for _, e := range entries {
						update(e.APIKey)
					}
				}
			}
		}
	}

	apiKeyToUserID := make(map[uint64]uint64)
	if len(apiKeyIDsSet) > 0 {
		apiKeyIDs := make([]uint64, 0, len(apiKeyIDsSet))
		for id := range apiKeyIDsSet {
			apiKeyIDs = append(apiKeyIDs, id)
		}
		var apiKeys []models.APIKey
		if errAPIKeys := h.db.WithContext(c.Request.Context()).
			Model(&models.APIKey{}).
			Select("id", "user_id").
			Where("id IN ?", apiKeyIDs).
			Find(&apiKeys).Error; errAPIKeys == nil {
			for _, k := range apiKeys {
				if k.UserID != nil {
					apiKeyToUserID[k.ID] = *k.UserID
					userIDsSet[*k.UserID] = struct{}{}
				}
			}
		}
	}

	userIDToUsername := make(map[uint64]string)
	if len(userIDsSet) > 0 {
		userIDs := make([]uint64, 0, len(userIDsSet))
		for id := range userIDsSet {
			userIDs = append(userIDs, id)
		}
		var users []models.User
		if errUsers := h.db.WithContext(c.Request.Context()).
			Model(&models.User{}).
			Select("id", "username").
			Where("id IN ?", userIDs).
			Find(&users).Error; errUsers == nil {
			for _, u := range users {
				userIDToUsername[u.ID] = u.Username
			}
		}
	}

	transactions := make([]transactionItem, 0, len(usages))
	for _, u := range usages {
		username := ""
		if u.UserID != nil {
			username = userIDToUsername[*u.UserID]
		} else if u.APIKeyID != nil {
			if uid, ok := apiKeyToUserID[*u.APIKeyID]; ok {
				username = userIDToUsername[uid]
			}
		}

		authLabel := ""
		if u.AuthID != nil {
			authLabel = authIDToLabel[*u.AuthID]
		}
		providerKeyLabel := providerKeyNameByAuthKey[strings.TrimSpace(u.AuthKey)]
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
			Username:      username,
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

	c.JSON(http.StatusOK, gin.H{"transactions": transactions})
}

// calcTrend computes percentage change from a previous value.
func calcTrend(prev, current float64) float64 {
	if prev == 0 {
		if current > 0 {
			return 100.0
		}
		return 0.0
	}
	return (current - prev) / prev * 100
}
