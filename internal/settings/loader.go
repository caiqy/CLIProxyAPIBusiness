package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// RefreshDBConfigSnapshot reloads all settings from the database and updates the in-memory snapshot.
//
// This is required at process startup; otherwise DBConfigValue() will return empty values until
// an admin updates settings via the API (which triggers refresh).
func RefreshDBConfigSnapshot(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errors.New("settings: nil db")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var rows []models.Setting
	if errFind := db.WithContext(ctx).
		Select("key", "value", "updated_at").
		Order("key ASC").
		Find(&rows).Error; errFind != nil {
		return errFind
	}

	values := make(map[string]json.RawMessage, len(rows))
	maxUpdatedAt := time.Time{}
	maxUpdatedKey := ""
	for _, row := range rows {
		key := strings.TrimSpace(row.Key)
		if key == "" {
			continue
		}
		values[key] = row.Value
		rowUpdatedAt := row.UpdatedAt.UTC()
		if rowUpdatedAt.After(maxUpdatedAt) || (rowUpdatedAt.Equal(maxUpdatedAt) && key > maxUpdatedKey) {
			maxUpdatedAt = rowUpdatedAt
			maxUpdatedKey = key
		}
	}

	StoreDBConfig(maxUpdatedAt, values)
	return nil
}
