package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIBusiness/internal/billing"
	dbutil "github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/modelmapping"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormUsagePlugin persists usage records and applies billing deductions.
type GormUsagePlugin struct {
	db *gorm.DB
}

// NewGormUsagePlugin constructs a GormUsagePlugin backed by GORM.
func NewGormUsagePlugin(db *gorm.DB) *GormUsagePlugin { return &GormUsagePlugin{db: db} }

// HandleUsage records usage data and deducts bill or prepaid balances.
func (p *GormUsagePlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if p == nil || p.db == nil {
		return
	}

	meta := accessMetadataFromContext(ctx)

	dbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var apiKeyID *uint64
	if rawID := strings.TrimSpace(meta["api_key_id"]); rawID != "" {
		parsed, errParseUint := strconv.ParseUint(rawID, 10, 64)
		if errParseUint == nil {
			parsedID := parsed
			apiKeyID = &parsedID
		}
	}

	var userID *uint64
	if rawID := strings.TrimSpace(meta["user_id"]); rawID != "" {
		parsed, errParseUint := strconv.ParseUint(rawID, 10, 64)
		if errParseUint == nil {
			parsedID := parsed
			userID = &parsedID
		}
	}

	var billingUserGroupID *uint64
	if rawID := strings.TrimSpace(meta["billing_user_group_id"]); rawID != "" {
		parsed, errParseUint := strconv.ParseUint(rawID, 10, 64)
		if errParseUint == nil && parsed != 0 {
			parsedID := parsed
			billingUserGroupID = &parsedID
		}
	}

	authKey := strings.TrimSpace(record.AuthID)
	authID := resolveAuthRecordID(dbCtx, p.db, authKey)

	totalTokens := record.Detail.TotalTokens
	if totalTokens == 0 {
		totalTokens = record.Detail.InputTokens + record.Detail.OutputTokens + record.Detail.ReasoningTokens
	}

	provider := strings.TrimSpace(record.Provider)
	model := strings.TrimSpace(record.Model)
	if mappedModel, ok := modelmapping.LookupMappedModelName(provider, model); ok {
		model = mappedModel
	}

	recordForBilling := record
	recordForBilling.Provider = provider
	recordForBilling.Model = model

	costMicros := calculateCost(dbCtx, p.db, apiKeyID, userID, authID, billingUserGroupID, recordForBilling)
	amountToDeduct := float64(costMicros) / 1_000_000

	errorStatusCode, errorDetail := buildUsageErrorDetail(ctx, record)

	row := models.Usage{
		Provider:        provider,
		Model:           model,
		VariantOrigin:   strings.TrimSpace(record.VariantOrigin),
		Variant:         strings.TrimSpace(record.Variant),
		UserID:          userID,
		UserGroupID:     billingUserGroupID,
		APIKeyID:        apiKeyID,
		AuthID:          authID,
		AuthKey:         authKey,
		AuthIndex:       strings.TrimSpace(record.AuthIndex),
		Source:          strings.TrimSpace(record.Source),
		RequestedAt:     normalizeTime(record.RequestedAt),
		Failed:          record.Failed,
		ErrorStatusCode: errorStatusCode,
		ErrorDetail:     errorDetail,
		InputTokens:     record.Detail.InputTokens,
		OutputTokens:    record.Detail.OutputTokens,
		ReasoningTokens: record.Detail.ReasoningTokens,
		CachedTokens:    record.Detail.CachedTokens,
		TotalTokens:     totalTokens,
		CostMicros:      costMicros,
		ChargedTo:       "none",
		CreatedAt:       time.Now().UTC(),
	}

	if errTx := p.db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		if errCreate := tx.Create(&row).Error; errCreate != nil {
			return errCreate
		}

		if amountToDeduct > 0 && row.UserID != nil {
			chargedTo := "none"
			deducted, errDeductBill := deductBillBalance(dbCtx, tx, *row.UserID, billingUserGroupID, amountToDeduct, costMicros)
			if errDeductBill != nil {
				return errDeductBill
			}
			if deducted {
				chargedTo = "bill"
			} else {
				if errDeductPrepaid := deductPrepaidBalance(dbCtx, tx, *row.UserID, billingUserGroupID, amountToDeduct); errDeductPrepaid != nil {
					return errDeductPrepaid
				}
				chargedTo = "prepaid"
			}

			if errUpdate := tx.WithContext(dbCtx).
				Model(&models.Usage{}).
				Where("id = ?", row.ID).
				Update("charged_to", chargedTo).Error; errUpdate != nil {
				return errUpdate
			}
		}
		return nil
	}); errTx != nil {
		log.WithError(errTx).Warn("usage plugin: failed to persist usage or deduct balance")
	}
}

