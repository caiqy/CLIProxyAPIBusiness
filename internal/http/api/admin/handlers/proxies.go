package handlers

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	dbutil "github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// ProxyHandler manages admin proxy CRUD endpoints.
type ProxyHandler struct {
	db *gorm.DB // Database handle for proxy records.
}

// NewProxyHandler constructs a proxy handler with a database dependency.
func NewProxyHandler(db *gorm.DB) *ProxyHandler {
	return &ProxyHandler{db: db}
}

// createProxyRequest captures the payload for creating a proxy.
type createProxyRequest struct {
	ProxyURL string `json:"proxy_url"` // Proxy URL.
}

// updateProxyRequest captures the payload for updating a proxy.
type updateProxyRequest struct {
	ProxyURL *string `json:"proxy_url"` // Optional updated proxy URL.
}

// batchCreateProxyRequest captures the payload for batch proxy creation.
type batchCreateProxyRequest struct {
	ProxyURLs []string `json:"proxy_urls"` // List of proxy URLs.
}

// Create validates and inserts a new proxy record.
func (h *ProxyHandler) Create(c *gin.Context) {
	var body createProxyRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	normalized, errNormalize := normalizeProxyURL(body.ProxyURL)
	if errNormalize != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy_url"})
		return
	}

	now := time.Now().UTC()
	row := models.Proxy{
		ProxyURL:  normalized,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if errCreate := h.db.WithContext(c.Request.Context()).Create(&row).Error; errCreate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create proxy failed"})
		return
	}

	c.JSON(http.StatusCreated, proxyRow(&row))
}

// List returns proxies filtered by keyword.
func (h *ProxyHandler) List(c *gin.Context) {
	keywordQ := strings.TrimSpace(c.Query("keyword"))

	q := h.db.WithContext(c.Request.Context()).Model(&models.Proxy{})
	if keywordQ != "" {
		pattern := dbutil.NormalizeLikePattern(h.db, "%"+keywordQ+"%")
		q = q.Where(dbutil.CaseInsensitiveLikeExpr(h.db, "proxy_url"), pattern)
	}

	var rows []models.Proxy
	if errFind := q.Order("created_at DESC").Find(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list proxies failed"})
		return
	}

	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		out = append(out, proxyRow(&rows[i]))
	}
	c.JSON(http.StatusOK, gin.H{"proxies": out})
}

// Update validates and updates a proxy record by ID.
func (h *ProxyHandler) Update(c *gin.Context) {
	id, errID := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errID != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var row models.Proxy
	if errFind := h.db.WithContext(c.Request.Context()).First(&row, "id = ?", id).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "proxy not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "fetch proxy failed"})
		return
	}

	var body updateProxyRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	if body.ProxyURL != nil {
		normalized, errNormalize := normalizeProxyURL(*body.ProxyURL)
		if errNormalize != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid proxy_url"})
			return
		}
		row.ProxyURL = normalized
	}

	row.UpdatedAt = time.Now().UTC()
	if errSave := h.db.WithContext(c.Request.Context()).Save(&row).Error; errSave != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update proxy failed"})
		return
	}

	c.JSON(http.StatusOK, proxyRow(&row))
}

// Delete removes a proxy record by ID.
func (h *ProxyHandler) Delete(c *gin.Context) {
	id, errID := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errID != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if errDelete := h.db.WithContext(c.Request.Context()).Delete(&models.Proxy{}, "id = ?", id).Error; errDelete != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete proxy failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// BatchCreate creates multiple proxy records in one request.
func (h *ProxyHandler) BatchCreate(c *gin.Context) {
	var body batchCreateProxyRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	if len(body.ProxyURLs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "proxy_urls is required"})
		return
	}

	now := time.Now().UTC()
	rows := make([]models.Proxy, 0, len(body.ProxyURLs))
	for idx, raw := range body.ProxyURLs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		normalized, errNormalize := normalizeProxyURL(trimmed)
		if errNormalize != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid proxy_url at line %d", idx+1)})
			return
		}
		rows = append(rows, models.Proxy{
			ProxyURL:  normalized,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	if len(rows) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "proxy_urls is required"})
		return
	}

	if errCreate := h.db.WithContext(c.Request.Context()).Create(&rows).Error; errCreate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "batch create proxies failed"})
		return
	}

	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		out = append(out, proxyRow(&rows[i]))
	}
	c.JSON(http.StatusCreated, gin.H{"proxies": out})
}

// normalizeProxyURL validates and normalizes proxy URL inputs.
func normalizeProxyURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("proxy_url is required")
	}

	parsed, errParse := url.Parse(trimmed)
	if errParse != nil {
		return "", errParse
	}

	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" && scheme != "socks5" {
		return "", fmt.Errorf("invalid scheme")
	}
	parsed.Scheme = scheme

	if parsed.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	if _, _, errHost := net.SplitHostPort(parsed.Host); errHost != nil {
		return "", errHost
	}

	if parsed.Path == "" {
		parsed.Path = "/"
	}

	return parsed.String(), nil
}

// proxyRow converts a proxy model into a response payload.
func proxyRow(row *models.Proxy) gin.H {
	if row == nil {
		return gin.H{}
	}
	return gin.H{
		"id":         row.ID,
		"proxy_url":  row.ProxyURL,
		"created_at": row.CreatedAt,
		"updated_at": row.UpdatedAt,
	}
}
