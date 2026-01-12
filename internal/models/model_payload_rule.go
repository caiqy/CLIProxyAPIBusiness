package models

import (
	"time"

	"gorm.io/datatypes"
)

// ModelPayloadRule defines payload injection rules bound to a specific model mapping.
type ModelPayloadRule struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.

	ModelMappingID uint64         `gorm:"not null;uniqueIndex:idx_model_payload_rules_mapping"` // Owning model mapping ID.
	ModelMapping   *ModelMapping  `gorm:"constraint:OnDelete:CASCADE;OnUpdate:CASCADE"`         // Related model mapping.
	Protocol       string         `gorm:"type:varchar(32);index"`                               // Protocol name.
	Params         datatypes.JSON `gorm:"type:jsonb;not null"`                                  // Injection parameters.
	IsEnabled      bool           `gorm:"not null;default:true;index"`                          // Whether rule is active.
	Description    string         `gorm:"type:text"`                                            // Human-readable description.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"` // Last update timestamp.
}
