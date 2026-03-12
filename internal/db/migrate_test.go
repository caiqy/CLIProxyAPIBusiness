package db

import (
	"database/sql"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

func TestMigrateSQLiteTokenHealthColumns(t *testing.T) {
	conn, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}

	if errMigrate := Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate: %v", errMigrate)
	}

	for _, column := range []string{"token_invalid", "last_auth_check_at", "last_auth_error"} {
		if !conn.Migrator().HasColumn("auths", column) {
			t.Fatalf("auths missing column %s", column)
		}
	}
}

func TestMigrateSQLiteTokenHealthColumnsBackfillExistingAuthsTable(t *testing.T) {
	conn, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}

	if errExec := conn.Exec(`
		CREATE TABLE auths (
			id integer primary key autoincrement,
			key text not null unique,
			name text,
			content json not null,
			is_available boolean not null default 1,
			rate_limit integer not null default 0,
			priority integer not null default 0,
			created_at datetime,
			updated_at datetime
		)
	`).Error; errExec != nil {
		t.Fatalf("create legacy auths table: %v", errExec)
	}

	if errMigrate := Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate: %v", errMigrate)
	}

	for _, column := range []string{"token_invalid", "last_auth_check_at", "last_auth_error"} {
		if !conn.Migrator().HasColumn("auths", column) {
			t.Fatalf("auths missing column %s after backfill migration", column)
		}
	}
}

func TestMigrateSQLiteUsageVariantColumns(t *testing.T) {
	conn, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}

	if errMigrate := Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate: %v", errMigrate)
	}

	for _, column := range []string{"variant_origin", "variant"} {
		if !conn.Migrator().HasColumn("usages", column) {
			t.Fatalf("usages missing column %s", column)
		}
	}
}

func TestMigrateSQLiteAuthWhitelistColumns(t *testing.T) {
	conn, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}

	if errMigrate := Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate: %v", errMigrate)
	}

	for _, column := range []string{"whitelist_enabled", "allowed_models", "excluded_models"} {
		if !conn.Migrator().HasColumn("auths", column) {
			t.Fatalf("auths missing column %s", column)
		}
	}

	if errCreate := conn.Exec(`
		INSERT INTO auths (key, name, content, created_at, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, "auth-whitelist-default", "auth-whitelist-default", `{"type":"claude"}`).Error; errCreate != nil {
		t.Fatalf("insert auth row: %v", errCreate)
	}

	var row struct {
		WhitelistEnabled sql.NullBool
		AllowedModels    sql.NullString
		ExcludedModels   sql.NullString
	}
	if errQuery := conn.Raw(`
		SELECT whitelist_enabled, allowed_models, excluded_models
		FROM auths
		WHERE key = ?
	`, "auth-whitelist-default").Scan(&row).Error; errQuery != nil {
		t.Fatalf("query auth row: %v", errQuery)
	}
	if !row.WhitelistEnabled.Valid || row.WhitelistEnabled.Bool {
		t.Fatalf("unexpected whitelist_enabled default: %+v", row.WhitelistEnabled)
	}
	if !row.AllowedModels.Valid || row.AllowedModels.String != "[]" {
		t.Fatalf("unexpected allowed_models default: %+v", row.AllowedModels)
	}
	if !row.ExcludedModels.Valid || row.ExcludedModels.String != "[]" {
		t.Fatalf("unexpected excluded_models default: %+v", row.ExcludedModels)
	}
}

func TestMigrateSQLiteAuthWhitelistColumnsBackfillExistingAuthsTable(t *testing.T) {
	conn, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}

	if errExec := conn.Exec(`
		CREATE TABLE auths (
			id integer primary key autoincrement,
			key text not null unique,
			name text,
			content json not null,
			is_available boolean not null default 1,
			rate_limit integer not null default 0,
			priority integer not null default 0,
			created_at datetime,
			updated_at datetime
		)
	`).Error; errExec != nil {
		t.Fatalf("create legacy auths table: %v", errExec)
	}
	if errMigrate := Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate: %v", errMigrate)
	}

	for _, column := range []string{"whitelist_enabled", "allowed_models", "excluded_models"} {
		if !conn.Migrator().HasColumn("auths", column) {
			t.Fatalf("auths missing column %s after backfill migration", column)
		}
	}

	if errCreate := conn.Exec(`
		INSERT INTO auths (key, name, content, created_at, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, "legacy-auth", "legacy-auth", `{"type":"claude"}`).Error; errCreate != nil {
		t.Fatalf("insert auth row after migration: %v", errCreate)
	}

	var row struct {
		WhitelistEnabled sql.NullBool
		AllowedModels    sql.NullString
		ExcludedModels   sql.NullString
	}
	if errQuery := conn.Raw(`
		SELECT whitelist_enabled, allowed_models, excluded_models
		FROM auths
		WHERE key = ?
	`, "legacy-auth").Scan(&row).Error; errQuery != nil {
		t.Fatalf("query legacy auth row: %v", errQuery)
	}
	if !row.WhitelistEnabled.Valid || row.WhitelistEnabled.Bool {
		t.Fatalf("unexpected legacy whitelist_enabled value: %+v", row.WhitelistEnabled)
	}
	if !row.AllowedModels.Valid || row.AllowedModels.String != "[]" {
		t.Fatalf("unexpected legacy allowed_models value: %+v", row.AllowedModels)
	}
	if !row.ExcludedModels.Valid || row.ExcludedModels.String != "[]" {
		t.Fatalf("unexpected legacy excluded_models value: %+v", row.ExcludedModels)
	}
}

