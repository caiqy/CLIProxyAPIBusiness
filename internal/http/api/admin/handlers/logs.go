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

// AdminLogsHandler serves admin usage log endpoints.
type AdminLogsHandler struct {
	db *gorm.DB // Database handle for usage queries.
}

// NewAdminLogsHandler constructs an admin logs handler.
func NewAdminLogsHandler(db *gorm.DB) *AdminLogsHandler {
	return &AdminLogsHandler{db: db}
}

// adminLogsListQuery defines filters for the aggregated list view.
type adminLogsListQuery struct {
	Page      int    `form:"page,default=1"`   // Page number.
	Limit     int    `form:"limit,default=20"` // Page size.
	StartDate string `form:"start_date"`       // Inclusive start date.
	EndDate   string `form:"end_date"`         // Inclusive end date.
	Project   string `form:"project"`          // Source/project filter.
	Model     string `form:"model"`            // Model filter.
}

// adminLogEntry represents a row in the aggregated logs list.
type adminLogEntry struct {
	Date         string `json:"date"`          // Formatted date label.
	DateRaw      string `json:"date_raw"`      // Raw date string.
	Project      string `json:"project"`       // Project/source.
	Model        string `json:"model"`         // Model name.
	Provider     string `json:"provider"`      // Provider list.
	Requests     int64  `json:"requests"`      // Request count.
	InputTokens  int64  `json:"input_tokens"`  // Input token count.
	OutputTokens int64  `json:"output_tokens"` // Output token count.
	TotalTokens  int64  `json:"total_tokens"`  // Total token count.
	CostMicros   int64  `json:"cost_micros"`   // Cost in micros.
	Cost         string `json:"cost"`          // Cost display string.
	FailedCount  int64  `json:"failed_count"`  // Failed request count.
	Status       string `json:"status"`        // Status label.
	StatusText   string `json:"status_text"`   // Status display string.
}

// adminLogDetailQuery defines filters for log detail entries.
type adminLogDetailQuery struct {
	Date     string `form:"date" binding:"required"` // Target date.
	Model    string `form:"model"`                   // Model filter.
	Provider string `form:"provider"`                // Provider filter.
	Project  string `form:"project"`                 // Project/source filter.
}

// adminLogDetailEntry represents a single usage record in detail view.
type adminLogDetailEntry struct {
	RequestedAt  time.Time `json:"requested_at"`  // Request timestamp.
	InputTokens  int64     `json:"input_tokens"`  // Input token count.
	OutputTokens int64     `json:"output_tokens"` // Output token count.
	CachedTokens int64     `json:"cached_tokens"` // Cached token count.
	TotalTokens  int64     `json:"total_tokens"`  // Total token count.
	CostMicros   int64     `json:"cost_micros"`   // Cost in micros.
	Failed       bool      `json:"failed"`        // Failure flag.
	Username     string    `json:"username"`      // Username.
}

