package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	permissions "github.com/router-for-me/CLIProxyAPIBusiness/internal/http/api/admin/permissions"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// adminPermissionMiddleware enforces permission checks for admin routes.
func adminPermissionMiddleware(db *gorm.DB) gin.HandlerFunc {
	permissionMap := permissions.DefinitionMap()

	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		path := c.FullPath()
		if path == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}

		key := permissions.Key(c.Request.Method, path)
		if _, ok := permissionMap[key]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}

		adminPermissions, okPermissions := readAdminPermissionsFromContext(c)
		adminIsSuperAdmin, okSuper := readAdminIsSuperAdminFromContext(c)
		if !okPermissions || !okSuper {
			adminIDValue, exists := c.Get("adminID")
			if !exists {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "admin not found"})
				return
			}
			adminID, okID := adminIDValue.(uint64)
			if !okID {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "admin not found"})
				return
			}

			var admin models.Admin
			if errFind := db.WithContext(c.Request.Context()).Select("id", "permissions", "is_super_admin").First(&admin, adminID).Error; errFind != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "admin not found"})
				return
			}
			adminPermissions = permissions.ParsePermissions(admin.Permissions)
			adminIsSuperAdmin = admin.IsSuperAdmin
			c.Set("adminPermissions", adminPermissions)
			c.Set("adminIsSuperAdmin", adminIsSuperAdmin)
		}

		if adminIsSuperAdmin {
			c.Next()
			return
		}

		if !permissions.HasPermission(adminPermissions, key) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}

		c.Next()
	}
}

// readAdminPermissionsFromContext extracts permissions from the gin context.
func readAdminPermissionsFromContext(c *gin.Context) ([]string, bool) {
	value, ok := c.Get("adminPermissions")
	if !ok {
		return nil, false
	}
	permissionsList, ok := value.([]string)
	return permissionsList, ok
}

// readAdminIsSuperAdminFromContext extracts the super admin flag from context.
func readAdminIsSuperAdminFromContext(c *gin.Context) (bool, bool) {
	value, ok := c.Get("adminIsSuperAdmin")
	if !ok {
		return false, false
	}
	flag, ok := value.(bool)
	return flag, ok
}
