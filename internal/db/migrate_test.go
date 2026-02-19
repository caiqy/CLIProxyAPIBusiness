package db

import (
	"testing"

	"github.com/glebarez/sqlite"
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
