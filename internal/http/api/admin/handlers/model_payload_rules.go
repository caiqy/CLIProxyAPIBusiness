package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ModelPayloadRuleHandler handles admin CRUD for model payload rules.
type ModelPayloadRuleHandler struct {
	db *gorm.DB // Database handle for payload rule queries.
}

// NewModelPayloadRuleHandler constructs a payload rule handler.
func NewModelPayloadRuleHandler(db *gorm.DB) *ModelPayloadRuleHandler {
	return &ModelPayloadRuleHandler{db: db}
}

// createPayloadRuleRequest captures the payload for creating a rule.
type createPayloadRuleRequest struct {
	Protocol    string          `json:"protocol"`    // Explicit protocol override.
	Params      json.RawMessage `json:"params"`      // Raw JSON params for payload overrides.
	IsEnabled   *bool           `json:"is_enabled"`  // Optional active flag.
	Description *string         `json:"description"` // Optional description.
}

// updatePayloadRuleRequest captures optional fields for rule updates.
type updatePayloadRuleRequest struct {
	Protocol    *string          `json:"protocol"`    // Optional protocol override.
	Params      *json.RawMessage `json:"params"`      // Optional raw JSON params.
	IsEnabled   *bool            `json:"is_enabled"`  // Optional active flag.
	Description *string          `json:"description"` // Optional description update.
}

// List returns payload rules for a model mapping.
func (h *ModelPayloadRuleHandler) List(c *gin.Context) {
	mappingID, errParse := parseUintParam(c.Param("id"))
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model mapping id"})
		return
	}
	if errFind := h.ensureModelMapping(c, mappingID); errFind != nil {
		return
	}

	var rows []models.ModelPayloadRule
	if errFind := h.db.WithContext(c.Request.Context()).
		Where("model_mapping_id = ?", mappingID).
		Order("id ASC").
		Find(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list payload rules failed"})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		out = append(out, h.formatPayloadRule(&rows[i]))
	}
	c.JSON(http.StatusOK, gin.H{"rules": out})
}

// Create validates input and persists a payload rule for a mapping.
func (h *ModelPayloadRuleHandler) Create(c *gin.Context) {
	mappingID, errParse := parseUintParam(c.Param("id"))
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model mapping id"})
		return
	}
	provider, errProvider := h.loadModelMappingProvider(c, mappingID)
	if errProvider != nil {
		return
	}

	var body createPayloadRuleRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	var existing models.ModelPayloadRule
	if errFind := h.db.WithContext(c.Request.Context()).
		Where("model_mapping_id = ?", mappingID).
		First(&existing).Error; errFind == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "payload rule already exists"})
		return
	}

	params, errParams := normalizePayloadParams(body.Params)
	if errParams != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errParams.Error()})
		return
	}

	isEnabled := true
	if body.IsEnabled != nil {
		isEnabled = *body.IsEnabled
	}
	protocol := protocolFromProvider(provider)
	if protocol == "" {
		protocol = strings.TrimSpace(body.Protocol)
	}
	description := ""
	if body.Description != nil {
		description = strings.TrimSpace(*body.Description)
	}

	now := time.Now().UTC()
	rule := models.ModelPayloadRule{
		ModelMappingID: mappingID,
		Protocol:       protocol,
		Params:         params,
		IsEnabled:      isEnabled,
		Description:    description,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if errCreate := h.db.WithContext(c.Request.Context()).Create(&rule).Error; errCreate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create payload rule failed"})
		return
	}
	c.JSON(http.StatusCreated, h.formatPayloadRule(&rule))
}