// resolveAuthRecordID looks up the auth record ID by key.
func resolveAuthRecordID(ctx context.Context, db *gorm.DB, authKey string) *uint64 {
	authKey = strings.TrimSpace(authKey)
	if authKey == "" || db == nil {
		return nil
	}

	// row holds the auth ID lookup result.
	var row struct {
		ID uint64
	}
	errFirst := db.WithContext(ctx).Model(&models.Auth{}).
		Select("id").
		Where("key = ?", authKey).
		Take(&row).Error
	if errFirst != nil {
		if errors.Is(errFirst, gorm.ErrRecordNotFound) {
			return nil
		}
		return nil
	}
	if row.ID == 0 {
		return nil
	}
	id := row.ID
	return &id
}

// billQuotaEpsilon defines a tolerance for quota comparisons.
const billQuotaEpsilon = 0.000001

// deductBillBalance deducts usage from active bills and updates quotas.
func deductBillBalance(ctx context.Context, tx *gorm.DB, userID uint64, userGroupID *uint64, amount float64, costMicros int64) (bool, error) {
	if tx == nil {
		return false, errors.New("nil tx")
	}
	if amount <= 0 {
		return true, nil
	}

	now := time.Now().UTC()
	var bills []models.Bill
	q := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND is_enabled = ? AND status = ? AND left_quota > 0", userID, true, models.BillStatusPaid).
		Where("period_start <= ? AND period_end >= ?", now, now).
		Order("period_end ASC, period_start ASC, id ASC").
		Model(&models.Bill{})
	if userGroupID != nil && *userGroupID != 0 {
		q = q.Where(dbutil.JSONArrayContainsExpr(tx, "user_group_id"), dbutil.JSONArrayContainsValue(tx, *userGroupID))
	}
	if errBills := q.Find(&bills).Error; errBills != nil {
		return false, errBills
	}
	if len(bills) == 0 {
		return false, nil
	}

	totalLeft := 0.0
	totalDaily := 0.0
	unlimitedDaily := false
	for _, bill := range bills {
		if bill.LeftQuota > 0 {
			totalLeft += bill.LeftQuota
		}
		if bill.DailyQuota <= 0 {
			unlimitedDaily = true
		} else {
			totalDaily += bill.DailyQuota
		}
	}
	if totalLeft <= 0 {
		return false, nil
	}
	if totalLeft+billQuotaEpsilon < amount {
		return false, nil
	}

	if !unlimitedDaily && totalDaily > 0 {
		usedToday, errUsage := loadTodayUsageAmount(ctx, tx, userID, userGroupID, now)
		if errUsage != nil {
			return false, errUsage
		}
		usedBefore := usedToday - float64(costMicros)/1_000_000
		if usedBefore < 0 {
			usedBefore = 0
		}
		if usedBefore >= totalDaily {
			return false, nil
		}
	}

	remaining := amount
	for _, bill := range bills {
		if remaining <= 0 {
			break
		}
		if bill.LeftQuota <= 0 {
			continue
		}
		deduct := bill.LeftQuota
		if deduct > remaining {
			deduct = remaining
		}
		res := tx.WithContext(ctx).
			Model(&models.Bill{}).
			Where("id = ?", bill.ID).
			Updates(map[string]any{
				"used_quota": gorm.Expr("used_quota + ?", deduct),
				"left_quota": gorm.Expr("left_quota - ?", deduct),
				"used_count": gorm.Expr("used_count + ?", 1),
				"updated_at": now,
			})
		if res.Error != nil {
			return false, res.Error
		}
		remaining -= deduct
	}
	if remaining > billQuotaEpsilon {
		return false, errors.New("bill quota not enough after lock")
	}
	if errRefresh := refreshBillUserGroupIDs(ctx, tx, userID); errRefresh != nil {
		return false, errRefresh
	}
	return true, nil
}

