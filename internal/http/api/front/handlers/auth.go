package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/config"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/security"
	"gorm.io/gorm"
)

// AuthHandler handles user authentication endpoints.
type AuthHandler struct {
	db     *gorm.DB
	jwtCfg config.JWTConfig
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(db *gorm.DB, jwtCfg config.JWTConfig) *AuthHandler {
	return &AuthHandler{db: db, jwtCfg: jwtCfg}
}

// registerRequest defines the request body for user registration.
type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Register creates a new user account.
func (h *AuthHandler) Register(c *gin.Context) {
	var body registerRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	username := strings.TrimSpace(body.Username)
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing username"})
		return
	}
	password := strings.TrimSpace(body.Password)
	if password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing password"})
		return
	}

	var exists models.User
	if errCheck := h.db.WithContext(c.Request.Context()).Where("username = ?", username).First(&exists).Error; errCheck == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	hash, errHash := security.HashPassword(password)
	if errHash != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hash password failed"})
		return
	}

	now := time.Now().UTC()
	user := models.User{
		Username:  username,
		Email:     strings.TrimSpace(body.Email),
		Password:  hash,
		Active:    true,
		Disabled:  false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	var defaultGroup models.UserGroup
	if errFind := h.db.WithContext(c.Request.Context()).
		Where("is_default = ?", true).
		First(&defaultGroup).Error; errFind == nil {
		user.UserGroupID = &defaultGroup.ID
	} else if !errors.Is(errFind, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query default user group failed"})
		return
	}
	if errCreate := h.db.WithContext(c.Request.Context()).Create(&user).Error; errCreate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create user failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":       user.ID,
		"username": user.Username,
		"email":    user.Email,
	})
}

// loginRequest defines the request body for login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login authenticates a user and issues a JWT if MFA is not required.
func (h *AuthHandler) Login(c *gin.Context) {
	var body loginRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	username := strings.TrimSpace(body.Username)
	password := strings.TrimSpace(body.Password)
	if username == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing username or password"})
		return
	}

	var user models.User
	if errFind := h.db.WithContext(c.Request.Context()).Where("username = ?", username).First(&user).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	if user.Disabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "user disabled"})
		return
	}

	if !security.CheckPassword(user.Password, password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if strings.TrimSpace(user.TOTPSecret) != "" || len(user.PasskeyID) > 0 || len(user.PasskeyPublicKey) > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "mfa required"})
		return
	}

	h.respondWithUserToken(c, user)
}

// resetPasswordRequest defines the request body for password resets.
type resetPasswordRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	NewPassword string `json:"new_password"`
}

// ResetPassword updates a user's password after verification.
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var body resetPasswordRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	username := strings.TrimSpace(body.Username)
	email := strings.TrimSpace(body.Email)
	newPassword := strings.TrimSpace(body.NewPassword)
	if username == "" || email == "" || newPassword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required fields"})
		return
	}

	var user models.User
	if errFind := h.db.WithContext(c.Request.Context()).Where("username = ? AND email = ?", username, email).First(&user).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reset password failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
