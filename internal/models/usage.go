package models

import (
	"time"
)

// Usage records metering data for a single request.
type Usage struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.

	Provider string `gorm:"type:text;not null;index"` // Provider name.
	Model    string `gorm:"type:text;not null;index"` // Model name.

	UserID   *uint64 `gorm:"index"` // Related user ID.
	APIKeyID *uint64 `gorm:"index"` // Related API key ID.
	AuthID   *uint64 `gorm:"index"` // Related auth ID.

	AuthKey   string `gorm:"type:text;index"` // Auth key value.
	AuthIndex string `gorm:"type:text"`       // Auth index identifier.
	Source    string `gorm:"type:text"`       // Usage source marker.

	RequestedAt time.Time `gorm:"not null;index"`         // Request timestamp.
	Failed      bool      `gorm:"not null;default:false"` // Failure flag.

	InputTokens     int64 `gorm:"not null;default:0"` // Input token count.
	OutputTokens    int64 `gorm:"not null;default:0"` // Output token count.
	ReasoningTokens int64 `gorm:"not null;default:0"` // Reasoning token count.
	CachedTokens    int64 `gorm:"not null;default:0"` // Cached token count.
	TotalTokens     int64 `gorm:"not null;default:0"` // Total token count.

	CostMicros int64 `gorm:"not null;default:0"` // Cost in micros.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
}
