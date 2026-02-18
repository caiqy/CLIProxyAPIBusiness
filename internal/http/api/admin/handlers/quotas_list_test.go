package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupQuotaListDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:quotalist_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, errOpen := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if errOpen != nil {
		t.Fatalf("open db: %v", errOpen)
	}
	if errMigrate := db.AutoMigrate(&models.Auth{}, &models.Quota{}); errMigrate != nil {
		t.Fatalf("migrate db: %v", errMigrate)
	}
	return db
}

func TestQuotaListIncludesAuthTokenHealthFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupQuotaListDB(t)

	checkedAt := time.Date(2026, 2, 18, 7, 8, 9, 0, time.UTC)
	auth := models.Auth{
		Key:             "auth-with-health",
		Content:         datatypes.JSON([]byte(`{"type":"codex"}`)),
		IsAvailable:     false,
		TokenInvalid:    true,
		LastAuthCheckAt: &checkedAt,
		LastAuthError:   "token expired",
	}
	if errCreate := db.Create(&auth).Error; errCreate != nil {
		t.Fatalf("create auth: %v", errCreate)
	}
	if errUpdate := db.Model(&models.Auth{}).Where("id = ?", auth.ID).Update("is_available", false).Error; errUpdate != nil {
		t.Fatalf("set auth unavailable: %v", errUpdate)
	}
	quota := models.Quota{AuthID: auth.ID, Type: "codex", Data: datatypes.JSON([]byte(`{"ok":true}`))}
	if errCreate := db.Create(&quota).Error; errCreate != nil {
		t.Fatalf("create quota: %v", errCreate)
	}

	handler := NewQuotaHandler(db, nil, nil)
	router := gin.New()
	router.GET("/v0/admin/quotas", handler.List)

	req := httptest.NewRequest(http.MethodGet, "/v0/admin/quotas", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Quotas []struct {
			AuthKey         string     `json:"auth_key"`
			IsAvailable     bool       `json:"is_available"`
			TokenInvalid    bool       `json:"token_invalid"`
			LastAuthCheckAt *time.Time `json:"last_auth_check_at"`
			LastAuthError   string     `json:"last_auth_error"`
		} `json:"quotas"`
	}
	if errDecode := json.Unmarshal(w.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if len(resp.Quotas) != 1 {
		t.Fatalf("expected 1 quota row, got %d", len(resp.Quotas))
	}
	row := resp.Quotas[0]
	if row.AuthKey != auth.Key {
		t.Fatalf("expected auth_key=%q, got %q", auth.Key, row.AuthKey)
	}
	if row.IsAvailable {
		t.Fatalf("expected is_available=false, got true")
	}
	if !row.TokenInvalid {
		t.Fatalf("expected token_invalid=true, got false")
	}
	if row.LastAuthCheckAt == nil || !row.LastAuthCheckAt.Equal(checkedAt) {
		t.Fatalf("expected last_auth_check_at=%s, got %#v", checkedAt.Format(time.RFC3339), row.LastAuthCheckAt)
	}
	if row.LastAuthError != "token expired" {
		t.Fatalf("expected last_auth_error token expired, got %q", row.LastAuthError)
	}
}

func TestParseQuotaListAuthCheckTime(t *testing.T) {
	cases := []struct {
		name  string
		input sql.NullString
	}{
		{
			name:  "timezone without colon",
			input: sql.NullString{String: "2026-02-18 07:08:09+00", Valid: true},
		},
		{
			name:  "fractional seconds timezone without colon",
			input: sql.NullString{String: "2026-02-18 07:08:09.123456+00", Valid: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseQuotaListAuthCheckTime(tc.input)
			if parsed == nil {
				t.Fatalf("expected parsed time for %q, got nil", tc.input.String)
			}
		})
	}
}
