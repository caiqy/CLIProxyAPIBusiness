package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/buildinfo"
)

const (
	githubAPIURL    = "https://api.github.com/repos/router-for-me/CLIProxyAPIBusiness/releases/latest"
	githubReleaseUI = "https://github.com/router-for-me/CLIProxyAPIBusiness/releases/latest"
	httpTimeout     = 10 * time.Second
	cacheDuration   = 1 * time.Hour
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type versionCache struct {
	mu            sync.RWMutex
	latestVersion string
	releaseURL    string
	fetchedAt     time.Time
	hasError      bool
}

var globalVersionCache = &versionCache{}

func (c *versionCache) get() (version string, url string, valid bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.hasError || c.fetchedAt.IsZero() {
		return "", "", false
	}
	if time.Since(c.fetchedAt) > cacheDuration {
		return "", "", false
	}
	return c.latestVersion, c.releaseURL, true
}

func (c *versionCache) set(version, url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latestVersion = version
	c.releaseURL = url
	c.fetchedAt = time.Now()
	c.hasError = false
}

func (c *versionCache) setError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hasError = true
	c.fetchedAt = time.Time{}
}

// VersionHandler handles version check endpoints.
type VersionHandler struct{}

// NewVersionHandler constructs a VersionHandler.
func NewVersionHandler() *VersionHandler {
	return &VersionHandler{}
}

// VersionResponse is the response for version check.
type VersionResponse struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	HasUpdate      bool   `json:"has_update"`
	ReleaseURL     string `json:"release_url,omitempty"`
	Commit         string `json:"commit,omitempty"`
	BuildDate      string `json:"build_date,omitempty"`
	CheckError     string `json:"check_error,omitempty"`
}

// GetVersion returns current version and checks for updates from GitHub.
func (h *VersionHandler) GetVersion(c *gin.Context) {
	resp := VersionResponse{
		CurrentVersion: buildinfo.Version,
		Commit:         buildinfo.Commit,
		BuildDate:      buildinfo.BuildDate,
		HasUpdate:      false,
	}

	if cachedVersion, cachedURL, valid := globalVersionCache.get(); valid {
		resp.LatestVersion = cachedVersion
		resp.ReleaseURL = cachedURL
		resp.HasUpdate = isNewerVersion(buildinfo.Version, cachedVersion)
		c.JSON(http.StatusOK, resp)
		return
	}

	latestVersion, releaseURL, errFetch := fetchLatestRelease(c.Request.Context())
	if errFetch != nil {
		globalVersionCache.setError()
		resp.CheckError = errFetch.Error()
		c.JSON(http.StatusOK, resp)
		return
	}

	globalVersionCache.set(latestVersion, releaseURL)
	resp.LatestVersion = latestVersion
	resp.ReleaseURL = releaseURL
	resp.HasUpdate = isNewerVersion(buildinfo.Version, latestVersion)

	c.JSON(http.StatusOK, resp)
}

func fetchLatestRelease(ctx context.Context) (version string, url string, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	req, errReq := http.NewRequestWithContext(reqCtx, http.MethodGet, githubAPIURL, nil)
	if errReq != nil {
		return "", "", fmt.Errorf("failed to create request: %w", errReq)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "CLIProxyAPIBusiness")

	client := &http.Client{}
	resp, errDo := client.Do(req)
	if errDo != nil {
		return "", "", fmt.Errorf("failed to fetch release: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			if err == nil {
				err = fmt.Errorf("failed to close response body: %w", errClose)
			}
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if errDecode := json.NewDecoder(resp.Body).Decode(&release); errDecode != nil {
		return "", "", fmt.Errorf("failed to decode response: %w", errDecode)
	}

	releaseURL := release.HTMLURL
	if releaseURL == "" {
		releaseURL = githubReleaseUI
	}

	return release.TagName, releaseURL, nil
}

// isNewerVersion compares two semver-like versions (with optional 'v' prefix).
// Returns true if latest is newer than current.
func isNewerVersion(current, latest string) bool {
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	if current == "dev" || current == "" {
		return latest != "" && latest != "dev"
	}

	if latest == "" || latest == "dev" {
		return false
	}

	return latest != current && compareVersions(current, latest) < 0
}

// compareVersions compares two version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			fmt.Sscanf(partsA[i], "%d", &numA)
		}
		if i < len(partsB) {
			fmt.Sscanf(partsB[i], "%d", &numB)
		}

		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}

	return 0
}
