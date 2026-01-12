package models

import (
	"time"

	"gorm.io/datatypes"
)

// Admin represents an administrator account stored in the database.
type Admin struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.

	Username string `gorm:"type:text;not null;uniqueIndex"` // Unique login name.
	Password string `gorm:"type:text;not null"`             // Hashed password.

	Active bool `gorm:"not null;default:true"` // Whether the admin can sign in.

	IsSuperAdmin bool `gorm:"not null;default:false"` // Grants all permissions when true.

	Permissions datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'"` // Permission keys in JSON.

	TOTPSecret            string  `gorm:"type:text"`    // TOTP secret for MFA.
	PasskeyID             []byte  `gorm:"type:bytea"`   // WebAuthn credential ID.
	PasskeyPublicKey      []byte  `gorm:"type:bytea"`   // WebAuthn public key bytes.
	PasskeySignCount      *uint32 `gorm:"type:bigint"`  // WebAuthn signature counter.
	PasskeyBackupEligible *bool   `gorm:"type:boolean"` // WebAuthn backup eligibility flag.
	PasskeyBackupState    *bool   `gorm:"type:boolean"` // WebAuthn backup state flag.

	CreatedAt time.Time `gorm:"not null;autoCreateTime"` // Creation timestamp.
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime"` // Last update timestamp.
}
