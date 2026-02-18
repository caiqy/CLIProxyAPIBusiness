package quota

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	internalsettings "github.com/router-for-me/CLIProxyAPIBusiness/internal/settings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	defaultPollInterval   = 3 * time.Minute
	defaultRequestTimeout = 20 * time.Second
	maxConcurrentRequests = 5
	noAuthRetryInterval   = 10 * time.Second
	maxErrorBodyBytes     = 512
)

const (
	antigravityUserAgent = "antigravity/1.11.5 windows/amd64"
	codexUserAgent       = "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal"
)

var (
	antigravityQuotaURLs = []string{
		"https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
		"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels",
		"https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
	}
	geminiCLIQuotaURL = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota"
	codexUsageURL     = "https://chatgpt.com/backend-api/wham/usage"

	ErrUnsupportedProvider = errors.New("quota poller: unsupported provider")
)

type providerRequestError struct {
	provider   string
	statusCode int
	err        error
}

func (e *providerRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		return e.err.Error()
	}
	if e.statusCode > 0 {
		return fmt.Sprintf("quota poller: %s status=%d", e.provider, e.statusCode)
	}
	return "quota poller: provider request failed"
}

func (e *providerRequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *providerRequestError) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.statusCode
}

func (e *providerRequestError) Provider() string {
	if e == nil {
		return ""
	}
	return e.provider
}

type authRowInfo struct {
	ID          uint64
	Type        string
	RuntimeOnly bool
}

// Poller periodically fetches quota data for stored auth entries.
type Poller struct {
	db             *gorm.DB
	manager        *coreauth.Manager
	interval       time.Duration
	requestTimeout time.Duration
	hadAuths       bool
}

// NewPoller constructs a quota poller.
func NewPoller(db *gorm.DB, manager *coreauth.Manager) *Poller {
	if db == nil || manager == nil {
		return nil
	}
	return &Poller{
		db:             db,
		manager:        manager,
		interval:       defaultPollInterval,
		requestTimeout: defaultRequestTimeout,
	}
}

// Start launches the polling loop in a background goroutine.
func (p *Poller) Start(ctx context.Context) {
	if p == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go p.run(ctx)
	log.Infof("quota poller started (interval=%s)", p.interval)
}

func (p *Poller) run(ctx context.Context) {
	for {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		interval := p.poll(ctx)
		if ctx != nil && ctx.Err() != nil {
			return
		}
		if interval <= 0 {
			interval = p.interval
		}
		timer := time.NewTimer(interval)
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

func (p *Poller) poll(ctx context.Context) time.Duration {
	if p == nil {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}

	interval, maxConcurrency := p.resolvePollConfig()
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}

	auths := p.manager.List()
	if len(auths) == 0 {
		if !p.hadAuths {
			return noAuthRetryInterval
		}
		return interval
	}
	p.hadAuths = true

	rowMap, errRows := p.loadAuthRows(ctx)
	if errRows != nil {
		log.WithError(errRows).Warn("quota poller: load auth rows failed")
		return interval
	}

	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	shouldStop := false

	for _, auth := range auths {
		if shouldStop {
			break
		}
		if auth == nil || strings.TrimSpace(auth.ID) == "" {
			continue
		}
		row, ok := rowMap[auth.ID]
		if !ok || row.RuntimeOnly {
			continue
		}

		provider := strings.ToLower(strings.TrimSpace(auth.Provider))
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(row.Type))
		}
		if provider != "antigravity" && provider != "codex" && provider != "gemini-cli" {
			continue
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			shouldStop = true
			break
		}
		if shouldStop {
			break
		}

		wg.Add(1)
		authCopy := auth
		rowCopy := row
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if errRefresh := p.refreshAuth(ctx, authCopy, rowCopy); errRefresh != nil && !errors.Is(errRefresh, ErrUnsupportedProvider) {
				log.WithError(errRefresh).Warnf("quota poller: refresh failed (auth=%s)", authCopy.ID)
			}
		}()
	}

	wg.Wait()
	return interval
}

func (p *Poller) refreshAuth(ctx context.Context, auth *coreauth.Auth, row authRowInfo) error {
	if p == nil {
		return errors.New("quota poller: poller not initialized")
	}
	if auth == nil || strings.TrimSpace(auth.ID) == "" {
		return errors.New("quota poller: auth not found")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(row.Type))
	}

	var errRefresh error
	switch provider {
	case "antigravity":
		errRefresh = p.pollAntigravity(ctx, auth, row)
	case "codex":
		errRefresh = p.pollCodex(ctx, auth, row)
	case "gemini-cli":
		errRefresh = p.pollGeminiCLI(ctx, auth, row)
	default:
		return ErrUnsupportedProvider
	}

	if row.ID != 0 {
		if errHealth := p.updateAuthHealth(ctx, row.ID, errRefresh); errHealth != nil {
			log.WithError(errHealth).Warnf("quota poller: update auth health failed (auth=%s)", auth.ID)
			if errRefresh == nil {
				return errHealth
			}
		}
	}

	return errRefresh
}

