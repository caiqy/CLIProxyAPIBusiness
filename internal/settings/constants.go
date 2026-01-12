package settings

// DB config keys and defaults for settings.
const (
	// SiteNameKey is the DB config key for the UI site name.
	SiteNameKey = "SITE_NAME"
	// DefaultSiteName is the fallback UI site name.
	DefaultSiteName = "CLIProxyAPI"
	// QuotaPollIntervalSecondsKey controls the quota poll interval in seconds.
	QuotaPollIntervalSecondsKey = "QUOTA_POLL_INTERVAL_SECONDS"
	// QuotaPollMaxConcurrencyKey controls the max concurrent quota requests.
	QuotaPollMaxConcurrencyKey = "QUOTA_POLL_MAX_CONCURRENCY"
	// AutoAssignProxyKey toggles auto assignment of proxies on create.
	AutoAssignProxyKey = "AUTO_ASSIGN_PROXY"
	// DefaultQuotaPollIntervalSeconds is the fallback poll interval (seconds).
	DefaultQuotaPollIntervalSeconds = 180
	// DefaultQuotaPollMaxConcurrency is the fallback max concurrency.
	DefaultQuotaPollMaxConcurrency = 5
	// DefaultAutoAssignProxy sets auto-assign proxy default.
	DefaultAutoAssignProxy = false
)
