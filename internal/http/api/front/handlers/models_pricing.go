package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	sdkcliproxy "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/billing"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/modelregistry"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	internalsettings "github.com/router-for-me/CLIProxyAPIBusiness/internal/settings"
	"gorm.io/gorm"
)

// ModelPricingHandler serves model pricing endpoints.
type ModelPricingHandler struct {
	db    *gorm.DB
	store *modelregistry.Store
}

// NewModelPricingHandler constructs a ModelPricingHandler.
func NewModelPricingHandler(db *gorm.DB, store *modelregistry.Store) *ModelPricingHandler {
	return &ModelPricingHandler{db: db, store: store}
}

// modelPricingItem defines pricing details for a model.
type modelPricingItem struct {
	Provider              string             `json:"provider"`
	Model                 string             `json:"model"`
	DisplayName           string             `json:"display_name"`
	OriginalModel         string             `json:"original_model,omitempty"`
	BillingType           models.BillingType `json:"billing_type"`
	RuleID                uint64             `json:"rule_id,omitempty"`
	PricePerRequest       *float64           `json:"price_per_request,omitempty"`
	PriceInputToken       *float64           `json:"price_input_token,omitempty"`
	PriceOutputToken      *float64           `json:"price_output_token,omitempty"`
	PriceCacheCreateToken *float64           `json:"price_cache_create_token,omitempty"`
	PriceCacheReadToken   *float64           `json:"price_cache_read_token,omitempty"`
}

// modelAvailability captures availability metadata for a model.
type modelAvailability struct {
	Provider      string
	ModelID       string
	DisplayName   string
	OriginalModel string
}

// List returns pricing details for available models.
func (h *ModelPricingHandler) List(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database not initialized"})
		return
	}

	ctx := c.Request.Context()

	var user models.User
	if errFind := h.db.WithContext(ctx).Select("id", "user_group_id").First(&user, userID).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query user failed"})
		return
	}
	defaultUserGroupID, errDefaultUserGroup := billing.ResolveDefaultUserGroupID(ctx, h.db)
	if errDefaultUserGroup != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query default user group failed"})
		return
	}
	userGroupID := user.UserGroupID
	if userGroupID == nil {
		userGroupID = defaultUserGroupID
	}

	authGroupID, errAuthGroup := billing.ResolveDefaultAuthGroupID(ctx, h.db)
	if errAuthGroup != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query auth group failed"})
		return
	}

	var authGroupIDValue uint64
	if authGroupID != nil {
		authGroupIDValue = *authGroupID
	}
	var userGroupIDValue uint64
	if userGroupID != nil {
		userGroupIDValue = *userGroupID
	}
	var defaultUserGroupIDValue uint64
	if defaultUserGroupID != nil {
		defaultUserGroupIDValue = *defaultUserGroupID
	}

	rules, errRules := h.loadBillingRules(ctx, authGroupIDValue, userGroupIDValue, authGroupIDValue, defaultUserGroupIDValue)
	if errRules != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query billing rules failed"})
		return
	}

	onlyMapped := loadOnlyMapped()
	available, errModels := h.loadAvailableModels(ctx, onlyMapped)
	if errModels != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load available models failed"})
		return
	}

	perRequest := make([]modelPricingItem, 0)
	perToken := make([]modelPricingItem, 0)
	unpriced := make([]modelPricingItem, 0)

	for _, item := range available {
		rule := billing.SelectBillingRule(rules, authGroupIDValue, userGroupIDValue, authGroupIDValue, defaultUserGroupIDValue, item.Provider, item.ModelID)
		result := modelPricingItem{
			Provider:      item.Provider,
			Model:         item.ModelID,
			DisplayName:   item.DisplayName,
			OriginalModel: item.OriginalModel,
		}
		if rule != nil {
			result.BillingType = rule.BillingType
			result.RuleID = rule.ID
			result.PricePerRequest = rule.PricePerRequest
			result.PriceInputToken = rule.PriceInputToken
			result.PriceOutputToken = rule.PriceOutputToken
			result.PriceCacheCreateToken = rule.PriceCacheCreateToken
			result.PriceCacheReadToken = rule.PriceCacheReadToken
		}

		switch result.BillingType {
		case models.BillingTypePerRequest:
			perRequest = append(perRequest, result)
		case models.BillingTypePerToken:
			perToken = append(perToken, result)
		default:
			unpriced = append(unpriced, result)
		}
	}

	sortModelPricing(perRequest)
	sortModelPricing(perToken)
	sortModelPricing(unpriced)

	c.JSON(http.StatusOK, gin.H{
		"per_request": perRequest,
		"per_token":   perToken,
		"unpriced":    unpriced,
		"only_mapped": onlyMapped,
	})
}

