package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ImportResult reports how many rules were created or updated.
type ImportResult struct {
	Created int
	Updated int
}

// ImportFromModelMappings imports billing rules from enabled model mappings.
func ImportFromModelMappings(ctx context.Context, db *gorm.DB, authGroupID, userGroupID uint64, billingType models.BillingType) (ImportResult, error) {
	result := ImportResult{}
	if db == nil {
		return result, fmt.Errorf("import billing rules: nil db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if authGroupID == 0 {
		return result, fmt.Errorf("import billing rules: auth_group_id is required")
	}
	if userGroupID == 0 {
		return result, fmt.Errorf("import billing rules: user_group_id is required")
	}
	if billingType != models.BillingTypePerRequest && billingType != models.BillingTypePerToken {
		return result, fmt.Errorf("import billing rules: invalid billing type")
	}

	var mappings []models.ModelMapping
	if errFind := db.WithContext(ctx).Where("is_enabled = ?", true).Find(&mappings).Error; errFind != nil {
		return result, fmt.Errorf("import billing rules: load model mappings: %w", errFind)
	}
	if len(mappings) == 0 {
		return result, nil
	}

	var modelRefs []models.ModelReference
	if errRefs := db.WithContext(ctx).Find(&modelRefs).Error; errRefs != nil {
		return result, fmt.Errorf("import billing rules: load model references: %w", errRefs)
	}

	refByModelID := make(map[string]*models.ModelReference, len(modelRefs))
	refByModelName := make(map[string]*models.ModelReference, len(modelRefs))
	for i := range modelRefs {
		if key := strings.TrimSpace(modelRefs[i].ModelID); key != "" {
			refByModelID[key] = &modelRefs[i]
		}
		if key := strings.TrimSpace(modelRefs[i].ModelName); key != "" {
			refByModelName[key] = &modelRefs[i]
		}
	}

	now := time.Now().UTC()
	for _, mapping := range mappings {
		provider := strings.ToLower(strings.TrimSpace(mapping.Provider))
		model := strings.TrimSpace(mapping.NewModelName)
		if provider == "" || model == "" {
			continue
		}

		var exists bool
		var existing models.BillingRule
		errExist := db.WithContext(ctx).
			Select("id").
			Where("auth_group_id = ? AND user_group_id = ? AND provider = ? AND model = ?", authGroupID, userGroupID, provider, model).
			First(&existing).Error
		if errExist == nil {
			exists = true
		} else if errExist != nil && !errors.Is(errExist, gorm.ErrRecordNotFound) {
			return result, fmt.Errorf("import billing rules: find existing rule: %w", errExist)
		}

		var pricePerRequest *float64
		var priceInputToken, priceOutputToken, priceCacheCreate, priceCacheRead *float64

		if billingType == models.BillingTypePerToken {
			var ref *models.ModelReference
			if r, ok := refByModelID[model]; ok {
				ref = r
			} else if r, ok := refByModelName[model]; ok {
				ref = r
			} else if baseModel := strings.TrimSpace(mapping.ModelName); baseModel != "" {
				if r, ok := refByModelID[baseModel]; ok {
					ref = r
				} else if r, ok := refByModelName[baseModel]; ok {
					ref = r
				}
			}

			if ref != nil {
				priceInputToken = ref.InputPrice
				priceOutputToken = ref.OutputPrice
				priceCacheCreate = ref.CacheWritePrice
				priceCacheRead = ref.CacheReadPrice
			} else {
				zero := float64(0)
				priceInputToken = &zero
				priceOutputToken = &zero
				priceCacheCreate = &zero
				priceCacheRead = &zero
			}
		} else {
			zero := float64(0)
			pricePerRequest = &zero
		}

		rule := models.BillingRule{
			AuthGroupID:           authGroupID,
			UserGroupID:           userGroupID,
			Provider:              provider,
			Model:                 model,
			BillingType:           billingType,
			PricePerRequest:       pricePerRequest,
			PriceInputToken:       priceInputToken,
			PriceOutputToken:      priceOutputToken,
			PriceCacheCreateToken: priceCacheCreate,
			PriceCacheReadToken:   priceCacheRead,
			IsEnabled:             true,
			CreatedAt:             now,
			UpdatedAt:             now,
		}

		if errUpsert := db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "auth_group_id"},
				{Name: "user_group_id"},
				{Name: "provider"},
				{Name: "model"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"billing_type":             billingType,
				"price_per_request":        pricePerRequest,
				"price_input_token":        priceInputToken,
				"price_output_token":       priceOutputToken,
				"price_cache_create_token": priceCacheCreate,
				"price_cache_read_token":   priceCacheRead,
				"is_enabled":               true,
				"updated_at":               now,
			}),
		}).Create(&rule).Error; errUpsert != nil {
			return result, fmt.Errorf("import billing rules: upsert rule: %w", errUpsert)
		}

		if exists {
			result.Updated++
		} else {
			result.Created++
		}
	}

	return result, nil
}
