package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type importAuthFilesByProviderRequest struct {
	Provider    string              `json:"provider"`
	Source      string              `json:"source"`
	AuthGroupID models.AuthGroupIDs `json:"auth_group_id"`
	Entries     []map[string]any    `json:"entries"`
}

type importAuthFilesByProviderFailure struct {
	Index int    `json:"index"`
	Key   string `json:"key,omitempty"`
	Error string `json:"error"`
}

type importAuthFilesByProviderResponse struct {
	Imported int                                `json:"imported"`
	Failed   []importAuthFilesByProviderFailure `json:"failed"`
}

type providerImportRule struct {
	allowedFields []string
	requireAny    []string
}

var providerAliasToCanonical = map[string]string{
	"codex":        "codex",
	"anthropic":    "claude",
	"claude":       "claude",
	"gemini":       "gemini",
	"gemini-cli":   "gemini",
	"antigravity":  "antigravity",
	"qwen":         "qwen",
	"kiro":         "kiro",
	"iflow":        "iflow",
	"iflow-cookie": "iflow",
}

var commonImportAllowedFields = []string{
	"email",
	"proxy_url",
	"prefix",
	"api_key",
	"access_token",
	"refresh_token",
	"id_token",
	"token",
	"cookie",
	"cookies",
	"bxauth",
	"base_url",
	"project_id",
	"organization_id",
	"profile_arn",
	"auth_method",
	"provider",
	"client_id",
	"client_secret",
	"expires_at",
	"expired",
	"expires_in",
	"timestamp",
	"last_refresh",
	"disable_cooling",
	"request_retry",
	"runtime_only",
	"name",
	"session_key",
}

var providerImportRules = map[string]providerImportRule{
	"codex": {
		allowedFields: commonImportAllowedFields,
		requireAny:    []string{"api_key", "access_token", "refresh_token", "id_token", "token"},
	},
	"claude": {
		allowedFields: commonImportAllowedFields,
		requireAny:    []string{"api_key", "access_token", "refresh_token", "id_token", "token"},
	},
	"gemini": {
		allowedFields: commonImportAllowedFields,
		requireAny:    []string{"token", "refresh_token", "access_token", "api_key"},
	},
	"antigravity": {
		allowedFields: commonImportAllowedFields,
		requireAny:    []string{"access_token", "refresh_token", "token", "api_key"},
	},
	"qwen": {
		allowedFields: commonImportAllowedFields,
		requireAny:    []string{"access_token", "refresh_token", "token", "api_key"},
	},
	"kiro": {
		allowedFields: commonImportAllowedFields,
		requireAny:    []string{"access_token", "refresh_token", "token"},
	},
	"iflow": {
		allowedFields: commonImportAllowedFields,
		requireAny:    []string{"api_key", "access_token", "refresh_token", "cookie", "bxauth", "token"},
	},
}

func normalizeProviderEntry(provider string, raw map[string]any) (map[string]any, error) {
	if raw == nil {
		return nil, fmt.Errorf("entry is required")
	}

	canonicalProvider, errCanonical := canonicalizeImportProvider(provider)
	if errCanonical != nil {
		return nil, errCanonical
	}

	rule, okRule := providerImportRules[canonicalProvider]
	if !okRule {
		return nil, fmt.Errorf("unsupported provider")
	}

	key := extractProviderImportKey(raw)
	if key == "" {
		return nil, fmt.Errorf("missing key")
	}

	if len(rule.requireAny) > 0 && !hasAnyRequiredCredentialField(raw, rule.requireAny) {
		return nil, fmt.Errorf("missing credential fields for provider %s", canonicalProvider)
	}

	normalized := map[string]any{
		"key":  key,
		"type": canonicalProvider,
	}

	for _, field := range rule.allowedFields {
		value, okValue := pickImportFieldValue(raw, field)
		if !okValue {
			continue
		}
		if !isMeaningfulImportValue(value) {
			continue
		}
		normalized[field] = value
	}

	if proxyURL, okProxy := normalized["proxy_url"].(string); okProxy {
		normalized["proxy_url"] = strings.TrimSpace(proxyURL)
	}

	return normalized, nil
}

func canonicalizeImportProvider(provider string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return "", fmt.Errorf("provider is required")
	}
	canonical, ok := providerAliasToCanonical[normalized]
	if !ok {
		return "", fmt.Errorf("unsupported provider")
	}
	return canonical, nil
}

