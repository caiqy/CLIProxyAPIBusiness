package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	permissions "github.com/router-for-me/CLIProxyAPIBusiness/internal/http/api/admin/permissions"
)

// PermissionHandler exposes permission definitions for admins.
type PermissionHandler struct{}

// NewPermissionHandler constructs a PermissionHandler.
func NewPermissionHandler() *PermissionHandler {
	return &PermissionHandler{}
}

// List returns all permission definitions.
func (h *PermissionHandler) List(c *gin.Context) {
	defs := permissions.Definitions()
	out := make([]gin.H, 0, len(defs))
	for _, def := range defs {
		out = append(out, gin.H{
			"key":    def.Key,
			"method": def.Method,
			"path":   def.Path,
			"label":  def.Label,
			"module": def.Module,
		})
	}
	c.JSON(http.StatusOK, gin.H{"permissions": out})
}