// List returns aggregated usage logs with paging and filters.
func (h *AdminLogsHandler) List(c *gin.Context) {
	var q adminLogsListQuery
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

	query := h.db.WithContext(ctx).
		Model(&models.Usage{})

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

	// dailyAgg captures aggregated log metrics for a date and model.
	type dailyAgg struct {
		Date         string // Date key.
		Model        string // Model name.
		Providers    string // Provider list.
		Requests     int64  // Request count.
		InputTokens  int64  // Input tokens.
		OutputTokens int64  // Output tokens.
		TotalTokens  int64  // Total tokens.
		CostMicros   int64  // Cost in micros.
		FailedCount  int64  // Failed request count.
	}

	var total int64
	countQuery := h.db.WithContext(ctx).
		Model(&models.Usage{})
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
	countQuery.Select("COUNT(DISTINCT TO_CHAR(requested_at, 'YYYY-MM-DD') || COALESCE(model, ''))").Scan(&total)

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

	logs := make([]adminLogEntry, 0, len(aggs))
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

		logs = append(logs, adminLogEntry{
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

// Detail returns per-request usage entries for a date and filters.
func (h *AdminLogsHandler) Detail(c *gin.Context) {
	var q adminLogDetailQuery
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
		Select(`
			requested_at,
			input_tokens,
			output_tokens,
			cached_tokens,
			total_tokens,
			cost_micros,
			failed,
			COALESCE(users.username, '') AS username
		`).
		Joins("LEFT JOIN users ON users.id = usages.user_id").
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

	var rows []adminLogDetailEntry
	if errFind := query.
		Order("requested_at DESC").
		Scan(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query details failed"})
		return
	}

	details := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		details = append(details, gin.H{
			"requested_at":  row.RequestedAt.In(time.Local).Format(time.RFC3339),
			"username":      row.Username,
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

// Stats returns aggregated KPIs for today vs yesterday.
func (h *AdminLogsHandler) Stats(c *gin.Context) {
	ctx := c.Request.Context()
	loc := time.Local
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	// statResult holds aggregated usage stats for a date range.
	type statResult struct {
		Requests         int64   // Request count.
		TotalTokens      int64   // Total token count.
		FailedCount      int64   // Failed request count.
		CostMicros       int64   // Cost in micros.
		AvgRequestTimeMs float64 // Average request time in ms.
	}

	var todayStats, yesterdayStats statResult

	h.db.WithContext(ctx).Model(&models.Usage{}).
		Where("requested_at >= ?", todayStart).
		Select(`
			COUNT(*) AS requests,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed_count,
			COALESCE(SUM(cost_micros), 0) AS cost_micros,
			COALESCE(AVG(GREATEST(EXTRACT(EPOCH FROM (created_at - requested_at)) * 1000, 0)), 0) AS avg_request_time_ms
		`).Scan(&todayStats)

	h.db.WithContext(ctx).Model(&models.Usage{}).
		Where("requested_at >= ? AND requested_at < ?", yesterdayStart, todayStart).
		Select(`
			COUNT(*) AS requests,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failed_count,
			COALESCE(SUM(cost_micros), 0) AS cost_micros,
			COALESCE(AVG(GREATEST(EXTRACT(EPOCH FROM (created_at - requested_at)) * 1000, 0)), 0) AS avg_request_time_ms
		`).Scan(&yesterdayStats)

	requestsChange := percentChange(todayStats.Requests, yesterdayStats.Requests)
	tokensChange := percentChange(todayStats.TotalTokens, yesterdayStats.TotalTokens)
	avgRequestTimeToday := int64(math.Round(todayStats.AvgRequestTimeMs))
	avgRequestTimeYesterday := int64(math.Round(yesterdayStats.AvgRequestTimeMs))
	requestTimeChange := percentChange(avgRequestTimeToday, avgRequestTimeYesterday)

	errorRate := calcErrorRate(todayStats.FailedCount, todayStats.Requests)
	yesterdayErrorRate := calcErrorRate(yesterdayStats.FailedCount, yesterdayStats.Requests)
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

// Trend returns a seven-day trend of requests and tokens.
func (h *AdminLogsHandler) Trend(c *gin.Context) {
	ctx := c.Request.Context()
	loc := time.Local
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	sevenDaysAgo := todayStart.AddDate(0, 0, -6)

	// dailyTrend stores aggregated metrics for a day.
	type dailyTrend struct {
		Date        string // Date key.
		Requests    int64  // Request count.
		TotalTokens int64  // Total tokens.
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

	var rows []dailyTrend
	if errFind := h.db.WithContext(ctx).
		Model(&models.Usage{}).
		Select(`TO_CHAR(requested_at, 'YYYY-MM-DD') AS date, COUNT(*) AS requests, COALESCE(SUM(total_tokens), 0) AS total_tokens`).
		Where("requested_at >= ?", sevenDaysAgo).
		Group("TO_CHAR(requested_at, 'YYYY-MM-DD')").
		Order("TO_CHAR(requested_at, 'YYYY-MM-DD')").
		Scan(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query trend failed"})
		return
	}

	trendMap := make(map[string]dailyTrend, len(rows))
	for _, row := range rows {
		trendMap[row.Date] = row
	}

	for i, item := range trend {
		date := item["date"].(string)
		if row, ok := trendMap[date]; ok {
			trend[i]["requests"] = row.Requests
			trend[i]["tokens"] = row.TotalTokens
		}
	}

	c.JSON(http.StatusOK, gin.H{"trend": trend})
}

// Models returns the distinct model names from usage logs.
func (h *AdminLogsHandler) Models(c *gin.Context) {
	var modelList []string
	if errModels := h.db.WithContext(c.Request.Context()).Table("usages").
		Distinct("model").
		Pluck("model", &modelList).Error; errModels != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query models failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"models": modelList})
}

// Projects returns the distinct project/source names from usage logs.
func (h *AdminLogsHandler) Projects(c *gin.Context) {
	var projects []string
	if errProjects := h.db.WithContext(c.Request.Context()).Model(&models.Usage{}).
		Where("source != ''").
		Distinct("source").
		Pluck("source", &projects).Error; errProjects != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query projects failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

// percentChange computes percentage change between two values.
func percentChange(current, previous int64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return (float64(current-previous) / float64(previous)) * 100
}

// calcErrorRate returns the failure rate percentage for a total count.
func calcErrorRate(failed, total int64) float64 {
	if total == 0 {
		return 0
	}
	return (float64(failed) / float64(total)) * 100
}

// formatNumber formats an integer as a string.
func formatNumber(num int64) string {
	return fmt.Sprintf("%d", num)
}

// formatTokensDisplay formats token counts into compact human-friendly strings.
func formatTokensDisplay(tokens int64) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}