func extractProviderImportKey(entry map[string]any) string {
	if entry == nil {
		return ""
	}
	if rawKey, okKey := entry["key"].(string); okKey {
		trimmed := strings.TrimSpace(rawKey)
		if trimmed != "" {
			return trimmed
		}
	}
	if rawID, okID := entry["id"].(string); okID {
		trimmed := strings.TrimSpace(rawID)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func pickImportFieldValue(entry map[string]any, field string) (any, bool) {
	if entry == nil || field == "" {
		return nil, false
	}
	if value, okValue := entry[field]; okValue {
		return value, true
	}
	metadataValue, okMetadata := entry["metadata"]
	if !okMetadata {
		return nil, false
	}
	metadata, okMap := metadataValue.(map[string]any)
	if !okMap {
		return nil, false
	}
	value, okValue := metadata[field]
	if !okValue {
		return nil, false
	}
	return value, true
}

func hasAnyRequiredCredentialField(entry map[string]any, fields []string) bool {
	for _, field := range fields {
		value, okValue := pickImportFieldValue(entry, field)
		if !okValue {
			continue
		}
		if isMeaningfulImportValue(value) {
			return true
		}
	}
	return false
}

func isMeaningfulImportValue(value any) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func resolveProviderImportAuthGroupIDs(c *gin.Context, db *gorm.DB, provided models.AuthGroupIDs) (models.AuthGroupIDs, error) {
	if provided != nil {
		return provided.Clean(), nil
	}

	var defaultGroup models.AuthGroup
	if errFind := db.WithContext(c.Request.Context()).
		Where("is_default = ?", true).
		First(&defaultGroup).Error; errFind == nil {
		defaultGroupID := defaultGroup.ID
		return models.AuthGroupIDs{&defaultGroupID}, nil
	} else if !errors.Is(errFind, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("query default auth group failed: %w", errFind)
	}

	return models.AuthGroupIDs{}, nil
}

// ImportByProvider imports auth entries using explicit provider-driven validation.
func (h *AuthFileHandler) ImportByProvider(c *gin.Context) {
	var body importAuthFilesByProviderRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	provider := strings.TrimSpace(body.Provider)
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is required"})
		return
	}
	if _, errProvider := canonicalizeImportProvider(provider); errProvider != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported provider"})
		return
	}

	source := strings.ToLower(strings.TrimSpace(body.Source))
	if source != "" && source != "file" && source != "text" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source"})
		return
	}
	if len(body.Entries) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "entries are required"})
		return
	}

	authGroupIDs, errGroup := resolveProviderImportAuthGroupIDs(c, h.db, body.AuthGroupID)
	if errGroup != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query default auth group failed"})
		return
	}

	now := time.Now().UTC()
	imported := 0
	failures := make([]importAuthFilesByProviderFailure, 0)

	for idx, entry := range body.Entries {
		normalized, errNormalize := normalizeProviderEntry(provider, entry)
		if errNormalize != nil {
			failures = append(failures, importAuthFilesByProviderFailure{
				Index: idx + 1,
				Error: errNormalize.Error(),
			})
			continue
		}

		key := extractProviderImportKey(normalized)
		if key == "" {
			failures = append(failures, importAuthFilesByProviderFailure{
				Index: idx + 1,
				Error: "missing key",
			})
			continue
		}

		proxyURL := ""
		if rawProxy, okProxy := normalized["proxy_url"].(string); okProxy {
			proxyURL = strings.TrimSpace(rawProxy)
		}
		if proxyURL == "" && autoAssignProxyEnabled() {
			assignedProxyURL, errAssignProxy := pickRandomProxyURL(c.Request.Context(), h.db)
			if errAssignProxy != nil {
				failures = append(failures, importAuthFilesByProviderFailure{
					Index: idx + 1,
					Key:   key,
					Error: "auto assign proxy failed",
				})
				continue
			}
			if assignedProxyURL != "" {
				proxyURL = assignedProxyURL
			}
		}

		contentBytes, errMarshal := json.Marshal(normalized)
		if errMarshal != nil {
			failures = append(failures, importAuthFilesByProviderFailure{
				Index: idx + 1,
				Key:   key,
				Error: "marshal json failed",
			})
			continue
		}

		auth := models.Auth{
			Key:         key,
			Name:        key,
			AuthGroupID: authGroupIDs,
			ProxyURL:    proxyURL,
			Content:     datatypes.JSON(contentBytes),
			IsAvailable: true,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		errCreate := h.db.WithContext(c.Request.Context()).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"auth_group_id": auth.AuthGroupID,
				"proxy_url":     auth.ProxyURL,
				"content":       auth.Content,
				"updated_at":    now,
			}),
		}).Create(&auth).Error
		if errCreate != nil {
			failures = append(failures, importAuthFilesByProviderFailure{
				Index: idx + 1,
				Key:   key,
				Error: "import auth file failed",
			})
			continue
		}

		imported++
	}

	c.JSON(http.StatusOK, importAuthFilesByProviderResponse{
		Imported: imported,
		Failed:   failures,
	})
}
