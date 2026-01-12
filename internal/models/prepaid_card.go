package models

import "time"

// PrepaidCard represents a prepaid balance card.
type PrepaidCard struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"` // Primary key.

	Name      string     `gorm:"type:text;not null"`                     // Card display name.
	CardSN    string     `gorm:"type:text;not null;uniqueIndex"`         // Unique card serial number.
	Password  string     `gorm:"type:text;not null"`                     // Redemption password.
	Amount    float64    `gorm:"type:decimal(20,10);not null"`           // Original card value.
	Balance   float64    `gorm:"type:decimal(20,10);not null;default:0"` // Remaining balance.
	ValidDays int        `gorm:"not null;default:0"`                     // Validity window in days.
	ExpiresAt *time.Time // Expiration time, if any.

	IsEnabled bool `gorm:"not null;default:true"` // Whether the card can be redeemed.

	RedeemedUserID *uint64 `gorm:"index"`                     // User who redeemed the card.
	RedeemedUser   *User   `gorm:"foreignKey:RedeemedUserID"` // Redeeming user record.

	CreatedAt  time.Time  `gorm:"not null;autoCreateTime"` // Creation timestamp.
	RedeemedAt *time.Time // Redemption time, if redeemed.
}
