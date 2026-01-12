package handlers

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// LogsHandler handles usage log endpoints.
type LogsHandler struct {
	db *gorm.DB
}

// NewLogsHandler constructs a LogsHandler.
func NewLogsHandler(db *gorm.DB) *LogsHandler {
	return &LogsHandler{db: db}
}

// logsListQuery defines query parameters for listing logs.
type logsListQuery struct {
	Page      int    `form:"page,default=1"`
	Limit     int    `form:"limit,default=20"`
	StartDate string `form:"start_date"`
	EndDate   string `form:"end_date"`
	Project   string `form:"project"`
	Model     string `form:"model"`
}

// logEntry defines an aggregated log entry response.
type logEntry struct {
	Date         string `json:"date"`
	DateRaw      string `json:"date_raw"`
	Project      string `json:"project"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	CostMicros   int64  `json:"cost_micros"`
	Cost         string `json:"cost"`
	FailedCount  int64  `json:"failed_count"`
	Status       string `json:"status"`
	StatusText   string `json:"status_text"`
}

// List returns aggregated usage logs for the user.
func (h *LogsHandler) List(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var q logsListQuery
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

	ctx := c.Request.Context()

	query := h.db.WithContext(ctx).Model(&models.Usage{}).Where("user_id = ?", userID)

	if q.StartDate != "" {
		if startTime, errParse := time.ParseInLocation("2006-01-02", q.StartDate, time.Local); errParse == nil {
			query = query.Where("requested_at >= ?", startTime)
		}
	}
	if q.EndDate != "" {
		if endTime, errParse := time.ParseInLocation("2006-01-02", q.EndDate, time.Local); errParse == nil {
			query = query.Where("requested_at < ?", endTime.AddDate(0, 0, 1))
		}
	}
	if q.Project != "" {
		query = query.Where("source = ?", q.Project)
	}
	if q.Model != "" {
		query = query.Where("model = ?", q.Model)
	}

	// dailyAgg holds aggregated usage per day/model.
	type dailyAgg struct {
		Date         string
		Model        string
		Providers    string
		Requests     int64
		InputTokens  int64
		OutputTokens int64
		TotalTokens  int64
		CostMicros   int64
		FailedCount  int64
	}

	var total int64
	countQuery := h.db.WithContext(ctx).Model(&models.Usage{}).Where("user_id = ?", userID)
	if q.StartDate != "" {
		if startTime, errParse := time.ParseInLocation("2006-01-02", q.StartDate, time.Local); errParse == nil {
			countQuery = countQuery.Where("requested_at >= ?", startTime)
		}
	}
	if q.EndDate != "" {
		if endTime, errParse := time.ParseInLocation("2006-01-02", q.EndDate, time.Local); errParse == nil {
			countQuery = countQuery.Where("requested_at < ?", endTime.AddDate(0, 0, 1))
		}
	}
	if q.Project != "" {
		countQuery = countQuery.Where("source = ?", q.Project)
	}
	if q.Model != "" {
		countQuery = countQuery.Where("model = ?", q.Model)
	}
	countQuery.Select("COUNT(DISTINCT TO_CHAR(requested_at, 'YYYY-MM-DD') || model)").Scan(&total)

	offset := (q.Page - 1) * q.Limit
	var aggs []dailyAgg
	if errAgg := query.
		Select(`
			TO_CHAR(requested_at, 'YYYY-MM-DD') AS date,
			model,
			COALESCE(STRING_AGG(DISTINCT NULLIF(provider, ''), ','), '') AS providers,
			COUNT(*) AS requests,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			COALESCE(SUM(cost_micros), 0) AS cost_micros,
			SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed_count
		`).
		Group("TO_CHAR(requested_at, 'YYYY-MM-DD'), model").
		Order("TO_CHAR(requested_at, 'YYYY-MM-DD') DESC, model").
		Offset(offset).Limit(q.Limit).
		Scan(&aggs).Error; errAgg != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query logs failed"})
		return
	}

	logs := make([]logEntry, 0, len(aggs))
	for _, a := range aggs {
		status := "normal"
		statusText := "Normal"
		if a.FailedCount > 0 {
			errorRate := float64(a.FailedCount) / float64(a.Requests) * 100
			if errorRate > 5 {
				status = "error"
				statusText = fmt.Sprintf("%.1f%% Errors", errorRate)
			} else if errorRate > 1 {
				status = "warning"
				statusText = fmt.Sprintf("%.1f%% Errors", errorRate)
			}
		}

		dateFormatted := a.Date
		if t, errParse := time.Parse("2006-01-02", a.Date); errParse == nil {
			dateFormatted = t.Format("Jan 02, 2006")
		}

		logs = append(logs, logEntry{
			Date:         dateFormatted,
			DateRaw:      a.Date,
			Project:      "",
			Model:        a.Model,
			Provider:     strings.ReplaceAll(a.Providers, ",", ", "),
			Requests:     a.Requests,
			InputTokens:  a.InputTokens,
			OutputTokens: a.OutputTokens,
			TotalTokens:  a.TotalTokens,
			CostMicros:   a.CostMicros,
			Cost:         fmt.Sprintf("$%.2f", float64(a.CostMicros)/1_000_000),
			FailedCount:  a.FailedCount,
			Status:       status,
			StatusText:   statusText,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":  logs,
		"total": total,
		"page":  q.Page,
		"limit": q.Limit,
	})
}

// Stats returns high-level usage statistics for today vs yesterday.
func (h *LogsHandler) Stats(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx := c.Request.Context()
	loc := time.Local
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	// statResult holds aggregated stats for a time window.
	type statResult struct {
		Requests         int64
		TotalTokens      int64
		FailedCount      int64
		CostMicros       int64
		AvgRequestTimeMs float64
	}

	var todayStats, yesterdayStats statResult

	h.db.WithContext(ctx).Model(&models.Usage{}).
		Where("user_id = ? AND requested_at >= ?", userID, todayStart).
		Select(`
			COUNT(*) AS requests,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed_count,
			COALESCE(SUM(cost_micros), 0) AS cost_micros,
			COALESCE(AVG(GREATEST(EXTRACT(EPOCH FROM (created_at - requested_at)) * 1000, 0)), 0) AS avg_request_time_ms
		`).Scan(&todayStats)

	h.db.WithContext(ctx).Model(&models.Usage{}).
		Where("user_id = ? AND requested_at >= ? AND requested_at < ?", userID, yesterdayStart, todayStart).
		Select(`
			COUNT(*) AS requests,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed_count,
			COALESCE(SUM(cost_micros), 0) AS cost_micros,
			COALESCE(AVG(GREATEST(EXTRACT(EPOCH FROM (created_at - requested_at)) * 1000, 0)), 0) AS avg_request_time_ms
		`).Scan(&yesterdayStats)

	requestsChange := calcChange(todayStats.Requests, yesterdayStats.Requests)
	tokensChange := calcChange(todayStats.TotalTokens, yesterdayStats.TotalTokens)
	avgRequestTimeToday := int64(math.Round(todayStats.AvgRequestTimeMs))
	avgRequestTimeYesterday := int64(math.Round(yesterdayStats.AvgRequestTimeMs))
	requestTimeChange := calcChange(avgRequestTimeToday, avgRequestTimeYesterday)

	var errorRate float64
	if todayStats.Requests > 0 {
		errorRate = float64(todayStats.FailedCount) / float64(todayStats.Requests) * 100
	}
	var yesterdayErrorRate float64
	if yesterdayStats.Requests > 0 {
		yesterdayErrorRate = float64(yesterdayStats.FailedCount) / float64(yesterdayStats.Requests) * 100
	}
	errorRateChange := errorRate - yesterdayErrorRate

	c.JSON(http.StatusOK, gin.H{
		"requests_today":          todayStats.Requests,
		"requests_today_display":  formatNumber(todayStats.Requests),
		"requests_change":         requestsChange,
		"tokens_consumed":         todayStats.TotalTokens,
		"tokens_consumed_display": formatTokensDisplay(todayStats.TotalTokens),
		"tokens_change":           tokensChange,
		"avg_request_time_ms":     avgRequestTimeToday,
		"request_time_change":     requestTimeChange,
		"error_rate":              errorRate,
		"error_rate_display":      fmt.Sprintf("%.2f%%", errorRate),
		"error_rate_change":       errorRateChange,
	})
}

// Trend returns seven-day usage trends.
func (h *LogsHandler) Trend(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx := c.Request.Context()
	loc := time.Local
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	sevenDaysAgo := todayStart.AddDate(0, 0, -6)

	// dailyTrend holds raw daily trend values.
	type dailyTrend struct {
		Date        string
		Requests    int64
		TotalTokens int64
	}

	trend := make([]gin.H, 7)
	for i := 0; i < 7; i++ {
		day := sevenDaysAgo.AddDate(0, 0, i)
		trend[i] = gin.H{
			"day":      day.Format("Mon"),
			"date":     day.Format("2006-01-02"),
			"requests": int64(0),
			"tokens":   int64(0),
			"active":   i == 6,
		}
	}

	var dailyData []dailyTrend
	if errQuery := h.db.WithContext(ctx).Model(&models.Usage{}).
		Where("user_id = ? AND requested_at >= ?", userID, sevenDaysAgo).
		Select(`
			TO_CHAR(requested_at, 'YYYY-MM-DD') AS date,
			COUNT(*) AS requests,
			COALESCE(SUM(total_tokens), 0) AS total_tokens
		`).
		Group("TO_CHAR(requested_at, 'YYYY-MM-DD')").
		Order("TO_CHAR(requested_at, 'YYYY-MM-DD')").
		Scan(&dailyData).Error; errQuery != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query trend failed"})
		return
	}

	dataMap := make(map[string]dailyTrend)
	for _, d := range dailyData {
		dataMap[d.Date] = d
	}

	var maxRequests, maxTokens int64
	for _, d := range dailyData {
		if d.Requests > maxRequests {
			maxRequests = d.Requests
		}
		if d.TotalTokens > maxTokens {
			maxTokens = d.TotalTokens
		}
	}

	for i := 0; i < 7; i++ {
		day := sevenDaysAgo.AddDate(0, 0, i)
		dateStr := day.Format("2006-01-02")
		if d, ok := dataMap[dateStr]; ok {
			reqPercent := int64(0)
			tokenPercent := int64(0)
			if maxRequests > 0 {
				reqPercent = d.Requests * 100 / maxRequests
			}
			if maxTokens > 0 {
				tokenPercent = d.TotalTokens * 100 / maxTokens
			}
			trend[i]["requests"] = reqPercent
			trend[i]["tokens"] = tokenPercent
			trend[i]["requests_raw"] = d.Requests
			trend[i]["tokens_raw"] = d.TotalTokens
		}
	}

	c.JSON(http.StatusOK, gin.H{"trend": trend})
}

// Models returns distinct model names used by the user.
func (h *LogsHandler) Models(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var modelList []string
	if errModels := h.db.WithContext(c.Request.Context()).Table("usages").
		Where("user_id = ?", userID).
		Distinct("model").
		Pluck("model", &modelList).Error; errModels != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query models failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"models": modelList})
}

// Projects returns distinct project/source names used by the user.
func (h *LogsHandler) Projects(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var projects []string
	if errProjects := h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("user_id = ? AND source != ''", userID).
		Distinct("source").
		Pluck("source", &projects).Error; errProjects != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query projects failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

// calcChange computes percent change between two values.
func calcChange(current, previous int64) float64 {
	if previous == 0 {
		if current > 0 {
			return 100.0
		}
		return 0.0
	}
	return float64(current-previous) / float64(previous) * 100
}

// logDetailQuery defines query parameters for log detail retrieval.
type logDetailQuery struct {
	Date     string `form:"date" binding:"required"`
	Model    string `form:"model"`
	Provider string `form:"provider"`
	Project  string `form:"project"`
}

// logDetailEntry defines a detailed usage record.
type logDetailEntry struct {
	RequestedAt  time.Time `json:"requested_at"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CachedTokens int64     `json:"cached_tokens"`
	TotalTokens  int64     `json:"total_tokens"`
	CostMicros   int64     `json:"cost_micros"`
	Failed       bool      `json:"failed"`
}

