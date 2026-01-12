package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	dbutil "github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/http/api/admin/permissions"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// AdminHandler manages admin account endpoints.
type AdminHandler struct {
	db *gorm.DB
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(db *gorm.DB) *AdminHandler {
	return &AdminHandler{db: db}
}

// createAdminRequest defines the request body for admin creation.
type createAdminRequest struct {
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	Permissions  []string `json:"permissions"`
	IsSuperAdmin bool     `json:"is_super_admin"`
}

// Create creates a new admin account.
func (h *AdminHandler) Create(c *gin.Context) {
	var body createAdminRequest
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

	hash, errHash := security.HashPassword(password)
	if errHash != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hash password failed"})
		return
	}

	normalizedPermissions := permissions.NormalizePermissions(body.Permissions)
	if errValidate := permissions.ValidatePermissions(normalizedPermissions); errValidate != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid permissions"})
		return
	}
	permissionsJSON, errMarshal := permissions.MarshalPermissions(normalizedPermissions)
	if errMarshal != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "marshal permissions failed"})
		return
	}

	now := time.Now().UTC()
	admin := models.Admin{
		Username:     username,
		Password:     hash,
		Active:       true,
		IsSuperAdmin: body.IsSuperAdmin,
		Permissions:  datatypes.JSON(permissionsJSON),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if errCreate := h.db.WithContext(c.Request.Context()).Create(&admin).Error; errCreate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create admin failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":             admin.ID,
		"username":       admin.Username,
		"active":         admin.Active,
		"is_super_admin": admin.IsSuperAdmin,
		"permissions":    permissions.ParsePermissions(admin.Permissions),
	})
}

// List returns all admin accounts with optional filters.
func (h *AdminHandler) List(c *gin.Context) {
	var (
		usernameQ = strings.TrimSpace(c.Query("username"))
		idQ       = strings.TrimSpace(c.Query("id"))
	)

	q := h.db.WithContext(c.Request.Context()).Model(&models.Admin{})
	if usernameQ != "" {
		pattern := dbutil.NormalizeLikePattern(h.db, "%"+usernameQ+"%")
		q = q.Where(dbutil.CaseInsensitiveLikeExpr(h.db, "username"), pattern)
	}
	if idQ != "" {
		if id, errParse := strconv.ParseUint(idQ, 10, 64); errParse == nil {
			q = q.Where("id = ?", id)
		}
	}

	var rows []models.Admin
	if errFind := q.Order("created_at DESC").Find(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list admins failed"})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, gin.H{
			"id":             row.ID,
			"username":       row.Username,
			"active":         row.Active,
			"is_super_admin": row.IsSuperAdmin,
			"permissions":    permissions.ParsePermissions(row.Permissions),
			"created_at":     row.CreatedAt,
			"updated_at":     row.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"admins": out})
}

// Get returns a single admin account by ID.
func (h *AdminHandler) Get(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var admin models.Admin
	if errFind := h.db.WithContext(c.Request.Context()).First(&admin, id).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":             admin.ID,
		"username":       admin.Username,
		"active":         admin.Active,
		"is_super_admin": admin.IsSuperAdmin,
		"permissions":    permissions.ParsePermissions(admin.Permissions),
		"created_at":     admin.CreatedAt,
		"updated_at":     admin.UpdatedAt,
	})
}

// updateAdminRequest defines the request body for admin updates.
type updateAdminRequest struct {
	Username     *string   `json:"username"`
	Permissions  *[]string `json:"permissions"`
	IsSuperAdmin *bool     `json:"is_super_admin"`
}

// Update modifies admin account fields.
func (h *AdminHandler) Update(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body updateAdminRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	updates := map[string]any{"updated_at": time.Now().UTC()}
	if body.Username != nil {
		username := strings.TrimSpace(*body.Username)
		if username == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "username cannot be empty"})
			return
		}
		updates["username"] = username
	}
	if body.Permissions != nil {
		normalizedPermissions := permissions.NormalizePermissions(*body.Permissions)
		if errValidate := permissions.ValidatePermissions(normalizedPermissions); errValidate != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid permissions"})
			return
		}
		permissionsJSON, errMarshal := permissions.MarshalPermissions(normalizedPermissions)
		if errMarshal != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "marshal permissions failed"})
			return
		}
		updates["permissions"] = datatypes.JSON(permissionsJSON)
	}
	if body.IsSuperAdmin != nil {
		updates["is_super_admin"] = *body.IsSuperAdmin
	}

	res := h.db.WithContext(c.Request.Context()).Model(&models.Admin{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes an admin account.
func (h *AdminHandler) Delete(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	res := h.db.WithContext(c.Request.Context()).Delete(&models.Admin{}, id)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// Disable deactivates an admin account.
func (h *AdminHandler) Disable(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	res := h.db.WithContext(c.Request.Context()).Model(&models.Admin{}).
		Where("id = ?", id).
		Updates(map[string]any{"active": false, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "disable failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Enable reactivates an admin account.
func (h *AdminHandler) Enable(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	res := h.db.WithContext(c.Request.Context()).Model(&models.Admin{}).
		Where("id = ?", id).
		Updates(map[string]any{"active": true, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "enable failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// changeAdminPasswordRequest defines the request body for password changes.
type changeAdminPasswordRequest struct {
	Password    string `json:"password"`
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword updates the admin password with optional old password check.
func (h *AdminHandler) ChangePassword(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body changeAdminPasswordRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	oldPassword := strings.TrimSpace(body.OldPassword)
	newPassword := strings.TrimSpace(body.NewPassword)
	password := strings.TrimSpace(body.Password)
	if oldPassword != "" || newPassword != "" {
		if oldPassword == "" || newPassword == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing password"})
			return
		}
		var admin models.Admin
		if errFind := h.db.WithContext(c.Request.Context()).Select("id", "password").First(&admin, id).Error; errFind != nil {
			if errors.Is(errFind, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		if !security.CheckPassword(admin.Password, oldPassword) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		password = newPassword
	}
	if password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing password"})
		return
	}
	hash, errHash := security.HashPassword(password)
	if errHash != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hash password failed"})
		return
	}
	res := h.db.WithContext(c.Request.Context()).Model(&models.Admin{}).
		Where("id = ?", id).
		Updates(map[string]any{"password": hash, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "change password failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
