package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	internalsettings "github.com/router-for-me/CLIProxyAPIBusiness/internal/settings"
)

// publicConfigResponse is the response payload for public config.
type publicConfigResponse struct {
	SiteName string `json:"site_name"`
}

// GetPublicConfig returns public configuration for the front UI.
func GetPublicConfig(c *gin.Context) {
	siteName := dbConfigString(internalsettings.SiteNameKey)
	if siteName == "" {
		siteName = internalsettings.DefaultSiteName
	}
	c.JSON(http.StatusOK, publicConfigResponse{SiteName: siteName})
}

// dbConfigString reads a string value from the DB config snapshot.
func dbConfigString(key string) string {
	raw, ok := internalsettings.DBConfigValue(key)
	if !ok {
		return ""
	}
	return parseDBConfigString(raw)
}

// parseDBConfigString extracts a string from JSON config payloads.
func parseDBConfigString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var s string
	if errUnmarshal := json.Unmarshal(raw, &s); errUnmarshal == nil {
		return strings.TrimSpace(s)
	}
	// wrapper allows parsing values wrapped in a { "value": ... } object.
	var wrapper struct {
		Value json.RawMessage `json:"value"`
	}
	if errUnmarshal := json.Unmarshal(raw, &wrapper); errUnmarshal == nil {
		if len(wrapper.Value) > 0 {
			return parseDBConfigString(wrapper.Value)
		}
	}
	return ""
}
