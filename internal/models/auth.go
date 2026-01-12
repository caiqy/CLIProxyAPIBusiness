package models

import (
	"time"

	"gorm.io/datatypes"
)

// Auth stores an authentication entry and its content for relay usage.
type Auth struct {
	ID  uint64 `gorm:"primaryKey;autoIncrement"`       // Primary key.
	Key string `gorm:"type:text;not null;uniqueIndex"` // Unique auth key.

	ProxyURL string `gorm:"type:text"` // Optional proxy override.

	AuthGroupID *uint64    `gorm:"index"`                  // Owning auth group ID.
	AuthGroup   *AuthGroup `gorm:"foreignKey:AuthGroupID"` // Owning auth group.

	Content datatypes.JSON `gorm:"type:jsonb;not null"` // Auth payload content.

	IsAvailable bool `gorm:"type:boolean;not null;default:true"` // Availability flag.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"` // Last update timestamp.
}
