package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dbpkg "github.com/router-for-me/CLIProxyAPIBusiness/internal/db"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/gorm"
)

func TestTransactionRequestLogSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openDashboardRequestLogTestDB(t)

	usage := models.Usage{
		Provider:    "openai",
		Model:       "gpt-5.2",
		RequestID:   "req-ok-1",
		RequestedAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	baseDir := t.TempDir()
	setWritablePathForTest(t, baseDir)
	logsDir := filepath.Join(baseDir, "logs")
	if errMkdir := os.MkdirAll(logsDir, 0o755); errMkdir != nil {
		t.Fatalf("mkdir logs dir: %v", errMkdir)
	}
	logPath := filepath.Join(logsDir, "20260221-svc-req-ok-1.log")
	content := strings.Join([]string{
		"=== API REQUEST ===",
		"{\"model\":\"gpt-5.2\",\"messages\":[\"hello\"]}",
		"",
		"=== API RESPONSE ===",
		"{\"id\":\"resp-1\",\"choices\":[]}",
		"",
	}, "\n")
	if errWrite := os.WriteFile(logPath, []byte(content), 0o644); errWrite != nil {
		t.Fatalf("write log file: %v", errWrite)
	}

	h := NewDashboardHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions/1/request-log", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(usage.ID, 10)}}

	h.GetTransactionRequestLog(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		RequestID      string `json:"request_id"`
		APIRequestRaw  string `json:"api_request_raw"`
		APIResponseRaw string `json:"api_response_raw"`
		SourceFile     string `json:"source_file"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if resp.RequestID != "req-ok-1" {
		t.Fatalf("expected request_id=req-ok-1, got %q", resp.RequestID)
	}
	if !strings.Contains(resp.APIRequestRaw, "\"messages\"") {
		t.Fatalf("expected api_request_raw contains request payload, got %q", resp.APIRequestRaw)
	}
	if !strings.Contains(resp.APIResponseRaw, "\"choices\"") {
		t.Fatalf("expected api_response_raw contains response payload, got %q", resp.APIResponseRaw)
	}
	if resp.SourceFile != "20260221-svc-req-ok-1.log" {
		t.Fatalf("expected source_file match log file, got %q", resp.SourceFile)
	}
}

func TestTransactionRequestLogSuccessFromErrorLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openDashboardRequestLogTestDB(t)

	usage := models.Usage{
		Provider:    "openai",
		Model:       "gpt-5.2",
		RequestID:   "req-err-1",
		RequestedAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	baseDir := t.TempDir()
	setWritablePathForTest(t, baseDir)
	logsDir := filepath.Join(baseDir, "logs")
	if errMkdir := os.MkdirAll(logsDir, 0o755); errMkdir != nil {
		t.Fatalf("mkdir logs dir: %v", errMkdir)
	}
	logPath := filepath.Join(logsDir, "error-20260221-svc-req-err-1.log")
	content := "=== API REQUEST ===\n{\"prompt\":\"hi\"}\n\n=== API RESPONSE ===\n{\"error\":\"boom\"}\n"
	if errWrite := os.WriteFile(logPath, []byte(content), 0o644); errWrite != nil {
		t.Fatalf("write log file: %v", errWrite)
	}

	h := NewDashboardHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions/1/request-log", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(usage.ID, 10)}}

	h.GetTransactionRequestLog(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		RequestID      string `json:"request_id"`
		APIRequestRaw  string `json:"api_request_raw"`
		APIResponseRaw string `json:"api_response_raw"`
		SourceFile     string `json:"source_file"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if resp.RequestID != "req-err-1" {
		t.Fatalf("expected request_id=req-err-1, got %q", resp.RequestID)
	}
	if !strings.Contains(resp.APIResponseRaw, "\"error\"") {
		t.Fatalf("expected api_response_raw contains error payload, got %q", resp.APIResponseRaw)
	}
	if resp.SourceFile != "error-20260221-svc-req-err-1.log" {
		t.Fatalf("expected source_file match error log file, got %q", resp.SourceFile)
	}
}

func TestTransactionRequestLogSelectsAttemptByUsageOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openDashboardRequestLogTestDB(t)

	usage1 := models.Usage{
		Provider:    "openai",
		Model:       "gpt-5.2",
		RequestID:   "req-retry-1",
		RequestedAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&usage1).Error; errCreate != nil {
		t.Fatalf("create usage1: %v", errCreate)
	}

	usage2 := models.Usage{
		Provider:    "openai",
		Model:       "gpt-5.2",
		RequestID:   "req-retry-1",
		RequestedAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&usage2).Error; errCreate != nil {
		t.Fatalf("create usage2: %v", errCreate)
	}

	baseDir := t.TempDir()
	setWritablePathForTest(t, baseDir)
	logsDir := filepath.Join(baseDir, "logs")
	if errMkdir := os.MkdirAll(logsDir, 0o755); errMkdir != nil {
		t.Fatalf("mkdir logs dir: %v", errMkdir)
	}
	logPath := filepath.Join(logsDir, "20260221-svc-req-retry-1.log")
	content := strings.Join([]string{
		"=== API REQUEST 1 ===",
		"{\"attempt\":1,\"prompt\":\"hello\"}",
		"",
		"=== API REQUEST 2 ===",
		"{\"attempt\":2,\"prompt\":\"hello\"}",
		"",
		"=== API RESPONSE 1 ===",
		"{\"error\":\"first\"}",
		"",
		"=== API RESPONSE 2 ===",
		"{\"error\":\"second\"}",
		"",
	}, "\n")
	if errWrite := os.WriteFile(logPath, []byte(content), 0o644); errWrite != nil {
		t.Fatalf("write log file: %v", errWrite)
	}

	h := NewDashboardHandler(db)

	firstResp := callTransactionRequestLogHandler(t, h, usage1.ID)
	if !strings.Contains(firstResp.APIRequestRaw, "\"attempt\":1") {
		t.Fatalf("expected usage1 to map attempt 1 request, got %q", firstResp.APIRequestRaw)
	}
	if !strings.Contains(firstResp.APIResponseRaw, "\"first\"") {
		t.Fatalf("expected usage1 to map attempt 1 response, got %q", firstResp.APIResponseRaw)
	}

	secondResp := callTransactionRequestLogHandler(t, h, usage2.ID)
	if !strings.Contains(secondResp.APIRequestRaw, "\"attempt\":2") {
		t.Fatalf("expected usage2 to map attempt 2 request, got %q", secondResp.APIRequestRaw)
	}
	if !strings.Contains(secondResp.APIResponseRaw, "\"second\"") {
		t.Fatalf("expected usage2 to map attempt 2 response, got %q", secondResp.APIResponseRaw)
	}
}

func TestTransactionRequestLogUsageWithoutRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openDashboardRequestLogTestDB(t)

	usage := models.Usage{
		Provider:    "openai",
		Model:       "gpt-5.2",
		RequestID:   " ",
		RequestedAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	h := NewDashboardHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions/1/request-log", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(usage.ID, 10)}}

	h.GetTransactionRequestLog(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTransactionRequestLogNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openDashboardRequestLogTestDB(t)

	usage := models.Usage{
		Provider:    "openai",
		Model:       "gpt-5.2",
		RequestID:   "req-missing-1",
		RequestedAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	baseDir := t.TempDir()
	setWritablePathForTest(t, baseDir)
	logsDir := filepath.Join(baseDir, "logs")
	if errMkdir := os.MkdirAll(logsDir, 0o755); errMkdir != nil {
		t.Fatalf("mkdir logs dir: %v", errMkdir)
	}

	h := NewDashboardHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions/1/request-log", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(usage.ID, 10)}}

	h.GetTransactionRequestLog(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTransactionRequestLogLogsDirReadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openDashboardRequestLogTestDB(t)

	usage := models.Usage{
		Provider:    "openai",
		Model:       "gpt-5.2",
		RequestID:   "req-io-1",
		RequestedAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	baseDir := t.TempDir()
	setWritablePathForTest(t, baseDir)
	logsPath := filepath.Join(baseDir, "logs")
	if errWrite := os.WriteFile(logsPath, []byte("not-a-dir"), 0o644); errWrite != nil {
		t.Fatalf("create non-directory logs path: %v", errWrite)
	}

	h := NewDashboardHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions/1/request-log", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(usage.ID, 10)}}

	h.GetTransactionRequestLog(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestTransactionRequestLogLogsDirNotExist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openDashboardRequestLogTestDB(t)

	usage := models.Usage{
		Provider:    "openai",
		Model:       "gpt-5.2",
		RequestID:   "req-no-dir-1",
		RequestedAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	if errCreate := db.Create(&usage).Error; errCreate != nil {
		t.Fatalf("create usage: %v", errCreate)
	}

	baseDir := t.TempDir()
	setWritablePathForTest(t, baseDir)

	h := NewDashboardHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions/1/request-log", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(usage.ID, 10)}}

	h.GetTransactionRequestLog(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func openDashboardRequestLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	conn, errOpen := dbpkg.Open(":memory:")
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := dbpkg.Migrate(conn); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return conn
}

func setWritablePathForTest(t *testing.T, dir string) {
	t.Helper()
	original, had := os.LookupEnv("WRITABLE_PATH")
	if errSet := os.Setenv("WRITABLE_PATH", dir); errSet != nil {
		t.Fatalf("set WRITABLE_PATH: %v", errSet)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("WRITABLE_PATH", original)
			return
		}
		_ = os.Unsetenv("WRITABLE_PATH")
	})
}

func callTransactionRequestLogHandler(t *testing.T, h *DashboardHandler, usageID uint64) struct {
	RequestID      string `json:"request_id"`
	APIRequestRaw  string `json:"api_request_raw"`
	APIResponseRaw string `json:"api_response_raw"`
	SourceFile     string `json:"source_file"`
} {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/admin/dashboard/transactions/1/request-log", nil)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(usageID, 10)}}

	h.GetTransactionRequestLog(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		RequestID      string `json:"request_id"`
		APIRequestRaw  string `json:"api_request_raw"`
		APIResponseRaw string `json:"api_response_raw"`
		SourceFile     string `json:"source_file"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	return resp
}
