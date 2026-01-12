package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HealthHandler serves health check endpoints.
type HealthHandler struct {
	db *gorm.DB
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Healthz checks database connectivity and returns status.
func (h *HealthHandler) Healthz(c *gin.Context) {
	sqlDB, err := h.db.DB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false})
		return
	}
	if errPing := sqlDB.PingContext(c.Request.Context()); errPing != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
