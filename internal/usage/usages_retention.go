package usage

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	internalsettings "github.com/router-for-me/CLIProxyAPIBusiness/internal/settings"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	defaultUsagesRetentionInterval = 6 * time.Hour
	defaultUsagesDeleteBatchSize   = 5000
	maxDeleteBatchesPerRun         = 2000
)

// UsagesRetentionCleaner periodically deletes old rows from the usages table.
type UsagesRetentionCleaner struct {
	db        *gorm.DB
	interval  time.Duration
	batchSize int
}

func NewUsagesRetentionCleaner(db *gorm.DB) *UsagesRetentionCleaner {
	if db == nil {
		return nil
	}
	return &UsagesRetentionCleaner{
		db:        db,
		interval:  defaultUsagesRetentionInterval,
		batchSize: defaultUsagesDeleteBatchSize,
	}
}

// Start launches the cleanup loop in a background goroutine.
func (c *UsagesRetentionCleaner) Start(ctx context.Context) {
	if c == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go c.run(ctx)
	log.Infof("usages retention cleaner started (interval=%s)", c.interval)
}

func (c *UsagesRetentionCleaner) run(ctx context.Context) {
	for {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		c.cleanupOnce(ctx)
		if ctx != nil && ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(c.interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (c *UsagesRetentionCleaner) cleanupOnce(ctx context.Context) {
	if c == nil || c.db == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	retentionDays := internalsettings.DefaultUsagesRetentionDays
	if raw, ok := internalsettings.DBConfigValue(internalsettings.UsagesRetentionDaysKey); ok {
		if parsed, okParse := parseDBConfigInt(raw); okParse && parsed >= 0 {
			retentionDays = parsed
		}
	}
	if retentionDays <= 0 {
		return
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	deletedTotal := int64(0)
	for i := 0; i < maxDeleteBatchesPerRun; i++ {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		n, err := c.deleteBatch(ctx, cutoff)
		if err != nil {
			log.WithError(err).Warn("usages retention cleaner: delete batch failed")
			break
		}
		if n <= 0 {
			break
		}
		deletedTotal += n
	}

	if deletedTotal > 0 {
		log.Infof("usages retention cleaner: deleted %d rows (cutoff=%s retention_days=%d)", deletedTotal, cutoff.Format(time.RFC3339), retentionDays)
	}
}

func (c *UsagesRetentionCleaner) deleteBatch(ctx context.Context, cutoff time.Time) (int64, error) {
	if c == nil || c.db == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limit := c.batchSize
	if limit <= 0 {
		limit = defaultUsagesDeleteBatchSize
	}

	// Use a limited subquery to avoid long-running transactions and table locks.
	res := c.db.WithContext(ctx).Exec(`
		DELETE FROM usages
		WHERE id IN (
			SELECT id FROM usages
			WHERE requested_at < ?
			ORDER BY requested_at ASC
			LIMIT ?
		)
	`, cutoff, limit)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func parseDBConfigInt(raw json.RawMessage) (int, bool) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 {
		return 0, false
	}
	var n int
	if errUnmarshal := json.Unmarshal(raw, &n); errUnmarshal == nil {
		return n, true
	}
	var f float64
	if errUnmarshal := json.Unmarshal(raw, &f); errUnmarshal == nil {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		if f != math.Trunc(f) {
			return 0, false
		}
		return int(f), true
	}
	var s string
	if errUnmarshal := json.Unmarshal(raw, &s); errUnmarshal == nil {
		parsed, errParse := strconv.Atoi(strings.TrimSpace(s))
		if errParse == nil {
			return parsed, true
		}
	}
	var wrapper struct {
		Value json.RawMessage `json:"value"`
	}
	if errUnmarshal := json.Unmarshal(raw, &wrapper); errUnmarshal == nil && len(wrapper.Value) > 0 {
		return parseDBConfigInt(wrapper.Value)
	}
	return 0, false
}

func bytesTrimSpace(input []byte) []byte {
	if len(input) == 0 {
		return nil
	}
	start := 0
	end := len(input)
	for start < end {
		if input[start] > ' ' {
			break
		}
		start++
	}
	for end > start {
		if input[end-1] > ' ' {
			break
		}
		end--
	}
	return input[start:end]
}
