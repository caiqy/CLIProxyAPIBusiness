package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/security"
	"gorm.io/gorm"
)

// ProfileHandler handles user profile endpoints.
type ProfileHandler struct {
	db *gorm.DB
}

// NewProfileHandler constructs a ProfileHandler.
func NewProfileHandler(db *gorm.DB) *ProfileHandler {
	return &ProfileHandler{db: db}
}

// Get returns the current user's profile.
func (h *ProfileHandler) Get(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var user models.User
	if errFind := h.db.WithContext(c.Request.Context()).First(&user, userID).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         user.ID,
		"username":   user.Username,
		"email":      user.Email,
		"active":     user.Active,
		"disabled":   user.Disabled,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
	})
}

// changePasswordRequest defines the request body for password changes.
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword verifies and updates the user's password.
func (h *ProfileHandler) ChangePassword(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var body changePasswordRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	oldPassword := strings.TrimSpace(body.OldPassword)
	newPassword := strings.TrimSpace(body.NewPassword)
	if oldPassword == "" || newPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing password"})
		return
	}

	var user models.User
	if errFind := h.db.WithContext(c.Request.Context()).First(&user, userID).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	if !security.CheckPassword(user.Password, oldPassword) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "old password incorrect"})
		return
	}

	hash, errHash := security.HashPassword(newPassword)
	if errHash != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hash password failed"})
		return
	}

	if errUpdate := h.db.WithContext(c.Request.Context()).Model(&user).Updates(map[string]any{
		"password":   hash,
		"updated_at": time.Now().UTC(),
	}).Error; errUpdate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "change password failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