// Detail returns raw usage details for a given day and filters.
func (h *LogsHandler) Detail(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var q logDetailQuery
	if errBind := c.ShouldBindQuery(&q); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid query"})
		return
	}

	dateKey := strings.TrimSpace(q.Date)
	if dateKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing date"})
		return
	}
	day, errParse := time.ParseInLocation("2006-01-02", dateKey, time.Local)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
		return
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 1)

	ctx := c.Request.Context()
	query := h.db.WithContext(ctx).
		Model(&models.Usage{}).
		Where("user_id = ?", userID).
		Where("requested_at >= ? AND requested_at < ?", start, end)

	if strings.TrimSpace(q.Model) != "" {
		query = query.Where("model = ?", strings.TrimSpace(q.Model))
	}
	if strings.TrimSpace(q.Provider) != "" {
		query = query.Where("provider = ?", strings.TrimSpace(q.Provider))
	}
	if strings.TrimSpace(q.Project) != "" {
		query = query.Where("source = ?", strings.TrimSpace(q.Project))
	}

	var rows []logDetailEntry
	if errFind := query.
		Select("requested_at, input_tokens, output_tokens, cached_tokens, total_tokens, cost_micros, failed").
		Order("requested_at DESC").
		Find(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query details failed"})
		return
	}

	details := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		details = append(details, gin.H{
			"requested_at":  row.RequestedAt.In(time.Local).Format(time.RFC3339),
			"input_tokens":  row.InputTokens,
			"output_tokens": row.OutputTokens,
			"cached_tokens": row.CachedTokens,
			"total_tokens":  row.TotalTokens,
			"cost":          fmt.Sprintf("$%.4f", float64(row.CostMicros)/1_000_000),
			"success":       !row.Failed,
		})
	}

	c.JSON(http.StatusOK, gin.H{"details": details})
}

// formatNumber formats large counts with suffixes.
func formatNumber(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// formatTokensDisplay formats token counts with suffixes.
func formatTokensDisplay(tokens int64) string {
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
