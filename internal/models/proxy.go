package models

import "time"

// Proxy represents an upstream proxy endpoint.
type Proxy struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.
	ProxyURL string `gorm:"type:text;not null"`       // Proxy URL.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"` // Last update timestamp.
}