func TestMigrateSQLiteBillingRulesUniqueKey(t *testing.T) {
	conn, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}

	if errMigrate := Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate: %v", errMigrate)
	}

	first := models.BillingRule{
		AuthGroupID: 1,
		UserGroupID: 1,
		Provider:    "openai",
		Model:       "gpt-5.3-codex",
		BillingType: models.BillingTypePerToken,
		IsEnabled:   true,
	}
	if errCreate := conn.Create(&first).Error; errCreate != nil {
		t.Fatalf("create first billing rule: %v", errCreate)
	}

	dup := models.BillingRule{
		AuthGroupID: 1,
		UserGroupID: 1,
		Provider:    "openai",
		Model:       "gpt-5.3-codex",
		BillingType: models.BillingTypePerToken,
		IsEnabled:   true,
	}
	errDup := conn.Create(&dup).Error
	if errDup == nil {
		t.Fatal("expected duplicate billing rule insert to fail")
	}
}

func TestNormalizeAndDeduplicateBillingRulesSQLite(t *testing.T) {
	conn, errOpen := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open sqlite: %v", errOpen)
	}

	if errMigrate := Migrate(conn); errMigrate != nil {
		t.Fatalf("initial migrate: %v", errMigrate)
	}

	if errInsert := conn.Exec(`
		INSERT INTO billing_rules (auth_group_id, user_group_id, provider, model, billing_type, is_enabled, created_at, updated_at)
		VALUES
		(1, 1, ' OpenAI ', 'gpt-5.3-codex ', 2, 1, '2026-01-01 00:00:00', '2026-01-01 00:00:00'),
		(1, 1, 'openai', 'gpt-5.3-codex', 2, 1, '2026-01-02 00:00:00', '2026-01-02 00:00:00')
	`).Error; errInsert != nil {
		t.Fatalf("insert legacy billing rules: %v", errInsert)
	}

	if errNormalize := normalizeAndDeduplicateBillingRulesSQLite(conn); errNormalize != nil {
		t.Fatalf("normalize and deduplicate: %v", errNormalize)
	}

	var count int64
	if errCount := conn.Raw(`
		SELECT COUNT(*)
		FROM billing_rules
		WHERE auth_group_id = 1
		  AND user_group_id = 1
		  AND lower(trim(provider)) = 'openai'
		  AND trim(model) = 'gpt-5.3-codex'
	`).Scan(&count).Error; errCount != nil {
		t.Fatalf("count normalized billing rules: %v", errCount)
	}
	if count != 1 {
		t.Fatalf("expected one normalized billing rule row, got %d", count)
	}

}