func refreshBillUserGroupIDs(ctx context.Context, tx *gorm.DB, userID uint64) error {
	if tx == nil {
		return errors.New("nil tx")
	}
	if userID == 0 {
		return errors.New("empty user id")
	}
	now := time.Now().UTC()
	var bills []models.Bill
	if errFind := tx.WithContext(ctx).
		Model(&models.Bill{}).
		Select("user_group_id").
		Where("user_id = ? AND is_enabled = ? AND status = ? AND left_quota > 0", userID, true, models.BillStatusPaid).
		Where("period_start <= ? AND period_end >= ?", now, now).
		Find(&bills).Error; errFind != nil {
		return errFind
	}

	seen := make(map[uint64]struct{})
	merged := make(models.UserGroupIDs, 0)
	for _, bill := range bills {
		for _, gid := range bill.UserGroupID.Clean() {
			if gid == nil || *gid == 0 {
				continue
			}
			if _, ok := seen[*gid]; ok {
				continue
			}
			seen[*gid] = struct{}{}
			idCopy := *gid
			merged = append(merged, &idCopy)
		}
	}

	return tx.WithContext(ctx).
		Model(&models.User{}).
		Where("id = ?", userID).
		Update("bill_user_group_id", merged.Clean()).Error
}

// deductPrepaidBalance deducts usage from prepaid cards in priority order.
func deductPrepaidBalance(ctx context.Context, tx *gorm.DB, userID uint64, userGroupID *uint64, amount float64) error {
	if tx == nil {
		return errors.New("nil tx")
	}
	if amount <= 0 {
		return nil
	}
	now := time.Now().UTC()
	var cards []models.PrepaidCard
	q := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("redeemed_user_id = ? AND is_enabled = ? AND balance > 0 AND redeemed_at IS NOT NULL", userID, true).
		Where("(expires_at IS NULL OR expires_at >= ?)", now).
		Order("expires_at ASC NULLS LAST, redeemed_at ASC NULLS LAST, id ASC").
		Model(&models.PrepaidCard{})
	if userGroupID != nil && *userGroupID != 0 {
		q = q.Where("user_group_id = ?", *userGroupID)
	}
	if errCards := q.Find(&cards).Error; errCards != nil {
		return errCards
	}

	remaining := amount
	for _, card := range cards {
		if remaining <= 0 {
			break
		}
		if card.Balance <= 0 {
			continue
		}
		deduct := card.Balance
		if deduct > remaining {
			deduct = remaining
		}
		res := tx.WithContext(ctx).
			Model(&models.PrepaidCard{}).
			Where("id = ?", card.ID).
			Update("balance", gorm.Expr("balance - ?", deduct))
		if res.Error != nil {
			return res.Error
		}
		remaining -= deduct
	}

	return nil
}

// loadTodayUsageAmount sums today's usage cost in local time.
func loadTodayUsageAmount(ctx context.Context, db *gorm.DB, userID uint64, userGroupID *uint64, now time.Time) (float64, error) {
	if db == nil {
		return 0, errors.New("nil db")
	}
	loc := time.Local
	localNow := now.In(loc)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	var costMicros int64
	q := db.WithContext(ctx).
		Model(&models.Usage{}).
		Where("user_id = ? AND requested_at >= ?", userID, todayStart).
		Select("COALESCE(SUM(cost_micros), 0)")
	if userGroupID != nil && *userGroupID != 0 {
		q = q.Where("user_group_id = ?", *userGroupID)
	}
	if errSum := q.Scan(&costMicros).Error; errSum != nil {
		return 0, errSum
	}
	return float64(costMicros) / 1_000_000, nil
}

// accessMetadataFromContext extracts access metadata from a gin context.
func accessMetadataFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return nil
	}
	v, exists := ginCtx.Get("accessMetadata")
	if !exists {
		return nil
	}
	meta, ok := v.(map[string]string)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(meta))
	for k, val := range meta {
		out[k] = val
	}
	return out
}

type usageErrorDetail struct {
	StatusCode   int    `json:"status_code"`
	Message      string `json:"message"`
	ResponseBody any    `json:"response_body,omitempty"`
}

func buildUsageErrorDetail(ctx context.Context, record coreusage.Record) (*int, datatypes.JSON) {
	statusCode, hasStatus, responseBody := extractUsageErrorContext(ctx)
	failed := record.Failed
	if !failed && (!hasStatus || statusCode < http.StatusBadRequest) {
		return nil, nil
	}
	if !hasStatus || statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}

	message := extractErrorMessage(responseBody)
	if message == "" {
		message = strings.TrimSpace(http.StatusText(statusCode))
	}

	detail := usageErrorDetail{
		StatusCode: statusCode,
		Message:    message,
	}
	if len(responseBody) > 0 {
		if json.Valid(responseBody) {
			detail.ResponseBody = json.RawMessage(responseBody)
		} else {
			detail.ResponseBody = string(responseBody)
		}
	}

	payload, errMarshal := json.Marshal(detail)
	if errMarshal != nil {
		return nil, nil
	}
	statusValue := statusCode
	return &statusValue, datatypes.JSON(payload)
}

