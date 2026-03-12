package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

const (
	defaultStartupImportWaitTimeout  = 60 * time.Second
	defaultStartupImportPollInterval = 2 * time.Second
)

// AutoImportDefaultGroupOnce waits for model references and imports billing rules for default groups.
func AutoImportDefaultGroupOnce(ctx context.Context, db *gorm.DB, waitTimeout, pollInterval time.Duration) error {
	if db == nil {
		return fmt.Errorf("auto import default billing rules: nil db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if waitTimeout <= 0 {
		waitTimeout = defaultStartupImportWaitTimeout
	}
	if pollInterval <= 0 {
		pollInterval = defaultStartupImportPollInterval
	}

	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()

	for {
		ready, errReady := modelReferencesReady(waitCtx, db)
		if errReady != nil {
			return fmt.Errorf("auto import default billing rules: check model references: %w", errReady)
		}
		if ready {
			break
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("auto import default billing rules: wait model references: %w", waitCtx.Err())
		case <-time.After(pollInterval):
		}
	}

	authGroupID, errAuthGroup := ResolveDefaultAuthGroupID(waitCtx, db)
	if errAuthGroup != nil {
		return fmt.Errorf("auto import default billing rules: resolve default auth group: %w", errAuthGroup)
	}
	if authGroupID == nil || *authGroupID == 0 {
		return fmt.Errorf("auto import default billing rules: default auth group not found")
	}

	userGroupID, errUserGroup := ResolveDefaultUserGroupID(waitCtx, db)
	if errUserGroup != nil {
		return fmt.Errorf("auto import default billing rules: resolve default user group: %w", errUserGroup)
	}
	if userGroupID == nil || *userGroupID == 0 {
		return fmt.Errorf("auto import default billing rules: default user group not found")
	}

	if _, errImport := ImportFromModelMappings(waitCtx, db, *authGroupID, *userGroupID, models.BillingTypePerToken); errImport != nil {
		return fmt.Errorf("auto import default billing rules: import from model mappings: %w", errImport)
	}

	return nil
}

func modelReferencesReady(ctx context.Context, db *gorm.DB) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("nil db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var count int64
	if errCount := db.WithContext(ctx).Model(&models.ModelReference{}).Limit(1).Count(&count).Error; errCount != nil {
		return false, errCount
	}
	return count > 0, nil
}
