package models

import "time"

// UserModelAuthBinding stores sticky auth bindings for a user and a model mapping.
type UserModelAuthBinding struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.

	UserID         uint64 `gorm:"not null;uniqueIndex:idx_user_model_auth_bindings_user_model,priority:1"`       // Bound user ID.
	ModelMappingID uint64 `gorm:"not null;uniqueIndex:idx_user_model_auth_bindings_user_model,priority:2;index"` // Bound model mapping ID.
	AuthIndex      string `gorm:"type:varchar(64);not null"`                                                     // Bound auth index.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"` // Last update timestamp.
}
