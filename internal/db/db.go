package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// tzConfig holds timezone configuration for database connections.
type tzConfig struct {
	dbTimeZone   string
	scanLocation *time.Location
}

func init() {
	logger.Default = logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             0,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)
}

func newGormLogger() logger.Interface {
	return logger.Default
}

// Global timezone cache and initializer for DB connections.
var (
	// globalTZOnce initializes timezone configuration once.
	globalTZOnce sync.Once
	// globalTZConfig caches the resolved timezone configuration.
	globalTZConfig tzConfig
)

// Open opens a GORM connection based on the provided DSN.
func Open(dsn string) (*gorm.DB, error) {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return nil, fmt.Errorf("db: empty dsn")
	}

	dialect, err := detectDialectFromDSN(trimmed)
	if err != nil {
		return nil, err
	}
	switch dialect {
	case DialectPostgres:
		return openPostgres(trimmed)
	case DialectSQLite:
		return openSQLite(trimmed)
	default:
		return nil, fmt.Errorf("db: unsupported dialect: %s", dialect)
	}
}

// detectDialectFromDSN infers the dialect from a DSN string.
func detectDialectFromDSN(dsn string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(dsn))
	switch {
	case strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://"):
		return DialectPostgres, nil
	case strings.Contains(lower, "host=") || strings.Contains(lower, "user=") || strings.Contains(lower, "dbname=") || strings.Contains(lower, "sslmode="):
		return DialectPostgres, nil
	case strings.HasPrefix(lower, "file:"),
		strings.HasPrefix(lower, "sqlite://"),
		strings.HasPrefix(lower, "sqlite3://"),
		!strings.Contains(lower, "://"):
		return DialectSQLite, nil
	default:
		return "", fmt.Errorf("db: unsupported dsn: %s", dsn)
	}
}

// openPostgres opens a PostgreSQL connection with timezone handling.
func openPostgres(dsn string) (*gorm.DB, error) {
	tz := loadGlobalTimeZone()
	sqlDB, err := openPostgresSQLDB(dsn, tz)
	if err != nil {
		return nil, err
	}

	conn, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: newGormLogger(),
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: open: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if errPing := sqlDB.PingContext(pingCtx); errPing != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: ping: %w", errPing)
	}

	return conn, nil
}

// openSQLite opens a SQLite connection with defaults and pragmas applied.
func openSQLite(dsn string) (*gorm.DB, error) {
	normalized := normalizeSQLiteDSN(dsn)
	normalized = ensureSQLiteParams(normalized)
	if errEnsure := ensureSQLiteDir(normalized); errEnsure != nil {
		return nil, errEnsure
	}

	conn, err := gorm.Open(sqlite.Open(normalized), &gorm.Config{
		Logger: newGormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("db: open sqlite: %w", err)
	}

	sqlDB, err := conn.DB()
	if err != nil {
		return nil, fmt.Errorf("db: open sqlite sql: %w", err)
	}

	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if errPragma := applySQLitePragmas(sqlDB); errPragma != nil {
		_ = sqlDB.Close()
		return nil, errPragma
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if errPing := sqlDB.PingContext(pingCtx); errPing != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db: ping: %w", errPing)
	}

	return conn, nil
}

// openPostgresSQLDB opens a sql.DB with timezone-aware type mapping.
func openPostgresSQLDB(dsn string, tz tzConfig) (*sql.DB, error) {
	cfg, errParse := pgx.ParseConfig(dsn)
	if errParse != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", errParse)
	}

	var options []stdlib.OptionOpenDB
	if tz.dbTimeZone != "" {
		cfg.RuntimeParams["timezone"] = tz.dbTimeZone
	}
	if tz.scanLocation != nil {
		options = append(options, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
			loc := tz.scanLocation
			if loc == nil {
				return nil
			}
			conn.TypeMap().RegisterType(&pgtype.Type{
				Name:  "timestamp",
				OID:   pgtype.TimestampOID,
				Codec: &pgtype.TimestampCodec{ScanLocation: loc},
			})
			conn.TypeMap().RegisterType(&pgtype.Type{
				Name:  "timestamptz",
				OID:   pgtype.TimestamptzOID,
				Codec: &pgtype.TimestamptzCodec{ScanLocation: loc},
			})
			return nil
		}))
	}

	return stdlib.OpenDB(*cfg, options...), nil
}

// normalizeSQLiteDSN converts sqlite URLs into file-based DSNs.
func normalizeSQLiteDSN(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "sqlite3://") || strings.HasPrefix(lower, "sqlite://") {
		parts := strings.SplitN(trimmed, "://", 2)
		if len(parts) == 2 {
			return "file:" + parts[1]
		}
	}
	return trimmed
}

