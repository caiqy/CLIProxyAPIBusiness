package db

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Dialect identifiers supported by the database layer.
const (
	// DialectPostgres is the PostgreSQL dialect name.
	DialectPostgres = "postgres"
	// DialectSQLite is the SQLite dialect name.
	DialectSQLite = "sqlite"
)

// DialectName returns the active database dialect name.
func DialectName(conn *gorm.DB) string {
	if conn == nil || conn.Dialector == nil {
		return ""
	}
	return conn.Dialector.Name()
}

// IsSQLite reports whether the connection uses SQLite.
func IsSQLite(conn *gorm.DB) bool {
	return DialectName(conn) == DialectSQLite
}

// CaseInsensitiveLikeExpr returns a SQL expression for case-insensitive LIKE.
func CaseInsensitiveLikeExpr(conn *gorm.DB, column string) string {
	if IsSQLite(conn) {
		return fmt.Sprintf("LOWER(%s) LIKE ?", column)
	}
	return fmt.Sprintf("%s ILIKE ?", column)
}

// NormalizeLikePattern normalizes a LIKE pattern for the current dialect.
func NormalizeLikePattern(conn *gorm.DB, pattern string) string {
	if IsSQLite(conn) {
		return strings.ToLower(pattern)
	}
	return pattern
}

// JSONExtractTextExpr returns a SQL expression to extract a JSON field as text.
func JSONExtractTextExpr(conn *gorm.DB, column, key string) string {
	if IsSQLite(conn) {
		return fmt.Sprintf("json_extract(%s, '$.%s')", column, key)
	}
	return fmt.Sprintf("%s->>'%s'", column, key)
}
