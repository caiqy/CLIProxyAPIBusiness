package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// ModelReferenceHandler manages admin queries for model reference pricing.
type ModelReferenceHandler struct {
	db *gorm.DB // Database handle for model references.
}

// NewModelReferenceHandler constructs a model reference handler.
func NewModelReferenceHandler(db *gorm.DB) *ModelReferenceHandler {
	return &ModelReferenceHandler{db: db}
}

// GetPrice returns model reference pricing for a model (and optional provider).
func (h *ModelReferenceHandler) GetPrice(c *gin.Context) {
	modelID := strings.TrimSpace(c.Query("model_id"))
	if modelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id is required"})
		return
	}
	provider := strings.TrimSpace(c.Query("provider"))

	var ref models.ModelReference
	if provider != "" {
		errFind := h.db.WithContext(c.Request.Context()).
			Where("provider_name = ? AND model_id = ?", provider, modelID).
			First(&ref).Error
		if errFind == nil {
			c.JSON(http.StatusOK, formatModelReferencePrice(&ref))
			return
		}
		if !errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query model reference failed"})
			return
		}
	}

	errFind := h.db.WithContext(c.Request.Context()).
		Where("model_id = ?", modelID).
		Order("provider_name ASC").
		First(&ref).Error
	if errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			if errFallback := h.db.WithContext(c.Request.Context()).
				Where("model_name = ?", modelID).
				Order("provider_name ASC").
				First(&ref).Error; errFallback != nil {
				if errors.Is(errFallback, gorm.ErrRecordNotFound) {
					c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "query model reference failed"})
				return
			}
			if strings.TrimSpace(ref.ModelID) == "" {
				_ = h.db.WithContext(c.Request.Context()).
					Model(&models.ModelReference{}).
					Where("provider_name = ? AND model_name = ?", ref.ProviderName, ref.ModelName).
					Update("model_id", modelID).Error
				ref.ModelID = modelID
			}
			c.JSON(http.StatusOK, formatModelReferencePrice(&ref))
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query model reference failed"})
		return
	}

	c.JSON(http.StatusOK, formatModelReferencePrice(&ref))
}

func formatModelReferencePrice(ref *models.ModelReference) gin.H {
	modelValue := ref.ModelID
	if strings.TrimSpace(modelValue) == "" {
		modelValue = ref.ModelName
	}
	return gin.H{
		"provider":                 ref.ProviderName,
		"model":                    modelValue,
		"price_input_token":        ref.InputPrice,
		"price_output_token":       ref.OutputPrice,
		"price_cache_create_token": ref.CacheWritePrice,
		"price_cache_read_token":   ref.CacheReadPrice,
	}
}
