package handlers

import (
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	dbutil "github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

// PrepaidCardHandler handles admin operations for prepaid cards.
type PrepaidCardHandler struct {
	db *gorm.DB // Database handle for prepaid card queries.
}

// NewPrepaidCardHandler wires a prepaid card handler with its database dependency.
func NewPrepaidCardHandler(db *gorm.DB) *PrepaidCardHandler {
	return &PrepaidCardHandler{db: db}
}

// createPrepaidCardRequest captures the payload for creating a single card.
type createPrepaidCardRequest struct {
	Name      string  `json:"name"`       // Display name for the card.
	CardSN    string  `json:"card_sn"`    // Card serial number.
	Password  string  `json:"password"`   // Redemption password.
	Amount    float64 `json:"amount"`     // Initial amount and balance.
	ValidDays *int    `json:"valid_days"` // Optional validity period in days.
	IsEnabled *bool   `json:"is_enabled"` // Optional active flag.
}

// Create validates input and persists a new prepaid card with initial balance.
func (h *PrepaidCardHandler) Create(c *gin.Context) {
	var body createPrepaidCardRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing name"})
		return
	}
	cardSN := strings.TrimSpace(body.CardSN)
	if cardSN == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing card_sn"})
		return
	}
	password := strings.TrimSpace(body.Password)
	if password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing password"})
		return
	}
	if body.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be positive"})
		return
	}
	validDays := 0
	if body.ValidDays != nil {
		if *body.ValidDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid_days cannot be negative"})
			return
		}
		validDays = *body.ValidDays
	}

	now := time.Now().UTC()
	isEnabled := true
	if body.IsEnabled != nil {
		isEnabled = *body.IsEnabled
	}
	card := models.PrepaidCard{
		Name:      name,
		CardSN:    cardSN,
		Password:  password,
		Amount:    body.Amount,
		Balance:   body.Amount,
		ValidDays: validDays,
		IsEnabled: isEnabled,
		CreatedAt: now,
	}
	if errCreate := h.db.WithContext(c.Request.Context()).Create(&card).Error; errCreate != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create prepaid card failed"})
		return
	}
	c.JSON(http.StatusCreated, h.formatCard(&card))
}

// batchCreatePrepaidCardRequest captures the payload for batch card creation.
type batchCreatePrepaidCardRequest struct {
	Name           string  `json:"name"`            // Display name for the cards.
	Amount         float64 `json:"amount"`          // Amount to assign to each card.
	Count          int     `json:"count"`           // Number of cards to create.
	CardSNPrefix   string  `json:"card_sn_prefix"`  // Optional card serial prefix.
	PasswordLength int     `json:"password_length"` // Length of generated passwords.
	ValidDays      *int    `json:"valid_days"`      // Optional validity period in days.
	IsEnabled      *bool   `json:"is_enabled"`      // Optional active flag.
}

// BatchCreate generates multiple prepaid cards in a single transaction.
func (h *PrepaidCardHandler) BatchCreate(c *gin.Context) {
	var body batchCreatePrepaidCardRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing name"})
		return
	}
	if body.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be positive"})
		return
	}
	if body.Count <= 0 || body.Count > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "count must be between 1 and 1000"})
		return
	}

	passwordLength := body.PasswordLength
	if passwordLength <= 0 {
		passwordLength = 10
	}
	if passwordLength < 6 || passwordLength > 32 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password_length must be between 6 and 32"})
		return
	}
	validDays := 0
	if body.ValidDays != nil {
		if *body.ValidDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid_days cannot be negative"})
			return
		}
		validDays = *body.ValidDays
	}

	prefix := strings.TrimSpace(body.CardSNPrefix)
	isEnabled := true
	if body.IsEnabled != nil {
		isEnabled = *body.IsEnabled
	}
	now := time.Now().UTC()
	created := make([]gin.H, 0, body.Count)
	errTx := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		for i := 0; i < body.Count; i++ {
			cardSN, errSN := generateCode(16)
			if errSN != nil {
				return errSN
			}
			password, errPass := generateCode(passwordLength)
			if errPass != nil {
				return errPass
			}
			card := models.PrepaidCard{
				Name:      name,
				CardSN:    prefix + cardSN,
				Password:  password,
				Amount:    body.Amount,
				Balance:   body.Amount,
				ValidDays: validDays,
				IsEnabled: isEnabled,
				CreatedAt: now,
			}
			if errCreate := tx.Create(&card).Error; errCreate != nil {
				return errCreate
			}
			created = append(created, h.formatCard(&card))
		}
		return nil
	})
	if errTx != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "batch create prepaid cards failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"prepaid_cards": created})
}

