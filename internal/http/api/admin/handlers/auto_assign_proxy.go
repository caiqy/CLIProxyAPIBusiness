package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	dbutil "github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	internalsettings "github.com/router-for-me/CLIProxyAPIBusiness/internal/settings"
	"gorm.io/gorm"
)

// autoAssignProxyEnabled reports whether auto proxy assignment is enabled.
func autoAssignProxyEnabled() bool {
	raw, ok := internalsettings.DBConfigValue(internalsettings.AutoAssignProxyKey)
	if !ok {
		return internalsettings.DefaultAutoAssignProxy
	}
	return parseDBConfigBool(raw)
}

// pickRandomProxyURL selects a random proxy URL from the proxy table.
func pickRandomProxyURL(ctx context.Context, db *gorm.DB) (string, error) {
	var row models.Proxy
	if errFind := db.WithContext(ctx).
		Order(randomOrderExpr(db)).
		Limit(1).
		Take(&row).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", errFind
	}
	return strings.TrimSpace(row.ProxyURL), nil
}

// randomOrderExpr returns a dialect-aware random ordering expression.
func randomOrderExpr(db *gorm.DB) string {
	switch dbutil.DialectName(db) {
	case dbutil.DialectSQLite, dbutil.DialectPostgres:
		return "RANDOM()"
	default:
		return "RANDOM()"
	}
}

// parseDBConfigBool parses a boolean from JSON config payloads.
func parseDBConfigBool(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	var parsedBool bool
	if errUnmarshalBool := json.Unmarshal(raw, &parsedBool); errUnmarshalBool == nil {
		return parsedBool
	}
	var parsedString string
	if errUnmarshalString := json.Unmarshal(raw, &parsedString); errUnmarshalString == nil {
		trimmed := strings.TrimSpace(parsedString)
		if trimmed == "" {
			return false
		}
		if strings.EqualFold(trimmed, "true") || trimmed == "1" {
			return true
		}
		return false
	}
	var parsedNumber float64
	if errUnmarshalNumber := json.Unmarshal(raw, &parsedNumber); errUnmarshalNumber == nil {
		return parsedNumber != 0
	}
	var wrapper struct {
		Value json.RawMessage `json:"value"`
	}
	if errUnmarshalWrapper := json.Unmarshal(raw, &wrapper); errUnmarshalWrapper == nil {
		if len(wrapper.Value) > 0 {
			return parseDBConfigBool(wrapper.Value)
		}
	}
	return false
}
