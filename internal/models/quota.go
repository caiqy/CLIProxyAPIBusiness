package models

import (
	"time"

	"gorm.io/datatypes"
)

// Quota stores quota payloads scoped to an auth entry.
type Quota struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.

	AuthID uint64 `gorm:"not null;index"`                       // Related auth ID.
	Type   string `gorm:"column:type;type:text;not null;index"` // Auth content type.

	Data datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'"` // Quota payload.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"` // Last update timestamp.
}

// TableName overrides the default table name.
func (Quota) TableName() string {
	return "quota"
}