// ensureSQLiteParams adds default SQLite query parameters when missing.
func ensureSQLiteParams(dsn string) string {
	if strings.TrimSpace(dsn) == "" {
		return dsn
	}
	targetParams := map[string]string{
		"_busy_timeout": "5000",
		"_journal_mode": "WAL",
		"_foreign_keys": "on",
		"_synchronous":  "NORMAL",
	}

	lower := strings.ToLower(dsn)
	existing := map[string]struct{}{}
	if idx := strings.Index(lower, "?"); idx >= 0 {
		query := lower[idx+1:]
		for _, part := range strings.Split(query, "&") {
			if part == "" {
				continue
			}
			key := strings.SplitN(part, "=", 2)[0]
			existing[key] = struct{}{}
		}
	}

	var add []string
	for key, value := range targetParams {
		if _, ok := existing[key]; ok {
			continue
		}
		add = append(add, key+"="+value)
	}
	if len(add) == 0 {
		return dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + strings.Join(add, "&")
}

// sqlitePathFromDSN extracts the file path from a SQLite DSN.
func sqlitePathFromDSN(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return ""
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "file:") {
		pathPart := trimmed[len("file:"):]
		if idx := strings.Index(pathPart, "?"); idx >= 0 {
			pathPart = pathPart[:idx]
		}
		pathPart = strings.TrimPrefix(pathPart, "//")
		if pathPart == "" || pathPart == ":memory:" {
			return ""
		}
		return pathPart
	}

	if strings.Contains(lower, "://") || trimmed == ":memory:" {
		return ""
	}
	return trimmed
}

// ensureSQLiteDir creates the parent directory for a SQLite database file.
func ensureSQLiteDir(dsn string) error {
	path := sqlitePathFromDSN(dsn)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if errMkdir := os.MkdirAll(dir, 0755); errMkdir != nil {
		return fmt.Errorf("db: create sqlite dir: %w", errMkdir)
	}
	return nil
}

// applySQLitePragmas applies recommended SQLite pragmas.
func applySQLitePragmas(sqlDB *sql.DB) error {
	if sqlDB == nil {
		return fmt.Errorf("db: nil sqlite db")
	}
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, pragma := range pragmas {
		if _, err := sqlDB.Exec(pragma); err != nil {
			return fmt.Errorf("db: sqlite pragma %s: %w", pragma, err)
		}
	}
	return nil
}

// loadGlobalTimeZone resolves the timezone configuration once.
func loadGlobalTimeZone() tzConfig {
	globalTZOnce.Do(func() {
		if tzName, ok := detectHostTimeZoneName(); ok {
			loc, errLoad := time.LoadLocation(tzName)
			if errLoad == nil {
				time.Local = loc
				globalTZConfig = tzConfig{
					dbTimeZone:   tzName,
					scanLocation: loc,
				}
				return
			}
		}

		_, offsetSeconds := time.Now().Zone()
		offsetName := formatUTCOffset(offsetSeconds)
		loc := time.FixedZone(offsetName, offsetSeconds)
		time.Local = loc
		globalTZConfig = tzConfig{
			dbTimeZone:   offsetName,
			scanLocation: loc,
		}
	})

	return globalTZConfig
}

// detectHostTimeZoneName attempts to read the host timezone name.
func detectHostTimeZoneName() (string, bool) {
	if tz, ok := normalizeTimeZoneName(strings.TrimSpace(os.Getenv("TZ"))); ok {
		return tz, true
	}

	if data, errRead := os.ReadFile("/etc/timezone"); errRead == nil {
		if tz, ok := normalizeTimeZoneName(strings.TrimSpace(string(data))); ok {
			return tz, true
		}
	}

	if link, errReadlink := os.Readlink("/etc/localtime"); errReadlink == nil {
		if tz, ok := normalizeTimeZoneName(extractTimeZoneFromZoneinfoPath(link)); ok {
			return tz, true
		}
	}

	return "", false
}

// normalizeTimeZoneName validates and normalizes a timezone string.
func normalizeTimeZoneName(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	input = strings.TrimPrefix(input, ":")

	if strings.Contains(input, "/") {
		if tz, ok := normalizeTimeZoneName(extractTimeZoneFromZoneinfoPath(input)); ok {
			return tz, true
		}
	}

	if _, errLoad := time.LoadLocation(input); errLoad != nil {
		return "", false
	}
	return input, true
}

// extractTimeZoneFromZoneinfoPath derives a timezone name from a zoneinfo path.
func extractTimeZoneFromZoneinfoPath(path string) string {
	const marker = "/zoneinfo/"
	idx := strings.Index(path, marker)
	if idx < 0 {
		return ""
	}
	out := strings.TrimSpace(path[idx+len(marker):])
	out = strings.TrimPrefix(out, "/")
	out = strings.TrimSuffix(out, "/")
	out = strings.TrimPrefix(out, "posix/")
	out = strings.TrimPrefix(out, "right/")
	return out
}

// formatUTCOffset formats a numeric offset into "+HH:MM" or "-HH:MM".
func formatUTCOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}

	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}
