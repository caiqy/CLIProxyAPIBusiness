package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PrepaidCardFrontHandler handles prepaid card endpoints for users.
type PrepaidCardFrontHandler struct {
	db *gorm.DB
}

// NewPrepaidCardFrontHandler constructs a PrepaidCardFrontHandler.
func NewPrepaidCardFrontHandler(db *gorm.DB) *PrepaidCardFrontHandler {
	return &PrepaidCardFrontHandler{db: db}
}

// prepaidCardDTO defines the prepaid card response payload.
type prepaidCardDTO struct {
	ID          uint64     `json:"id"`
	Name        string     `json:"name"`
	CardSN      string     `json:"card_sn"`
	Amount      float64    `json:"amount"`
	Balance     float64    `json:"balance"`
	ValidDays   int        `json:"valid_days"`
	ExpiresAt   *time.Time `json:"expires_at"`
	RedeemedAt  *time.Time `json:"redeemed_at"`
	RedeemedUid *uint64    `json:"redeemed_user_id,omitempty"`
}

// redeemCardRequest defines the request body for card redemption.
type redeemCardRequest struct {
	CardSN   string `json:"card_sn"`
	Password string `json:"password"`
}

// Redeem redeems a prepaid card for the current user.
func (h *PrepaidCardFrontHandler) Redeem(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var body redeemCardRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	cardSN := strings.TrimSpace(body.CardSN)
	password := strings.TrimSpace(body.Password)
	if cardSN == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "card_sn and password are required"})
		return
	}

	var result gin.H
	errTx := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var card models.PrepaidCard
		if errFind := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("card_sn = ?", cardSN).
			First(&card).Error; errFind != nil {
			if errors.Is(errFind, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "card not found"})
				return errFind
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query card failed"})
			return errFind
		}

		if card.Password != password {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid password"})
			return errors.New("invalid password")
		}
		if !card.IsEnabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "card is disabled"})
			return errors.New("card disabled")
		}
		if card.RedeemedUserID != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "card already redeemed"})
			return errors.New("card redeemed")
		}

		now := time.Now().UTC()
		var expiresAt *time.Time
		if card.ValidDays > 0 {
			exp := now.AddDate(0, 0, card.ValidDays)
			expiresAt = &exp
		}
		if errUpdate := tx.Model(&card).Updates(map[string]any{
			"redeemed_user_id": userID,
			"redeemed_at":      now,
			"expires_at":       expiresAt,
		}).Error; errUpdate != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "redeem failed"})
			return errUpdate
		}

		card.RedeemedUserID = &userID
		card.RedeemedAt = &now
		card.ExpiresAt = expiresAt
		result = gin.H{
			"id":          card.ID,
			"name":        card.Name,
			"card_sn":     card.CardSN,
			"amount":      card.Amount,
			"balance":     card.Balance,
			"valid_days":  card.ValidDays,
			"expires_at":  card.ExpiresAt,
			"redeemed_at": card.RedeemedAt,
		}
		return nil
	})
	if errTx != nil {
		// response already written inside transaction on error paths
		return
	}

	c.JSON(http.StatusOK, gin.H{"card": result})
}

// GetCurrent returns the most recently redeemed card for the user.
func (h *PrepaidCardFrontHandler) GetCurrent(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var card models.PrepaidCard
	if errFind := h.db.WithContext(c.Request.Context()).
		Where("redeemed_user_id = ?", userID).
		Order("redeemed_at DESC NULLS LAST, created_at DESC").
		First(&card).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusOK, gin.H{"card": nil})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query card failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"card": gin.H{
			"id":          card.ID,
			"name":        card.Name,
			"card_sn":     card.CardSN,
			"amount":      card.Amount,
			"balance":     card.Balance,
			"valid_days":  card.ValidDays,
			"expires_at":  card.ExpiresAt,
			"redeemed_at": card.RedeemedAt,
		},
	})
}

// List returns all redeemed prepaid cards for the user.
func (h *PrepaidCardFrontHandler) List(c *gin.Context) {
	userID := getUserID(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var cards []models.PrepaidCard
	if errFind := h.db.WithContext(c.Request.Context()).
		Where("redeemed_user_id = ?", userID).
		Order("redeemed_at DESC NULLS LAST, created_at DESC").
		Find(&cards).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query cards failed"})
		return
	}

	resp := make([]prepaidCardDTO, 0, len(cards))
	for _, citem := range cards {
		resp = append(resp, prepaidCardDTO{
			ID:          citem.ID,
			Name:        citem.Name,
			CardSN:      citem.CardSN,
			Amount:      citem.Amount,
			Balance:     citem.Balance,
			ValidDays:   citem.ValidDays,
			ExpiresAt:   citem.ExpiresAt,
			RedeemedAt:  citem.RedeemedAt,
			RedeemedUid: citem.RedeemedUserID,
		})
	}

	c.JSON(http.StatusOK, gin.H{"cards": resp})
}
