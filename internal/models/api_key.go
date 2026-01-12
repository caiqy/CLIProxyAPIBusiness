package models

import "time"

// APIKey represents an API key issued to a user or admin.
type APIKey struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.

	UserID *uint64 `gorm:"index"`             // Owning user ID when bound to a user.
	User   *User   `gorm:"foreignKey:UserID"` // Associated user record.

	Name   string `gorm:"type:text;not null"`             // Display name for the key.
	APIKey string `gorm:"type:text;not null;uniqueIndex"` // Full API key string.

	IsAdmin bool `gorm:"not null;default:false"` // Marks admin-issued keys.

	Active     bool       `gorm:"not null;default:true"` // Whether the key is enabled.
	ExpiresAt  *time.Time // Optional expiration timestamp.
	RevokedAt  *time.Time // Revocation timestamp when disabled.
	LastUsedAt *time.Time // Last successful usage time.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"` // Last update timestamp.
}

// Status returns the current key status based on revocation, expiry window, and active flag.
func (k *APIKey) Status() string {
	if k.RevokedAt != nil {
		return "revoked"
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now().AddDate(0, 0, 7)) {
		return "expiring"
	}
	if k.Active {
		return "active"
	}
	return "inactive"
}
