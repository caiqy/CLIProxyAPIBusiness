package access

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	"gorm.io/gorm"
)

// ProviderTypeDBAPIKey identifies the database API key access provider.
const ProviderTypeDBAPIKey = "db-api-key"

// ErrInsufficientBalance indicates the user has no valid quota or prepaid balance.
var ErrInsufficientBalance = errors.New("insufficient balance")

// ErrDailyMaxUsageExceeded indicates the user exceeded daily prepaid spending limit.
var ErrDailyMaxUsageExceeded = errors.New("daily max usage exceeded")

// DBAPIKeyProvider authenticates requests using API keys stored in the database.
type DBAPIKeyProvider struct {
	db *gorm.DB

	name string

	header       string
	scheme       string
	allowXAPIKey bool

	bypassPathPrefixes []string
}

// RegisterDBAPIKeyProvider registers the DB-backed API key provider with the SDK registry.
func RegisterDBAPIKeyProvider(db *gorm.DB) {
	sdkaccess.RegisterProvider(ProviderTypeDBAPIKey, func(cfg *config.AccessProvider, _ *config.SDKConfig) (sdkaccess.Provider, error) {
		if db == nil {
			return nil, fmt.Errorf("db api key provider: nil db")
		}
		p := &DBAPIKeyProvider{
			db:   db,
			name: ProviderTypeDBAPIKey,

			header:       "Authorization",
			scheme:       "Bearer",
			allowXAPIKey: true,

			bypassPathPrefixes: []string{"/healthz", "/v0/management"},
		}
		if cfg != nil {
			if strings.TrimSpace(cfg.Name) != "" {
				p.name = strings.TrimSpace(cfg.Name)
			}
			if cfg.Config != nil {
				if v, ok := cfg.Config["header"].(string); ok && strings.TrimSpace(v) != "" {
					p.header = strings.TrimSpace(v)
				}
				if v, ok := cfg.Config["scheme"].(string); ok && strings.TrimSpace(v) != "" {
					p.scheme = strings.TrimSpace(v)
				}
				if v, ok := cfg.Config["allow-x-api-key"].(bool); ok {
					p.allowXAPIKey = v
				}
				switch rawList := cfg.Config["bypass-path-prefixes"].(type) {
				case []any:
					p.bypassPathPrefixes = make([]string, 0, len(rawList))
					for _, raw := range rawList {
						s, ok := raw.(string)
						if !ok {
							continue
						}
						trimmed := strings.TrimSpace(s)
						if trimmed != "" {
							p.bypassPathPrefixes = append(p.bypassPathPrefixes, trimmed)
						}
					}
				case []string:
					p.bypassPathPrefixes = make([]string, 0, len(rawList))
					for _, raw := range rawList {
						trimmed := strings.TrimSpace(raw)
						if trimmed != "" {
							p.bypassPathPrefixes = append(p.bypassPathPrefixes, trimmed)
						}
					}
				}
			}
		}
		return p, nil
	})
}

// Identifier returns the configured provider name.
func (p *DBAPIKeyProvider) Identifier() string { return p.name }