func (p *Poller) RefreshByAuthKey(ctx context.Context, authKey string) error {
	if p == nil || p.manager == nil {
		return errors.New("quota poller: manager not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	authKey = strings.TrimSpace(authKey)
	if authKey == "" {
		return errors.New("quota poller: auth key is required")
	}

	var targetAuth *coreauth.Auth
	for _, auth := range p.manager.List() {
		if auth == nil {
			continue
		}
		if strings.TrimSpace(auth.ID) == authKey {
			targetAuth = auth
			break
		}
	}
	if targetAuth == nil {
		return fmt.Errorf("quota poller: auth key not found: %s", authKey)
	}

	row, ok, errRow := p.loadAuthRowByKey(ctx, authKey)
	if errRow != nil {
		return errRow
	}
	if !ok {
		return fmt.Errorf("quota poller: auth key not found: %s", authKey)
	}
	if row.RuntimeOnly {
		return fmt.Errorf("quota poller: auth key is runtime-only: %s", authKey)
	}

	return p.refreshAuth(ctx, targetAuth, row)
}

func (p *Poller) loadAuthRowByKey(ctx context.Context, authKey string) (authRowInfo, bool, error) {
	if p == nil || p.db == nil {
		return authRowInfo{}, false, errors.New("quota poller: db not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	authKey = strings.TrimSpace(authKey)
	if authKey == "" {
		return authRowInfo{}, false, errors.New("quota poller: auth key is required")
	}

	var row models.Auth
	errFind := p.db.WithContext(ctx).
		Select("id", "key", "content").
		Where("key = ?", authKey).
		First(&row).Error
	if errors.Is(errFind, gorm.ErrRecordNotFound) {
		return authRowInfo{}, false, nil
	}
	if errFind != nil {
		return authRowInfo{}, false, errFind
	}

	metadata := parseMetadata(row.Content)
	return authRowInfo{
		ID:          row.ID,
		Type:        normalizeString(metadata["type"]),
		RuntimeOnly: isRuntimeOnly(metadata),
	}, true, nil
}

func (p *Poller) loadAuthRows(ctx context.Context) (map[string]authRowInfo, error) {
	if p == nil || p.db == nil {
		return nil, errors.New("quota poller: db not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var rows []models.Auth
	if errFind := p.db.WithContext(ctx).
		Select("id", "key", "content").
		Order("id ASC").
		Find(&rows).Error; errFind != nil {
		return nil, errFind
	}

	rowMap := make(map[string]authRowInfo, len(rows))
	for _, row := range rows {
		metadata := parseMetadata(row.Content)
		rowMap[row.Key] = authRowInfo{
			ID:          row.ID,
			Type:        normalizeString(metadata["type"]),
			RuntimeOnly: isRuntimeOnly(metadata),
		}
	}
	return rowMap, nil
}

func (p *Poller) resolvePollConfig() (time.Duration, int) {
	intervalSeconds := internalsettings.DefaultQuotaPollIntervalSeconds
	maxConcurrency := internalsettings.DefaultQuotaPollMaxConcurrency

	if raw, ok := internalsettings.DBConfigValue(internalsettings.QuotaPollIntervalSecondsKey); ok {
		if parsed, okParse := parseDBConfigInt(raw); okParse && parsed > 0 {
			intervalSeconds = parsed
		}
	}
	if raw, ok := internalsettings.DBConfigValue(internalsettings.QuotaPollMaxConcurrencyKey); ok {
		if parsed, okParse := parseDBConfigInt(raw); okParse && parsed > 0 {
			maxConcurrency = parsed
		}
	}
	if maxConcurrency > maxConcurrentRequests {
		maxConcurrency = maxConcurrentRequests
	}
	return time.Duration(intervalSeconds) * time.Second, maxConcurrency
}

func (p *Poller) pollAntigravity(ctx context.Context, auth *coreauth.Auth, row authRowInfo) error {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", antigravityUserAgent)
	body := []byte("{}")
	var lastErr error

	for _, url := range antigravityQuotaURLs {
		status, payload, errReq := p.doRequest(ctx, auth, http.MethodPost, url, body, headers)
		if errReq != nil {
			log.WithError(errReq).Warnf("quota poller: antigravity request failed (auth=%s)", auth.ID)
			lastErr = errReq
			continue
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			log.Warnf("quota poller: antigravity status=%d (auth=%s body=%s)", status, auth.ID, summarizePayload(payload))
			lastErr = &providerRequestError{
				provider:   "antigravity",
				statusCode: status,
				err:        fmt.Errorf("quota poller: antigravity non-2xx status=%d", status),
			}
			continue
		}
		if errSave := p.saveQuota(ctx, row.ID, row.Type, payload); errSave != nil {
			log.WithError(errSave).Warnf("quota poller: antigravity save failed (auth=%s)", auth.ID)
			return errSave
		}
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("quota poller: antigravity refresh failed")
	}
	return lastErr
}

func (p *Poller) pollCodex(ctx context.Context, auth *coreauth.Auth, row authRowInfo) error {
	metadata := auth.Metadata
	accountID := resolveCodexAccountID(metadata)
	if accountID == "" {
		log.Warnf("quota poller: codex missing account id (auth=%s)", auth.ID)
		return errors.New("quota poller: codex missing account id")
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", codexUserAgent)
	headers.Set("Chatgpt-Account-Id", accountID)

	status, payload, errReq := p.doRequest(ctx, auth, http.MethodGet, codexUsageURL, nil, headers)
	if errReq != nil {
		log.WithError(errReq).Warnf("quota poller: codex request failed (auth=%s)", auth.ID)
		return errReq
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		log.Warnf("quota poller: codex status=%d (auth=%s body=%s)", status, auth.ID, summarizePayload(payload))
		return &providerRequestError{
			provider:   "codex",
			statusCode: status,
			err:        fmt.Errorf("quota poller: codex non-2xx status=%d", status),
		}
	}
	if errSave := p.saveQuota(ctx, row.ID, row.Type, payload); errSave != nil {
		log.WithError(errSave).Warnf("quota poller: codex save failed (auth=%s)", auth.ID)
		return errSave
	}
	return nil
}

func (p *Poller) pollGeminiCLI(ctx context.Context, auth *coreauth.Auth, row authRowInfo) error {
	metadata := auth.Metadata
	projectID := resolveGeminiProjectID(metadata)
	if projectID == "" {
		log.Warnf("quota poller: gemini-cli missing project id (auth=%s)", auth.ID)
		return errors.New("quota poller: gemini-cli missing project id")
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	body, errMarshal := json.Marshal(map[string]string{"project": projectID})
	if errMarshal != nil {
		log.WithError(errMarshal).Warnf("quota poller: gemini-cli request body failed (auth=%s)", auth.ID)
		return errMarshal
	}

	status, payload, errReq := p.doRequest(ctx, auth, http.MethodPost, geminiCLIQuotaURL, body, headers)
	if errReq != nil {
		log.WithError(errReq).Warnf("quota poller: gemini-cli request failed (auth=%s)", auth.ID)
		return errReq
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		log.Warnf("quota poller: gemini-cli status=%d (auth=%s body=%s)", status, auth.ID, summarizePayload(payload))
		return &providerRequestError{
			provider:   "gemini-cli",
			statusCode: status,
			err:        fmt.Errorf("quota poller: gemini-cli non-2xx status=%d", status),
		}
	}
	if errSave := p.saveQuota(ctx, row.ID, row.Type, payload); errSave != nil {
		log.WithError(errSave).Warnf("quota poller: gemini-cli save failed (auth=%s)", auth.ID)
		return errSave
	}
	return nil
}

func (p *Poller) doRequest(ctx context.Context, auth *coreauth.Auth, method, targetURL string, body []byte, headers http.Header) (int, []byte, error) {
	if p == nil || p.manager == nil {
		return 0, nil, errors.New("quota poller: manager not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
	defer cancel()

	req, errReq := p.manager.NewHttpRequest(reqCtx, auth, method, targetURL, body, headers)
	if errReq != nil {
		return 0, nil, errReq
	}

	resp, errResp := p.manager.HttpRequest(reqCtx, auth, req)
	if errResp != nil {
		return 0, nil, errResp
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("quota poller: close response body error: %v", errClose)
		}
	}()

	payload, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return resp.StatusCode, nil, errRead
	}
	return resp.StatusCode, payload, nil
}

func (p *Poller) saveQuota(ctx context.Context, authID uint64, authType string, payload []byte) error {
	if p == nil || p.db == nil {
		return errors.New("quota poller: db not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	authType = strings.TrimSpace(authType)
	if authType == "" {
		authType = "unknown"
	}

	payload = normalizePayload(payload)
	if len(payload) == 0 {
		return errors.New("quota poller: empty payload")
	}

	now := time.Now().UTC()
	var existing models.Quota
	errFind := p.db.WithContext(ctx).
		Where("auth_id = ? AND type = ?", authID, authType).
		First(&existing).Error
	if errFind == nil {
		return p.db.WithContext(ctx).
			Model(&models.Quota{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"data":       datatypes.JSON(payload),
				"updated_at": now,
			}).Error
	}
	if errors.Is(errFind, gorm.ErrRecordNotFound) {
		row := models.Quota{
			AuthID:    authID,
			Type:      authType,
			Data:      datatypes.JSON(payload),
			CreatedAt: now,
			UpdatedAt: now,
		}
		return p.db.WithContext(ctx).Create(&row).Error
	}
	return errFind
}

func normalizePayload(payload []byte) []byte {
	trimmed := bytesTrimSpace(payload)
	if len(trimmed) == 0 {
		return nil
	}
	if json.Valid(trimmed) {
		return trimmed
	}
	escaped, errMarshal := json.Marshal(string(trimmed))
	if errMarshal != nil {
		return nil
	}
	return escaped
}

func parseMetadata(content datatypes.JSON) map[string]any {
	if len(content) == 0 {
		return nil
	}
	var metadata map[string]any
	if errUnmarshal := json.Unmarshal(content, &metadata); errUnmarshal != nil {
		return nil
	}
	return metadata
}

func isRuntimeOnly(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	if raw := metadata["runtime_only"]; normalizeBool(raw) {
		return true
	}
	return normalizeBool(metadata["runtimeOnly"])
}

func resolveCodexAccountID(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if accountID := normalizeString(metadata["account_id"]); accountID != "" {
		return accountID
	}
	if accountID := normalizeString(metadata["accountId"]); accountID != "" {
		return accountID
	}

	meta := mapFromAny(metadata["metadata"])
	attrs := mapFromAny(metadata["attributes"])
	candidates := []any{
		metadata["id_token"],
		meta["id_token"],
		attrs["id_token"],
	}
	for _, candidate := range candidates {
		accountID := extractCodexChatgptAccountID(candidate)
		if accountID != "" {
			return accountID
		}
	}
	return ""
}

func extractCodexChatgptAccountID(value any) string {
	payload := parseIDTokenPayload(value)
	if payload == nil {
		return ""
	}
	accountID := normalizeString(payload["chatgpt_account_id"])
	if accountID != "" {
		return accountID
	}
	return normalizeString(payload["chatgptAccountId"])
}

func resolveGeminiProjectID(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if projectID := normalizeString(metadata["project_id"]); projectID != "" {
		return projectID
	}
	return ""
}

func parseIDTokenPayload(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	raw := normalizeString(value)
	if raw == "" {
		return nil
	}

	var parsed map[string]any
	if errUnmarshal := json.Unmarshal([]byte(raw), &parsed); errUnmarshal == nil {
		return parsed
	}

	segments := strings.Split(raw, ".")
	if len(segments) < 2 {
		return nil
	}
	payload, errDecode := decodeBase64URL(segments[1])
	if errDecode != nil || payload == "" {
		return nil
	}
	if errUnmarshal := json.Unmarshal([]byte(payload), &parsed); errUnmarshal != nil {
		return nil
	}
	return parsed
}

func decodeBase64URL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	decoded, errDecode := base64.RawURLEncoding.DecodeString(value)
	if errDecode == nil {
		return string(decoded), nil
	}

	padded := value
	if rem := len(value) % 4; rem != 0 {
		padded = value + strings.Repeat("=", 4-rem)
	}
	decoded, errDecode = base64.URLEncoding.DecodeString(padded)
	if errDecode != nil {
		return "", errDecode
	}
	return string(decoded), nil
}

func normalizeString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return ""
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		val := float64(typed)
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return ""
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	default:
		return ""
	}
}

func normalizeBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return nil
	}
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
		return int(math.Round(f)), true
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

func summarizePayload(payload []byte) string {
	trimmed := bytesTrimSpace(payload)
	if len(trimmed) == 0 {
		return ""
	}
	if len(trimmed) > maxErrorBodyBytes {
		return string(trimmed[:maxErrorBodyBytes]) + "...(truncated)"
	}
	return string(trimmed)
}

func (p *Poller) updateAuthHealth(ctx context.Context, authID uint64, errRefresh error) error {
	if p == nil || p.db == nil {
		return errors.New("quota poller: db not initialized")
	}
	if authID == 0 {
		return errors.New("quota poller: auth id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC()
	updates := map[string]any{
		"last_auth_check_at": now,
	}
	if errRefresh == nil {
		updates["token_invalid"] = false
		updates["last_auth_error"] = ""
	} else {
		updates["last_auth_error"] = errRefresh.Error()
		if statusCode, ok := refreshStatusCode(errRefresh); ok {
			if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
				updates["token_invalid"] = true
			}
		}
	}

	return p.db.WithContext(ctx).
		Model(&models.Auth{}).
		Where("id = ?", authID).
		Updates(updates).Error
}

func refreshStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	var statusErr interface{ StatusCode() int }
	if !errors.As(err, &statusErr) {
		return 0, false
	}
	statusCode := statusErr.StatusCode()
	if statusCode <= 0 {
		return 0, false
	}
	return statusCode, true
}