// List returns prepaid cards filtered by query parameters.
func (h *PrepaidCardHandler) List(c *gin.Context) {
	var (
		nameQ         = strings.TrimSpace(c.Query("name"))
		cardSNQ       = strings.TrimSpace(c.Query("card_sn"))
		redeemedQ     = strings.TrimSpace(c.Query("redeemed"))
		redeemedUserQ = strings.TrimSpace(c.Query("redeemed_user"))
	)

	q := h.db.WithContext(c.Request.Context()).
		Model(&models.PrepaidCard{}).
		Preload("RedeemedUser")
	if nameQ != "" {
		pattern := dbutil.NormalizeLikePattern(h.db, "%"+nameQ+"%")
		q = q.Where(dbutil.CaseInsensitiveLikeExpr(h.db, "name"), pattern)
	}
	if cardSNQ != "" {
		pattern := dbutil.NormalizeLikePattern(h.db, "%"+cardSNQ+"%")
		q = q.Where(dbutil.CaseInsensitiveLikeExpr(h.db, "card_sn"), pattern)
	}
	if redeemedUserQ != "" {
		pattern := dbutil.NormalizeLikePattern(h.db, "%"+redeemedUserQ+"%")
		q = q.Joins("LEFT JOIN users ON users.id = prepaid_cards.redeemed_user_id").
			Where(dbutil.CaseInsensitiveLikeExpr(h.db, "users.username"), pattern)
	}
	if redeemedQ == "true" || redeemedQ == "1" {
		q = q.Where("redeemed_at IS NOT NULL")
	} else if redeemedQ == "false" || redeemedQ == "0" {
		q = q.Where("redeemed_at IS NULL")
	}

	var rows []models.PrepaidCard
	if errFind := q.Order("created_at DESC").Find(&rows).Error; errFind != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list prepaid cards failed"})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.formatCard(&row))
	}
	c.JSON(http.StatusOK, gin.H{"prepaid_cards": out})
}

// Get fetches a single prepaid card by ID.
func (h *PrepaidCardHandler) Get(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var card models.PrepaidCard
	if errFind := h.db.WithContext(c.Request.Context()).
		Preload("RedeemedUser").
		First(&card, id).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	c.JSON(http.StatusOK, h.formatCard(&card))
}

// updatePrepaidCardRequest captures optional fields for card updates.
type updatePrepaidCardRequest struct {
	Name      *string  `json:"name"`       // Optional updated name.
	CardSN    *string  `json:"card_sn"`    // Optional updated serial.
	Password  *string  `json:"password"`   // Optional updated password.
	Amount    *float64 `json:"amount"`     // Optional updated amount.
	ValidDays *int     `json:"valid_days"` // Optional updated validity in days.
	IsEnabled *bool    `json:"is_enabled"` // Optional active flag.
}

// Update applies validated field changes to a prepaid card.
func (h *PrepaidCardHandler) Update(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var card models.PrepaidCard
	if errFind := h.db.WithContext(c.Request.Context()).First(&card, id).Error; errFind != nil {
		if errors.Is(errFind, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	var body updatePrepaidCardRequest
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	updates := map[string]any{}
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name cannot be empty"})
			return
		}
		updates["name"] = name
	}
	if body.CardSN != nil {
		cardSN := strings.TrimSpace(*body.CardSN)
		if cardSN == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "card_sn cannot be empty"})
			return
		}
		updates["card_sn"] = cardSN
	}
	if body.Password != nil {
		password := strings.TrimSpace(*body.Password)
		if password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password cannot be empty"})
			return
		}
		updates["password"] = password
	}
	if body.Amount != nil {
		if *body.Amount <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be positive"})
			return
		}
		updates["amount"] = *body.Amount
	}
	if body.ValidDays != nil {
		if *body.ValidDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid_days cannot be negative"})
			return
		}
		updates["valid_days"] = *body.ValidDays
		if card.RedeemedAt == nil {
			updates["expires_at"] = nil
		}
	}
	if body.IsEnabled != nil {
		updates["is_enabled"] = *body.IsEnabled
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}

	res := h.db.WithContext(c.Request.Context()).Model(&models.PrepaidCard{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes a prepaid card record by ID.
func (h *PrepaidCardHandler) Delete(c *gin.Context) {
	id, errParse := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	res := h.db.WithContext(c.Request.Context()).Delete(&models.PrepaidCard{}, id)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

// formatCard maps a prepaid card model into a response payload.
func (h *PrepaidCardHandler) formatCard(card *models.PrepaidCard) gin.H {
	item := gin.H{
		"id":               card.ID,
		"name":             card.Name,
		"card_sn":          card.CardSN,
		"password":         card.Password,
		"amount":           card.Amount,
		"balance":          card.Balance,
		"valid_days":       card.ValidDays,
		"expires_at":       card.ExpiresAt,
		"is_enabled":       card.IsEnabled,
		"redeemed_user_id": card.RedeemedUserID,
		"created_at":       card.CreatedAt,
		"redeemed_at":      card.RedeemedAt,
	}
	if card.RedeemedUser != nil {
		item["redeemed_user"] = gin.H{
			"id":       card.RedeemedUser.ID,
			"username": card.RedeemedUser.Username,
		}
	}
	return item
}

// generateCode returns a random uppercase token of the requested length.
func generateCode(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}
