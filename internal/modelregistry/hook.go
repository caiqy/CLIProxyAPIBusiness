package modelregistry

import (
	"context"
	"strings"
	"time"

	sdkcliproxy "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Hook tracks model registrations and mirrors them into the database/store.
type Hook struct {
	db    *gorm.DB
	store *Store
}

// NewHook constructs a Hook with an optional store.
func NewHook(db *gorm.DB, store *Store) *Hook {
	if store == nil {
		store = NewStore()
	}
	return &Hook{db: db, store: store}
}

// Store returns the in-memory model registry store.
func (h *Hook) Store() *Store {
	if h == nil {
		return nil
	}
	return h.store
}

// OnModelsRegistered caches model infos and seeds DB mappings when needed.
func (h *Hook) OnModelsRegistered(ctx context.Context, provider, clientID string, infos []*sdkcliproxy.ModelInfo) {
	if h == nil {
		return
	}
	if h.store != nil {
		h.store.Upsert(provider, clientID, infos)
	}
	if h.db == nil {
		return
	}

	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	if normalizedProvider == "" {
		return
	}

	uniqueModels := make(map[string]string)
	for _, info := range infos {
		if info == nil {
			continue
		}
		modelName := strings.TrimSpace(info.ID)
		if modelName == "" {
			continue
		}
		modelKey := strings.ToLower(modelName)
		if _, exists := uniqueModels[modelKey]; exists {
			continue
		}
		uniqueModels[modelKey] = modelName
	}

	if len(uniqueModels) == 0 {
		return
	}

	lowerNames := make([]string, 0, len(uniqueModels))
	for modelKey := range uniqueModels {
		lowerNames = append(lowerNames, modelKey)
	}

	var existing []models.ModelMapping
	errFind := h.db.WithContext(ctx).
		Model(&models.ModelMapping{}).
		Select("provider", "model_name").
		Where("provider = ?", normalizedProvider).
		Where("LOWER(model_name) IN ?", lowerNames).
		Find(&existing).Error
	if errFind != nil {
		log.WithError(errFind).Warn("model registry hook load existing model mappings failed")
		return
	}

	existingKeys := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		existingProvider := strings.ToLower(strings.TrimSpace(row.Provider))
		existingModel := strings.ToLower(strings.TrimSpace(row.ModelName))
		if existingProvider == "" || existingModel == "" {
			continue
		}
		existingKeys[existingProvider+"\x00"+existingModel] = struct{}{}
	}

	now := time.Now().UTC()
	toCreate := make([]models.ModelMapping, 0)
	for modelKey, modelName := range uniqueModels {
		if _, exists := existingKeys[normalizedProvider+"\x00"+modelKey]; exists {
			continue
		}
		toCreate = append(toCreate, models.ModelMapping{
			Provider:     normalizedProvider,
			ModelName:    modelName,
			NewModelName: modelName,
			IsEnabled:    true,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}

	if len(toCreate) == 0 {
		return
	}

	if errCreate := h.db.WithContext(ctx).Create(&toCreate).Error; errCreate != nil {
		log.WithError(errCreate).Warn("model registry hook create model mappings failed")
	}
}

// OnModelsUnregistered removes model infos from the in-memory store.
func (h *Hook) OnModelsUnregistered(ctx context.Context, provider, clientID string) {
	if h == nil {
		return
	}
	if h.store != nil {
		h.store.Remove(provider, clientID)
	}
}
