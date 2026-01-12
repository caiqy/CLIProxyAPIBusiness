package models

import "time"

// BillingType defines how costs are calculated.
type BillingType int

// BillingType constants define pricing strategies.
const (
	// BillingTypePerRequest charges per request.
	BillingTypePerRequest BillingType = 1
	// BillingTypePerToken charges per token.
	BillingTypePerToken BillingType = 2
)

// BillingRule defines pricing and applicability for a provider/model pair.
type BillingRule struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.

	AuthGroupID uint64 `gorm:"not null;index"`     // Auth group scope.
	UserGroupID uint64 `gorm:"not null;index"`     // User group scope.
	Provider    string `gorm:"varchar(255);index"` // Provider name filter.
	Model       string `gorm:"varchar(255);index"` // Model name filter.

	BillingType BillingType `gorm:"not null"` // Billing strategy.

	PricePerRequest       *float64 `gorm:"type:decimal(20,10)"` // Request-level price.
	PriceInputToken       *float64 `gorm:"type:decimal(20,10)"` // Input token price.
	PriceOutputToken      *float64 `gorm:"type:decimal(20,10)"` // Output token price.
	PriceCacheCreateToken *float64 `gorm:"type:decimal(20,10)"` // Cache create token price.
	PriceCacheReadToken   *float64 `gorm:"type:decimal(20,10)"` // Cache read token price.

	IsEnabled bool `gorm:"not null;default:true"` // Whether the rule is active.

	AuthGroup AuthGroup `gorm:"foreignKey:AuthGroupID"` // Auth group relation.
	UserGroup UserGroup `gorm:"foreignKey:UserGroupID"` // User group relation.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"` // Last update timestamp.
}