func extractUsageErrorContext(ctx context.Context) (int, bool, []byte) {
	if ctx == nil {
		return 0, false, nil
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return 0, false, nil
	}
	statusCode := ginCtx.Writer.Status()
	if statusCode == 0 {
		return 0, false, extractAPIResponse(ginCtx)
	}
	return statusCode, true, extractAPIResponse(ginCtx)
}

func extractAPIResponse(ctx *gin.Context) []byte {
	if ctx == nil {
		return nil
	}
	if v, exists := ctx.Get("API_RESPONSE"); exists {
		switch value := v.(type) {
		case []byte:
			return bytes.Clone(value)
		case string:
			return []byte(value)
		}
	}
	return nil
}

func extractErrorMessage(responseBody []byte) string {
	trimmed := strings.TrimSpace(string(responseBody))
	if trimmed == "" {
		return ""
	}
	if !json.Valid([]byte(trimmed)) {
		return trimmed
	}

	var payload map[string]any
	if errUnmarshal := json.Unmarshal([]byte(trimmed), &payload); errUnmarshal != nil {
		return ""
	}
	if errValue, ok := payload["error"]; ok {
		switch typed := errValue.(type) {
		case map[string]any:
			if msg, ok := typed["message"].(string); ok {
				return strings.TrimSpace(msg)
			}
		case string:
			return strings.TrimSpace(typed)
		}
	}
	if msg, ok := payload["message"].(string); ok {
		return strings.TrimSpace(msg)
	}
	return ""
}

// normalizeTime returns a UTC timestamp, defaulting to now if zero.
func normalizeTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}

