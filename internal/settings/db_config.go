package settings

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"
)

// dbConfigSnapshot holds the in-memory DB config values.
type dbConfigSnapshot struct {
	updatedAt time.Time
	values    map[string]json.RawMessage
}

// globalDBConfig stores the latest dbConfigSnapshot atomically.
var globalDBConfig atomic.Value // stores dbConfigSnapshot

// init seeds the global DB config snapshot.
func init() {
	globalDBConfig.Store(dbConfigSnapshot{values: map[string]json.RawMessage{}})
}

// StoreDBConfig replaces the in-memory snapshot of DB-backed settings.
func StoreDBConfig(updatedAt time.Time, values map[string]json.RawMessage) {
	next := make(map[string]json.RawMessage, len(values))
	for k, v := range values {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if v == nil {
			next[key] = nil
			continue
		}
		copied := make([]byte, len(v))
		copy(copied, v)
		next[key] = copied
	}

	globalDBConfig.Store(dbConfigSnapshot{
		updatedAt: updatedAt.UTC(),
		values:    next,
	})
}

// DBConfigUpdatedAt returns the last update timestamp for DB config.
func DBConfigUpdatedAt() time.Time {
	cfg := loadDBConfig()
	return cfg.updatedAt
}

// DBConfigValue returns a copy of the raw config value for a key.
func DBConfigValue(key string) (json.RawMessage, bool) {
	cfg := loadDBConfig()
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false
	}
	val, ok := cfg.values[key]
	if !ok {
		return nil, false
	}
	if val == nil {
		return nil, true
	}
	copied := make([]byte, len(val))
	copy(copied, val)
	return copied, true
}

// loadDBConfig returns the current snapshot with safe defaults.
func loadDBConfig() dbConfigSnapshot {
	v := globalDBConfig.Load()
	cfg, ok := v.(dbConfigSnapshot)
	if !ok {
		return dbConfigSnapshot{values: map[string]json.RawMessage{}}
	}
	if cfg.values == nil {
		return dbConfigSnapshot{updatedAt: cfg.updatedAt, values: map[string]json.RawMessage{}}
	}
	return cfg
}