// Authenticate validates the request API key and returns the access result.
func (p *DBAPIKeyProvider) Authenticate(ctx context.Context, r *http.Request) (*sdkaccess.Result, error) {
	if p == nil || p.db == nil || r == nil {
		return nil, sdkaccess.ErrNotHandled
	}

	path := ""
	if r.URL != nil {
		path = r.URL.Path
	}
	for _, prefix := range p.bypassPathPrefixes {
		if prefix != "" && strings.HasPrefix(path, prefix) {
			return nil, nil
		}
	}

	token := extractToken(r, p.header, p.scheme, p.allowXAPIKey)
	if token == "" {
		return nil, sdkaccess.ErrNoCredentials
	}

	var apiKey models.APIKey
	err := p.db.WithContext(ctx).
		Preload("User").
		Where("api_key = ? AND active = ? AND revoked_at IS NULL", token, true).
		First(&apiKey).Error
	switch {
	case err == nil:
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, sdkaccess.ErrInvalidCredential
	default:
		return nil, fmt.Errorf("db api key provider: query failed: %w", err)
	}

	if apiKey.User != nil {
		if apiKey.User.Disabled {
			return nil, sdkaccess.ErrInvalidCredential
		}
		if apiKey.UserID != nil {
			ok, errBalance := hasValidBillOrPrepaidBalance(ctx, p.db, *apiKey.UserID)
			if errBalance != nil {
				return nil, fmt.Errorf("db api key provider: balance check failed: %w", errBalance)
			}
			if !ok {
				return nil, ErrInsufficientBalance
			}
		}
	}

	now := time.Now().UTC()
	_ = p.db.WithContext(ctx).Model(&models.APIKey{}).
		Where("id = ?", apiKey.ID).
		Update("last_used_at", &now).Error

	meta := map[string]string{
		"api_key_id":   strconv.FormatUint(apiKey.ID, 10),
		"api_key_name": apiKey.Name,
		"is_admin":     strconv.FormatBool(apiKey.IsAdmin),
	}
	if apiKey.UserID != nil {
		meta["user_id"] = strconv.FormatUint(*apiKey.UserID, 10)
	}

	return &sdkaccess.Result{
		Provider:  p.name,
		Principal: strconv.FormatUint(apiKey.ID, 10),
		Metadata:  meta,
	}, nil
}

// extractToken extracts an API key token from headers or query parameters.
func extractToken(r *http.Request, header string, scheme string, allowXAPIKey bool) string {
	header = strings.TrimSpace(header)
	scheme = strings.TrimSpace(scheme)
	if header == "" {
		header = "Authorization"
	}
	val := strings.TrimSpace(r.Header.Get(header))
	if val != "" && scheme != "" {
		prefix := scheme + " "
		if strings.HasPrefix(val, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(val, prefix))
		}
	}
	if val != "" && scheme == "" {
		return val
	}
	if allowXAPIKey {
		if v := strings.TrimSpace(r.Header.Get("X-API-Key")); v != "" {
			return v
		}
		if v := strings.TrimSpace(r.Header.Get("X-Goog-Api-Key")); v != "" {
			return v
		}
	}
	if r.URL != nil && strings.HasPrefix(r.URL.Path, "/v1beta") {
		if v := strings.TrimSpace(r.URL.Query().Get("key")); v != "" {
			return v
		}
	}
	return ""
}

// hasValidBillOrPrepaidBalance checks if a user has active bill quota or prepaid balance.
func hasValidBillOrPrepaidBalance(ctx context.Context, db *gorm.DB, userID uint64) (bool, error) {
	okBill, errBill := hasValidBillQuota(ctx, db, userID)
	if errBill != nil {
		return false, errBill
	}
	if okBill {
		return true, nil
	}

	okPrepaid, errPrepaid := hasValidPrepaidBalance(ctx, db, userID)
	if errPrepaid != nil {
		return false, errPrepaid
	}
	if !okPrepaid {
		return false, nil
	}

	dailyMax, errDailyMax := loadUserDailyMaxUsage(ctx, db, userID)
	if errDailyMax != nil {
		return false, errDailyMax
	}
	if dailyMax <= 0 {
		return true, nil
	}

	usedPrepaidToday, errUsed := loadTodayPrepaidUsageAmount(ctx, db, userID, time.Now().UTC())
	if errUsed != nil {
		return false, errUsed
	}
	const dailyUsageEpsilon = 0.000001
	if usedPrepaidToday+dailyUsageEpsilon >= dailyMax {
		return false, ErrDailyMaxUsageExceeded
	}
	return true, nil
}

