package billing

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// ResolveDefaultAuthGroupID returns the default auth group ID, if any.
func ResolveDefaultAuthGroupID(ctx context.Context, db *gorm.DB) (*uint64, error) {
	if db == nil {
		return nil, nil
	}

	var group models.AuthGroup
	if errFind := db.WithContext(ctx).
		Where("is_default = ?", true).
		Order("id ASC").
		First(&group).Error; errFind == nil {
		return &group.ID, nil
	} else if !errors.Is(errFind, gorm.ErrRecordNotFound) {
		return nil, errFind
	}

	var anyGroup models.AuthGroup
	if errFindAny := db.WithContext(ctx).
		Select("id").
		Order("id ASC").
		Limit(1).
		First(&anyGroup).Error; errFindAny != nil {
		if errors.Is(errFindAny, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errFindAny
	}

	return &anyGroup.ID, nil
}

// ResolveDefaultUserGroupID returns the default user group ID, if any.
func ResolveDefaultUserGroupID(ctx context.Context, db *gorm.DB) (*uint64, error) {
	if db == nil {
		return nil, nil
	}

	var group models.UserGroup
	if errFind := db.WithContext(ctx).
		Where("is_default = ?", true).
		Order("id ASC").
		First(&group).Error; errFind == nil {
		return &group.ID, nil
	} else if !errors.Is(errFind, gorm.ErrRecordNotFound) {
		return nil, errFind
	}

	var anyGroup models.UserGroup
	if errFindAny := db.WithContext(ctx).
		Select("id").
		Order("id ASC").
		Limit(1).
		First(&anyGroup).Error; errFindAny != nil {
		if errors.Is(errFindAny, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errFindAny
	}

	return &anyGroup.ID, nil
}

// SelectBillingRule matches billing rules using the following priority:
// 1) authGroup + userGroup + provider + model
// 2) authGroup + userGroup (provider/model are empty)
// 3) default authGroup + default userGroup (provider/model exact, then empty)
func SelectBillingRule(
	rules []models.BillingRule,
	authGroupID, userGroupID uint64,
	defaultAuthGroupID, defaultUserGroupID uint64,
	provider, model string,
) *models.BillingRule {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)

	bestPriority := -1
	bestUpdatedAt := time.Time{}
	var best *models.BillingRule

	consider := func(r *models.BillingRule, priority int) {
		if r == nil {
			return
		}
		if priority > bestPriority {
			bestPriority = priority
			bestUpdatedAt = r.UpdatedAt
			best = r
			return
		}
		if priority < bestPriority || best == nil {
			return
		}
		if r.UpdatedAt.After(bestUpdatedAt) {
			bestUpdatedAt = r.UpdatedAt
			best = r
			return
		}
		if r.UpdatedAt.Equal(bestUpdatedAt) && r.ID > best.ID {
			best = r
		}
	}

	for i := range rules {
		r := &rules[i]
		if !r.IsEnabled {
			continue
		}

		rProvider := strings.ToLower(strings.TrimSpace(r.Provider))
		rModel := strings.TrimSpace(r.Model)

		if authGroupID != 0 && userGroupID != 0 && r.AuthGroupID == authGroupID && r.UserGroupID == userGroupID {
			if rProvider == provider && rModel == model {
				consider(r, 3)
				continue
			}
			if rProvider == "" && rModel == "" {
				consider(r, 2)
				continue
			}
		}

		if defaultAuthGroupID != 0 && defaultUserGroupID != 0 && r.AuthGroupID == defaultAuthGroupID && r.UserGroupID == defaultUserGroupID {
			if rProvider == provider && rModel == model {
				consider(r, 1)
				continue
			}
			if rProvider == "" && rModel == "" {
				consider(r, 0)
				continue
			}
		}
	}

	return best
}
