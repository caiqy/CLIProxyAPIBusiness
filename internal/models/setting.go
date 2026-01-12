package models

import (
	"encoding/json"
	"time"
)

// Setting stores a key/value configuration entry in the database.
type Setting struct {
	Key       string          `gorm:"type:varchar(255);primaryKey"`                      // Configuration key.
	Value     json.RawMessage `gorm:"type:jsonb"`                                        // JSON-encoded value.
	UpdatedAt time.Time       `gorm:"not null;autoUpdateTime;default:CURRENT_TIMESTAMP"` // Last update timestamp.
}