func loadUserDailyMaxUsage(ctx context.Context, db *gorm.DB, userID uint64) (float64, error) {
	if db == nil {
		return 0, errors.New("nil db")
	}
	if userID == 0 {
		return 0, nil
	}
	var row struct {
		DailyMaxUsage float64 `gorm:"column:daily_max_usage"`
	}
	err := db.WithContext(ctx).
		Model(&models.User{}).
		Select("daily_max_usage").
		Where("id = ?", userID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.DailyMaxUsage, nil
}

func loadTodayPrepaidUsageAmount(ctx context.Context, db *gorm.DB, userID uint64, now time.Time) (float64, error) {
	if db == nil {
		return 0, errors.New("nil db")
	}
	loc := time.Local
	localNow := now.In(loc)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	var costMicros int64
	if errSum := db.WithContext(ctx).
		Model(&models.Usage{}).
		Where("user_id = ? AND charged_to = ? AND requested_at >= ?", userID, "prepaid", todayStart).
		Select("COALESCE(SUM(cost_micros), 0)").
		Scan(&costMicros).Error; errSum != nil {
		return 0, errSum
	}
	return float64(costMicros) / 1_000_000, nil
}

// hasValidBillQuota checks if the user has paid bill quota remaining.
func hasValidBillQuota(ctx context.Context, db *gorm.DB, userID uint64) (bool, error) {
	if db == nil {
		return false, errors.New("nil db")
	}
	now := time.Now().UTC()
	var summary struct {
		LeftQuota      float64 `gorm:"column:left_quota"`      // Total remaining quota across bills.
		DailyQuota     float64 `gorm:"column:daily_quota"`     // Sum of daily quotas for limited plans.
		UnlimitedDaily int64   `gorm:"column:unlimited_daily"` // Count of unlimited daily plans.
	}
	if errSummary := db.WithContext(ctx).
		Model(&models.Bill{}).
		Select(`
			COALESCE(SUM(left_quota), 0) AS left_quota,
			COALESCE(SUM(CASE WHEN daily_quota > 0 THEN daily_quota ELSE 0 END), 0) AS daily_quota,
			COALESCE(SUM(CASE WHEN daily_quota <= 0 THEN 1 ELSE 0 END), 0) AS unlimited_daily
		`).
		Where("user_id = ? AND is_enabled = ? AND status = ? AND left_quota > 0", userID, true, models.BillStatusPaid).
		Where("period_start <= ? AND period_end >= ?", now, now).
		Scan(&summary).Error; errSummary != nil {
		return false, errSummary
	}
	if summary.LeftQuota <= 0 {
		return false, nil
	}
	if summary.UnlimitedDaily > 0 || summary.DailyQuota <= 0 {
		return true, nil
	}
	usedToday, errUsage := loadTodayUsageAmount(ctx, db, userID, now)
	if errUsage != nil {
		return false, errUsage
	}
	return usedToday < summary.DailyQuota, nil
}

// loadTodayUsageAmount calculates today's usage cost for the user in local time.
func loadTodayUsageAmount(ctx context.Context, db *gorm.DB, userID uint64, now time.Time) (float64, error) {
	if db == nil {
		return 0, errors.New("nil db")
	}
	loc := time.Local
	localNow := now.In(loc)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	var costMicros int64
	if errSum := db.WithContext(ctx).
		Model(&models.Usage{}).
		Where("user_id = ? AND requested_at >= ?", userID, todayStart).
		Select("COALESCE(SUM(cost_micros), 0)").
		Scan(&costMicros).Error; errSum != nil {
		return 0, errSum
	}
	return float64(costMicros) / 1_000_000, nil
}

// hasValidPrepaidBalance checks if the user has redeemable prepaid card balance.
func hasValidPrepaidBalance(ctx context.Context, db *gorm.DB, userID uint64) (bool, error) {
	if db == nil {
		return false, errors.New("nil db")
	}
	now := time.Now().UTC()
	var count int64
	if err := db.WithContext(ctx).
		Model(&models.PrepaidCard{}).
		Where("redeemed_user_id = ? AND is_enabled = ? AND balance > 0 AND redeemed_at IS NOT NULL", userID, true).
		Where("(expires_at IS NULL OR expires_at >= ?)", now).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
