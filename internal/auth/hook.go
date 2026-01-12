package auth

import (
	"context"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// StatusCodeHook logs auth results with status-based severity.
type StatusCodeHook struct {
	coreauth.NoopHook
}

// NewStatusCodeHook constructs a StatusCodeHook.
func NewStatusCodeHook() *StatusCodeHook {
	return &StatusCodeHook{}
}

// OnResult logs request outcomes with severity derived from HTTP status codes.
func (h *StatusCodeHook) OnResult(ctx context.Context, result coreauth.Result) {
	entry := log.WithFields(log.Fields{
		"auth_id":  result.AuthID,
		"provider": result.Provider,
		"model":    result.Model,
		"success":  result.Success,
	})

	if result.Success {
		entry.Debug("request succeeded")
		return
	}

	if result.Error == nil {
		entry.Warn("request failed without error details")
		return
	}

	statusCode := result.Error.HTTPStatus
	entry = entry.WithField("status_code", statusCode)

	switch {
	case statusCode == 401:
		entry.Warn("unauthorized: credentials may be invalid or expired")
	case statusCode == 403:
		entry.Warn("forbidden: access denied")
	case statusCode == 429:
		entry.Warn("rate limited: too many requests")
	case statusCode == 500:
		entry.Error("internal server error from upstream")
	case statusCode == 502:
		entry.Error("bad gateway from upstream")
	case statusCode == 503:
		entry.Error("service unavailable from upstream")
	case statusCode >= 400 && statusCode < 500:
		entry.Warnf("client error: %s", result.Error.Message)
	case statusCode >= 500:
		entry.Errorf("server error: %s", result.Error.Message)
	default:
		entry.Infof("request failed: %s", result.Error.Message)
	}
}