// calculateCost computes usage cost in micros based on billing rules.
func calculateCost(ctx context.Context, db *gorm.DB, apiKeyID, userID, authID, billingUserGroupID *uint64, record coreusage.Record) int64 {
	if db == nil {
		return 0
	}
	if record.Failed {
		return 0
	}

	provider := strings.TrimSpace(record.Provider)
	model := strings.TrimSpace(record.Model)
	if provider == "" || model == "" {
		return 0
	}
	providerLower := strings.ToLower(provider)

	var authGroupID *uint64
	if authID != nil {
		var auth models.Auth
		if errFindAuth := db.WithContext(ctx).Select("auth_group_id").First(&auth, *authID).Error; errFindAuth == nil {
			authGroupID = auth.AuthGroupID.Primary()
		}
	}

	userGroupID := billingUserGroupID
	if userGroupID == nil && apiKeyID != nil {
		var apiKey models.APIKey
		if errFindAPIKey := db.WithContext(ctx).Select("user_id").First(&apiKey, *apiKeyID).Error; errFindAPIKey == nil && apiKey.UserID != nil {
			var user models.User
			if errFindUser := db.WithContext(ctx).Select("user_group_id").First(&user, *apiKey.UserID).Error; errFindUser == nil {
				userGroupID = user.UserGroupID.Primary()
			}
		}
	}
	if userGroupID == nil && userID != nil {
		var user models.User
		if errFindUser := db.WithContext(ctx).Select("user_group_id").First(&user, *userID).Error; errFindUser == nil {
			userGroupID = user.UserGroupID.Primary()
		}
	}

	costFromRule := func(rule *models.BillingRule) int64 {
		if rule == nil {
			return 0
		}

		switch rule.BillingType {
		case models.BillingTypePerRequest:
			if rule.PricePerRequest == nil {
				return 0
			}
			return int64(math.Round(*rule.PricePerRequest * 1_000_000))
		case models.BillingTypePerToken:
			var total float64
			// Many upstream providers (e.g. OpenAI usage format) report CachedTokens as a subset of
			// InputTokens (input_tokens_details.cached_tokens). If we charge InputTokens in full AND
			// charge CachedTokens again as cache-read, cache hit tokens would be double-charged.
			//
			// To make per-token billing consistent across providers, we treat billable input tokens as:
			//   billableInput = max(InputTokens - CachedTokens, 0)
			// and charge CachedTokens separately via PriceCacheReadToken.
			billableInputTokens := record.Detail.InputTokens
			if record.Detail.CachedTokens > 0 && record.Detail.CachedTokens <= billableInputTokens {
				billableInputTokens -= record.Detail.CachedTokens
			}
			if rule.PriceInputToken != nil {
				total += float64(billableInputTokens) * (*rule.PriceInputToken)
			}
			if rule.PriceOutputToken != nil {
				total += float64(record.Detail.OutputTokens) * (*rule.PriceOutputToken)
			}
			if rule.PriceCacheCreateToken != nil {
				total += float64(0) * (*rule.PriceCacheCreateToken)
			}
			if rule.PriceCacheReadToken != nil {
				total += float64(record.Detail.CachedTokens) * (*rule.PriceCacheReadToken)
			}
			// Token prices are per 1,000,000 tokens, so micros = price_per_million * tokens
			return int64(math.Round(total))
		default:
			return 0
		}
	}

	loadCandidateRules := func(primaryAuthGroupID, primaryUserGroupID, defaultAuthGroupID, defaultUserGroupID uint64) ([]models.BillingRule, error) {
		q := db.WithContext(ctx).Model(&models.BillingRule{}).Where("is_enabled = true")
		if defaultAuthGroupID != 0 && defaultUserGroupID != 0 && (defaultAuthGroupID != primaryAuthGroupID || defaultUserGroupID != primaryUserGroupID) {
			q = q.Where("(auth_group_id = ? AND user_group_id = ?) OR (auth_group_id = ? AND user_group_id = ?)", primaryAuthGroupID, primaryUserGroupID, defaultAuthGroupID, defaultUserGroupID)
		} else {
			q = q.Where("auth_group_id = ? AND user_group_id = ?", primaryAuthGroupID, primaryUserGroupID)
		}
		q = q.Where("((LOWER(provider) = ? AND model = ?) OR (provider = '' AND model = ''))", providerLower, model)

		var rules []models.BillingRule
		if errFindRules := q.Find(&rules).Error; errFindRules != nil {
			return nil, errFindRules
		}
		return rules, nil
	}

	if authGroupID != nil && userGroupID != nil {
		rulesPrimary, errPrimary := loadCandidateRules(*authGroupID, *userGroupID, 0, 0)
		if errPrimary != nil {
			return 0
		}
		if rule := billing.SelectBillingRule(rulesPrimary, *authGroupID, *userGroupID, 0, 0, provider, model); rule != nil {
			return costFromRule(rule)
		}
	}

	defaultAuthGroupID, errDefaultAuthGroup := billing.ResolveDefaultAuthGroupID(ctx, db)
	if errDefaultAuthGroup != nil {
		return 0
	}
	defaultUserGroupID, errDefaultUserGroup := billing.ResolveDefaultUserGroupID(ctx, db)
	if errDefaultUserGroup != nil {
		return 0
	}

	primaryAuthGroupID := authGroupID
	if primaryAuthGroupID == nil {
		primaryAuthGroupID = defaultAuthGroupID
	}
	primaryUserGroupID := userGroupID
	if primaryUserGroupID == nil {
		primaryUserGroupID = defaultUserGroupID
	}
	if primaryAuthGroupID == nil || primaryUserGroupID == nil {
		return 0
	}

	primaryAuthGroupIDValue := *primaryAuthGroupID
	primaryUserGroupIDValue := *primaryUserGroupID

	var defaultAuthGroupIDValue uint64
	if defaultAuthGroupID != nil {
		defaultAuthGroupIDValue = *defaultAuthGroupID
	}
	var defaultUserGroupIDValue uint64
	if defaultUserGroupID != nil {
		defaultUserGroupIDValue = *defaultUserGroupID
	}

	rules, errRules := loadCandidateRules(primaryAuthGroupIDValue, primaryUserGroupIDValue, defaultAuthGroupIDValue, defaultUserGroupIDValue)
	if errRules != nil {
		return 0
	}

	rule := billing.SelectBillingRule(rules, primaryAuthGroupIDValue, primaryUserGroupIDValue, defaultAuthGroupIDValue, defaultUserGroupIDValue, provider, model)
	return costFromRule(rule)
}

// Ensure GormUsagePlugin implements coreusage.Plugin.
var _ coreusage.Plugin = (*GormUsagePlugin)(nil)