// loadBillingRules loads enabled billing rules for the given groups.
func (h *ModelPricingHandler) loadBillingRules(ctx context.Context, authGroupID, userGroupID, defaultAuthGroupID, defaultUserGroupID uint64) ([]models.BillingRule, error) {
	if h.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if authGroupID == 0 || userGroupID == 0 {
		return []models.BillingRule{}, nil
	}

	q := h.db.WithContext(ctx).Model(&models.BillingRule{}).Where("is_enabled = ?", true)
	if defaultAuthGroupID != 0 && defaultUserGroupID != 0 && (defaultAuthGroupID != authGroupID || defaultUserGroupID != userGroupID) {
		q = q.Where("(auth_group_id = ? AND user_group_id = ?) OR (auth_group_id = ? AND user_group_id = ?)", authGroupID, userGroupID, defaultAuthGroupID, defaultUserGroupID)
	} else {
		q = q.Where("auth_group_id = ? AND user_group_id = ?", authGroupID, userGroupID)
	}
	var rules []models.BillingRule
	if errFind := q.Find(&rules).Error; errFind != nil {
		return nil, errFind
	}
	return rules, nil
}

// loadAvailableModels returns available models from DB mappings or registry.
func (h *ModelPricingHandler) loadAvailableModels(ctx context.Context, onlyMapped bool) ([]modelAvailability, error) {
	if onlyMapped {
		return h.loadMappedModels(ctx)
	}
	return h.loadRegistryModels()
}

// loadMappedModels loads models based on DB mappings.
func (h *ModelPricingHandler) loadMappedModels(ctx context.Context) ([]modelAvailability, error) {
	if h.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []models.ModelMapping
	if errFind := h.db.WithContext(ctx).
		Model(&models.ModelMapping{}).
		Select("provider", "model_name", "new_model_name", "is_enabled").
		Where("is_enabled = ?", true).
		Order("provider ASC, new_model_name ASC, model_name ASC").
		Find(&rows).Error; errFind != nil {
		return nil, errFind
	}

	seen := make(map[string]struct{})
	result := make([]modelAvailability, 0, len(rows))

	for _, row := range rows {
		newName := strings.TrimSpace(row.NewModelName)
		if newName == "" {
			continue
		}
		key := strings.ToLower(newName)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		provider := strings.ToLower(strings.TrimSpace(row.Provider))
		original := strings.TrimSpace(row.ModelName)
		display := newName

		if h.store != nil && provider != "" && original != "" {
			if info := h.store.GetByProviderModelID(provider, original); info != nil {
				if trimmed := strings.TrimSpace(info.DisplayName); trimmed != "" {
					display = trimmed
				}
			}
		}

		result = append(result, modelAvailability{
			Provider:      provider,
			ModelID:       newName,
			DisplayName:   display,
			OriginalModel: original,
		})
	}

	return result, nil
}

// loadRegistryModels loads models from the in-memory registry snapshot.
func (h *ModelPricingHandler) loadRegistryModels() ([]modelAvailability, error) {
	snapshot := make(map[string][]*sdkcliproxy.ModelInfo)
	if h.store != nil {
		if byProvider := h.store.SnapshotByProvider(); len(byProvider) > 0 {
			snapshot = byProvider
		}
	}

	result := make([]modelAvailability, 0, 32)
	seen := make(map[string]struct{})

	for provider, infos := range snapshot {
		for _, info := range infos {
			if info == nil {
				continue
			}
			id := strings.TrimSpace(info.ID)
			if id == "" {
				continue
			}
			key := strings.ToLower(provider) + "\x00" + strings.ToLower(id)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}

			display := strings.TrimSpace(info.DisplayName)
			if display == "" {
				display = id
			}

			result = append(result, modelAvailability{
				Provider:    strings.ToLower(strings.TrimSpace(provider)),
				ModelID:     id,
				DisplayName: display,
			})
		}
	}

	return result, nil
}

// sortModelPricing sorts pricing items by provider and model.
func sortModelPricing(list []modelPricingItem) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Provider == list[j].Provider {
			return strings.ToLower(list[i].Model) < strings.ToLower(list[j].Model)
		}
		return list[i].Provider < list[j].Provider
	})
}

// loadOnlyMapped reads the ONLY_MAPPED_MODELS flag from DB config.
func loadOnlyMapped() bool {
	raw, ok := internalsettings.DBConfigValue("ONLY_MAPPED_MODELS")
	if !ok {
		return false
	}
	return parseDBConfigBool(raw)
}

// parseDBConfigBool parses a boolean from JSON config payloads.
func parseDBConfigBool(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	var b bool
	if errUnmarshal := json.Unmarshal(raw, &b); errUnmarshal == nil {
		return b
	}
	var s string
	if errUnmarshal := json.Unmarshal(raw, &s); errUnmarshal == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return false
		}
		if strings.EqualFold(s, "true") || s == "1" {
			return true
		}
		return false
	}
	var n float64
	if errUnmarshal := json.Unmarshal(raw, &n); errUnmarshal == nil {
		return n != 0
	}
	// wrapper allows parsing values wrapped in a { "value": ... } object.
	var wrapper struct {
		Value json.RawMessage `json:"value"`
	}
	if errUnmarshal := json.Unmarshal(raw, &wrapper); errUnmarshal == nil {
		if len(wrapper.Value) > 0 {
			return parseDBConfigBool(wrapper.Value)
		}
	}
	return false
}