// Update applies validated changes to an existing payload rule.
func (h *ModelPayloadRuleHandler) Update(c *gin.Context) {
	mappingID, errParse := parseUintParam(c.Param("id"))
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model mapping id"})
		return
	}
	ruleID, errParse := parseUintParam(c.Param("rule_id"))
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}
	provider, errProvider := h.loadModelMappingProvider(c, mappingID)
	if errProvider != nil {
		return
	}

	var body updatePayloadRuleRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	var existing models.ModelPayloadRule
	if errFind := h.db.WithContext(c.Request.Context()).
		Where("id = ? AND model_mapping_id = ?", ruleID, mappingID).
		First(&existing).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	updates := map[string]any{
		"updated_at": time.Now().UTC(),
	}
	protocol := protocolFromProvider(provider)
	if protocol != "" {
		updates["protocol"] = protocol
	} else if body.Protocol != nil {
		updates["protocol"] = strings.TrimSpace(*body.Protocol)
	}
	if body.Params != nil {
		params, errParams := normalizePayloadParams(*body.Params)
		if errParams != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": errParams.Error()})
			return
		}
		updates["params"] = params
	}
	if body.IsEnabled != nil {
		updates["is_enabled"] = *body.IsEnabled
	}
	if body.Description != nil {
		updates["description"] = strings.TrimSpace(*body.Description)
	}

	res := h.db.WithContext(c.Request.Context()).
		Model(&models.ModelPayloadRule{}).
		Where("id = ? AND model_mapping_id = ?", ruleID, mappingID).
		Updates(updates)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes a payload rule by mapping and rule ID.
func (h *ModelPayloadRuleHandler) Delete(c *gin.Context) {
	mappingID, errParse := parseUintParam(c.Param("id"))
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model mapping id"})
		return
	}
	ruleID, errParse := parseUintParam(c.Param("rule_id"))
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}
	if errFind := h.ensureModelMapping(c, mappingID); errFind != nil {
		return
	}

	res := h.db.WithContext(c.Request.Context()).
		Where("id = ? AND model_mapping_id = ?", ruleID, mappingID).
		Delete(&models.ModelPayloadRule{})
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

// loadModelMappingProvider loads the provider name for a mapping.
func (h *ModelPayloadRuleHandler) loadModelMappingProvider(c *gin.Context, mappingID uint64) (string, error) {
	if h == nil || h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "missing db"})
		return "", errors.New("missing db")
	}
	var mapping models.ModelMapping
	if errFind := h.db.WithContext(c.Request.Context()).
		Select("id", "provider").
		First(&mapping, mappingID).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "model mapping not found"})
			return "", errFind
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return "", errFind
	}
	return strings.TrimSpace(mapping.Provider), nil
}

// ensureModelMapping verifies the mapping exists before performing mutations.
func (h *ModelPayloadRuleHandler) ensureModelMapping(c *gin.Context, mappingID uint64) error {
	if h == nil || h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "missing db"})
		return errors.New("missing db")
	}
	var mapping models.ModelMapping
	if errFind := h.db.WithContext(c.Request.Context()).
		Select("id").
		First(&mapping, mappingID).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "model mapping not found"})
			return errFind
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return errFind
	}
	return nil
}

// formatPayloadRule converts a payload rule model into a response payload.
func (h *ModelPayloadRuleHandler) formatPayloadRule(rule *models.ModelPayloadRule) gin.H {
	if rule == nil {
		return gin.H{}
	}
	return gin.H{
		"id":               rule.ID,
		"model_mapping_id": rule.ModelMappingID,
		"protocol":         rule.Protocol,
		"params":           rule.Params,
		"is_enabled":       rule.IsEnabled,
		"description":      rule.Description,
		"created_at":       rule.CreatedAt,
		"updated_at":       rule.UpdatedAt,
	}
}

// protocolFromProvider maps provider identifiers to a protocol name.
func protocolFromProvider(provider string) string {
	key := strings.ToLower(strings.TrimSpace(provider))
	switch key {
	case "codex":
		return "codex"
	case "claude", "claude-code":
		return "claude"
	case "gemini", "gemini-cli", "antigravity", "vertex", "aistudio":
		return "gemini"
	case "iflow",
		"qwen",
		"openai",
		"openai-chat",
		"openai-chatcompletion",
		"openai-chatcompletions",
		"openai-chat-completion",
		"openai-chat-completions",
		"openai-chat-completion-v1",
		"openai-compatibility":
		return "openai"
	default:
		return ""
	}
}

// normalizePayloadParams validates and normalizes JSON params payloads.
func normalizePayloadParams(raw json.RawMessage) (datatypes.JSON, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return datatypes.JSON([]byte("[]")), nil
	}
	if !json.Valid(trimmed) {
		return nil, errors.New("params must be valid JSON")
	}
	copied := make([]byte, len(trimmed))
	copy(copied, trimmed)
	return datatypes.JSON(copied), nil
}

// parseUintParam trims and parses a uint64 from a string parameter.
func parseUintParam(value string) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(value), 10, 64)
}
